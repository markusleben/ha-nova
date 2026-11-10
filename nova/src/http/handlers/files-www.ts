import { createHash, randomBytes } from "node:crypto";
import { constants, type Stats } from "node:fs";
import { link, lstat, mkdir, open, readdir, readFile, rename, unlink } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

import { HttpError } from "../errors.js";
import { isNotFound, LOGICAL_PREFIX, type ResolvedTarget } from "./files-paths.js";

// Images and videos in /config/www (docs/work/2026-11-10-www-media-delete-spec.md):
// a delete keeps the file's exact bytes in the Relay's own data folder first,
// and only those bytes ever go back, to the path they came from, never over
// anything. Every store action holds one lock per store, so nothing ever sees
// a copy half written.

const MEDIA_EXTENSIONS = [".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4"];
export const MAX_COPY_BYTES = 25 * 1024 * 1024;
export const MAX_STORE_BYTES = 100 * 1024 * 1024;
export const KEEP_MS = 30 * 24 * 60 * 60 * 1000;
export const COPY_ID = /^[0-9a-f]{32}$/;
const ENTRY = /^([0-9a-f]{32})\.(bin|json)$/;
// What a copy's record and folder entries cost besides its bytes: an empty
// picture still takes room, so a quota of bytes alone wouldn't bound the store.
const COPY_OVERHEAD = 4096;
const used = (kept: Kept[]): number => kept.reduce((sum, copy) => sum + copy.bytes + COPY_OVERHEAD, 0);

interface Kept {
  copy: string;
  path: string;
  bytes: number;
  mode: number;
  sha256: string;
  deleted_at: string;
}

const locks = new Map<string, Promise<unknown>>();

function locked<T>(store: string, run: () => Promise<T>): Promise<T> {
  const result = (locks.get(store) ?? Promise.resolve()).then(run);
  locks.set(store, result.catch(() => undefined));
  return result;
}

export function assertMediaName(logicalPath: string): void {
  const name = logicalPath.split("/").pop() ?? "";
  const dot = name.lastIndexOf(".");
  if (dot <= 0 || !MEDIA_EXTENSIONS.includes(name.slice(dot).toLowerCase())) {
    throw new HttpError(
      403,
      "FILE_TYPE_DENIED",
      `in www HA NOVA deletes only images and videos (${MEDIA_EXTENSIONS.join(", ")}) — refusing '${name}'`
    );
  }
}

export async function deleteMedia(target: ResolvedTarget, store: string, now: () => number): Promise<unknown> {
  const { absolute, logical } = target;
  const info = await lstat(absolute).catch((error: unknown) => {
    throw isNotFound(error) ? new HttpError(404, "FILE_NOT_FOUND", `not found: ${logical}`) : error;
  });
  if (info.isDirectory()) {
    throw new HttpError(400, "FILE_IS_DIRECTORY", "the relay does not delete directories — remove files individually");
  }
  // One link only: a hard link's bytes live on under another name, so its
  // copy would not be what the delete removes.
  if (!info.isFile() || info.nlink !== 1) {
    throw new HttpError(400, "FILE_NOT_REGULAR", `${logical} is not a plain file (a link or a hard-linked file) — HA NOVA deletes plain files only`);
  }
  assertMediaName(logical);
  if (info.size > MAX_COPY_BYTES) {
    throw new HttpError(400, "FILE_TOO_LARGE", `${logical} is ${info.size} bytes; HA NOVA keeps copies of files up to ${MAX_COPY_BYTES} bytes, so it doesn't delete it. Delete it in Home Assistant's File editor instead.`);
  }
  return await locked(store, async () => {
    const at = now();
    const kept = await prune(store, at);
    if (used(kept) + info.size + COPY_OVERHEAD > MAX_STORE_BYTES) {
      throw new HttpError(
        409,
        "UNDO_STORE_FULL",
        `HA NOVA's copies of deleted files are at their ${MAX_STORE_BYTES}-byte limit, so nothing was deleted. Put a copy back or wait until copies expire (list_deleted shows when), or delete the file in Home Assistant's File editor.`
      );
    }
    const bytes = await readExactly(absolute, logical, info);
    if ((await mkdir(store, { recursive: true, mode: 0o700 })) !== undefined) {
      await syncFolder(dirname(store)); // a new store's own entry is durable too
    }
    const copy = randomBytes(16).toString("hex");
    const record = { path: logical, bytes: bytes.length, mode: info.mode & 0o777, sha256: sha256(bytes), deleted_at: new Date(at).toISOString() };
    try {
      await writeDurable(join(store, `${copy}.bin`), bytes);
      await writeDurable(join(store, `${copy}.json`), JSON.stringify(record));
      await syncFolder(store);
      await takeOut(absolute, logical, info, copy);
    } catch (error) {
      if (!(error instanceof HttpError && error.code === "FILE_NOT_PUT_BACK")) {
        await removeCopy(store, copy);
      }
      throw error;
    }
    return { deleted: true, copy, kept_until: new Date(at + KEEP_MS).toISOString() };
  });
}

export async function restoreMedia(target: ResolvedTarget, copy: string, store: string, now: () => number): Promise<unknown> {
  return await locked(store, async () => {
    const record = (await prune(store, now())).find((kept) => kept.copy === copy);
    if (record === undefined) {
      throw new HttpError(404, "COPY_NOT_FOUND", `HA NOVA keeps no copy ${copy} (copies expire after 30 days); list_deleted shows the ones it keeps`);
    }
    const bytes = await readFile(join(store, `${copy}.bin`));
    if (record.path !== target.logical || bytes.length !== record.bytes || sha256(bytes) !== record.sha256) {
      throw new HttpError(400, "COPY_INVALID", `copy ${copy} doesn't belong to ${target.logical}, or it no longer matches what was deleted; nothing was written`);
    }
    assertMediaName(target.logical);
    // wx: O_CREAT|O_EXCL, so an existing entry (a link included) is never
    // replaced or followed.
    const handle = await open(target.absolute, "wx", 0o600).catch((error: unknown) => {
      if ((error as NodeJS.ErrnoException).code === "EEXIST") {
        throw new HttpError(409, "FILE_EXISTS", `${target.logical} exists again, so HA NOVA put nothing back; the copy stays until it expires`);
      }
      if (isNotFound(error)) {
        throw new HttpError(404, "FILE_NOT_FOUND", `the folder of ${target.logical} no longer exists; HA NOVA creates no folders in www`);
      }
      throw notPermitted(error, target.logical) ?? error;
    });
    try {
      await handle.writeFile(bytes);
      await handle.chmod(record.mode);
      await handle.sync();
    } catch (error) {
      await handle.close();
      await unlink(target.absolute).catch(() => undefined);
      throw error;
    }
    await handle.close();
    await syncFolder(dirname(target.absolute));
    await removeCopy(store, copy);
    return { restored: true };
  });
}

export async function listDeleted(store: string, now: () => number): Promise<unknown> {
  return await locked(store, async () => {
    const kept = await prune(store, now());
    return {
      copies: kept.map(({ copy, path, bytes, deleted_at }) => ({
        copy,
        path,
        bytes,
        deleted_at,
        kept_until: new Date(Date.parse(deleted_at) + KEEP_MS).toISOString()
      })),
      used_bytes: used(kept),
      limit_bytes: MAX_STORE_BYTES,
      file_limit_bytes: MAX_COPY_BYTES
    };
  });
}

// Exactly the inode lstat saw, never more than its size: a file that grew,
// shrank, or became a link meanwhile is not the one the person meant.
async function readExactly(absolute: string, logical: string, seen: Stats): Promise<Buffer> {
  const handle = await open(absolute, constants.O_RDONLY | constants.O_NOFOLLOW).catch((error: unknown) => {
    throw (error as NodeJS.ErrnoException).code === "ELOOP" || isNotFound(error) ? changed(logical) : notPermitted(error, logical) ?? error;
  });
  try {
    const opened = await handle.stat();
    if (!opened.isFile() || opened.ino !== seen.ino || opened.dev !== seen.dev || opened.size !== seen.size) {
      throw changed(logical);
    }
    const bytes = Buffer.alloc(seen.size);
    let filled = 0;
    while (filled < bytes.length) {
      const { bytesRead } = await handle.read(bytes, filled, bytes.length - filled, filled);
      if (bytesRead === 0) {
        throw changed(logical);
      }
      filled += bytesRead;
    }
    if ((await handle.read(Buffer.alloc(1), 0, 1, filled)).bytesRead !== 0) {
      throw changed(logical);
    }
    return bytes;
  } finally {
    await handle.close();
  }
}

// The rename takes whatever is at the name in one step; only the copied
// inode, unchanged, is then removed. Anything else goes back under its name.
async function takeOut(absolute: string, logical: string, copied: Stats, copy: string): Promise<void> {
  const hidden = join(dirname(absolute), `.nova-delete-${copy}`);
  await rename(absolute, hidden).catch((error: unknown) => {
    throw isNotFound(error) ? changed(logical) : notPermitted(error, logical) ?? error;
  });
  let failure: unknown = changed(logical);
  try {
    const now = await lstat(hidden);
    if (now.isFile() && now.ino === copied.ino && now.dev === copied.dev && now.size === copied.size && now.mtimeMs === copied.mtimeMs) {
      await unlink(hidden);
      return;
    }
  } catch (error) {
    failure = notPermitted(error, logical) ?? error;
  }
  // Not deleted: whatever was taken goes back under its name (never over a
  // new file), so the caller may drop the copy.
  try {
    await link(hidden, absolute);
    await unlink(hidden);
  } catch {
    throw new HttpError(500, "FILE_NOT_PUT_BACK", `${logical} could not be put back after a failed delete; it waits in the same folder as ${basename(hidden)}, and HA NOVA's copy is kept`);
  }
  throw failure;
}

// Expired copies, a .bin without its record (a commit that never finished:
// the original never left www), and a record without its .bin go; the rest
// are the copies kept, newest first.
async function prune(store: string, now: number): Promise<Kept[]> {
  const folder = await lstat(store).catch((error: unknown) => (isNotFound(error) ? null : Promise.reject(error)));
  if (folder === null) {
    return [];
  }
  if (!folder.isDirectory()) {
    throw new Error(`HA NOVA's copy store ${store} is not a folder`);
  }
  const names = await readdir(store);
  const kept: Kept[] = [];
  for (const copy of new Set(names.flatMap((name) => ENTRY.exec(name)?.[1] ?? []))) {
    const record = await readRecord(store, copy);
    const bin = await lstat(join(store, `${copy}.bin`)).catch(() => null);
    if (record !== null && bin?.isFile() === true && bin.size === record.bytes && now - Date.parse(record.deleted_at) < KEEP_MS) {
      kept.push(record);
    } else {
      await removeCopy(store, copy);
    }
  }
  for (const name of names.filter((name) => name.startsWith(".tmp-"))) {
    await unlink(join(store, name)).catch(() => undefined);
  }
  return kept.sort((a, b) => b.deleted_at.localeCompare(a.deleted_at));
}

async function readRecord(store: string, copy: string): Promise<Kept | null> {
  const file = join(store, `${copy}.json`);
  if ((await lstat(file).catch(() => null))?.isFile() !== true) {
    return null;
  }
  try {
    const raw = JSON.parse(await readFile(file, "utf8")) as Record<string, unknown>;
    const { path, bytes, mode, sha256: hash, deleted_at } = raw;
    if (
      typeof path !== "string" || !path.startsWith(`${LOGICAL_PREFIX}/`) ||
      !Number.isSafeInteger(bytes) || !Number.isSafeInteger(mode) ||
      typeof hash !== "string" || !/^[0-9a-f]{64}$/.test(hash) ||
      typeof deleted_at !== "string" || Number.isNaN(Date.parse(deleted_at))
    ) {
      return null;
    }
    return { copy, path, bytes: bytes as number, mode: mode as number, sha256: hash, deleted_at };
  } catch {
    return null;
  }
}

async function writeDurable(path: string, data: Buffer | string): Promise<void> {
  const temp = join(dirname(path), `.tmp-${basename(path)}`);
  await unlink(temp).catch(() => undefined);
  const handle = await open(temp, "wx", 0o600);
  try {
    await handle.writeFile(data);
    await handle.sync();
  } finally {
    await handle.close();
  }
  await rename(temp, path);
}

async function syncFolder(path: string): Promise<void> {
  const handle = await open(path, "r");
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

async function removeCopy(store: string, copy: string): Promise<void> {
  for (const name of [`${copy}.json`, `${copy}.bin`]) {
    await unlink(join(store, name)).catch((error: unknown) => {
      if (!isNotFound(error)) {
        throw error;
      }
    });
  }
}

function sha256(bytes: Buffer): string {
  return createHash("sha256").update(bytes).digest("hex");
}

// A Relay running as its own user (the standalone container) may not change
// a file Home Assistant owns: a plain answer instead of a bare 500.
function notPermitted(error: unknown, logical: string): HttpError | null {
  const code = (error as NodeJS.ErrnoException | undefined)?.code;
  return code === "EACCES" || code === "EPERM"
    ? new HttpError(403, "FILE_NOT_PERMITTED", `the Relay is not allowed to change ${logical} or its folder; nothing was changed`)
    : null;
}

function changed(logical: string): HttpError {
  return new HttpError(409, "FILE_CHANGED", `${logical} changed while HA NOVA kept its copy, so nothing was deleted. Try again.`);
}
