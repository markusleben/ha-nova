import { chmod, chown, lstat, mkdir, readdir, readFile, rename, stat, unlink, writeFile } from "node:fs/promises";
import type { Stats } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { realpath } from "node:fs/promises";

import { HttpError } from "../errors.js";
import { assertWritableExtension, isNotFound, LOGICAL_PREFIX } from "./files-paths.js";

const MAX_READ_BYTES = 1024 * 1024;

/**
 * Writes get a smaller ceiling than reads on purpose. The HTTP server rejects
 * the whole request body above DEFAULT_MAX_JSON_BODY_BYTES (1 MiB), and the
 * body carries more than the content: the action, the path, JSON quoting, and
 * escaping that can inflate the payload well beyond its byte length. A file at
 * exactly 1 MiB would therefore die as an opaque 413 instead of a
 * FILE_TOO_LARGE that says what is wrong. 768 KiB leaves room for all of it —
 * and is still far beyond any real Home Assistant YAML file.
 */
export const MAX_WRITE_BYTES = 768 * 1024;
const MAX_DIR_ENTRIES = 500;

export async function listDir(absolute: string, logicalPath: string): Promise<unknown> {
  const entries = await withNotFound(logicalPath, () => readdir(absolute, { withFileTypes: true }));
  const truncated = entries.length > MAX_DIR_ENTRIES;
  const shown = entries.slice(0, MAX_DIR_ENTRIES);

  const items = await Promise.all(
    shown.map(async (entry) => {
      // lstat, not stat: stat FOLLOWS a symlink, so listing a directory that
      // contains a link to secrets.yaml (or to something outside the root)
      // would leak the target's metadata — a path the endpoint refuses to serve
      // directly. A link is reported as a link, with no size.
      const info = entry.isSymbolicLink()
        ? null
        : await lstat(join(absolute, entry.name)).catch(() => null);
      return {
        name: entry.name,
        type: entry.isSymbolicLink() ? "symlink" : entry.isDirectory() ? "dir" : "file",
        size: info?.size ?? null
      };
    })
  );

  return truncated ? { entries: items, truncated: true } : { entries: items };
}

export async function readTextFile(absolute: string, logicalPath: string): Promise<unknown> {
  const info = await withNotFound(logicalPath, () => stat(absolute));
  if (info.isDirectory()) {
    throw new HttpError(400, "FILE_IS_DIRECTORY", `${logicalPath} is a directory — use list_dir`);
  }
  // Only REGULAR files. A FIFO reports size 0 and then blocks readFile forever,
  // holding a libuv worker hostage; sockets and device nodes are equally not
  // what a text-file endpoint is for.
  if (!info.isFile()) {
    throw new HttpError(
      400,
      "FILE_NOT_REGULAR",
      `${logicalPath} is not a regular file — /files serves text files only`
    );
  }
  if (info.size > MAX_READ_BYTES) {
    throw new HttpError(
      400,
      "FILE_TOO_LARGE",
      `${logicalPath} is ${info.size} bytes, above the ${MAX_READ_BYTES}-byte read limit`
    );
  }

  const buffer = await readFile(absolute);
  // Text only, and STRICTLY so. buffer.toString("utf8") would silently replace
  // invalid sequences with U+FFFD — and in the read -> diff -> write flow this
  // endpoint exists for, the agent would then write that corruption back into
  // the user's configuration. A fatal decoder refuses instead.
  let content: string;
  try {
    content = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
  } catch {
    throw new HttpError(
      400,
      "FILE_NOT_TEXT",
      `${logicalPath} is not valid UTF-8 text — serving it would corrupt the file when it is written back`
    );
  }
  if (content.includes("\u0000")) {
    throw new HttpError(
      400,
      "FILE_NOT_TEXT",
      `${logicalPath} contains NUL bytes — the relay only serves text through /files`
    );
  }

  return { content, size: info.size };
}
export async function writeTextFile(
  absolute: string,
  logicalPath: string,
  content: string,
  backup: boolean,
  configRoot: string
): Promise<unknown> {
  const existing = await stat(absolute).catch(() => null);
  if (existing?.isDirectory()) {
    throw new HttpError(400, "FILE_IS_DIRECTORY", `${logicalPath} is a directory`);
  }
  // Same reason as the read side: writing into a FIFO or a device node is not
  // what this endpoint does, and it would hang instead of failing.
  if (existing && !existing.isFile()) {
    throw new HttpError(
      400,
      "FILE_NOT_REGULAR",
      `${logicalPath} is not a regular file — refusing to write to it`
    );
  }

  let backupPath: string | null = null;
  if (existing && backup) {
    // The .bak copy goes through the same ceiling as a read: without this, a
    // huge pre-existing file would be pulled into memory and duplicated on disk
    // by an otherwise tiny write, quietly bypassing the advertised cap.
    if (existing.size > MAX_READ_BYTES) {
      throw new HttpError(
        400,
        "FILE_TOO_LARGE",
        `${logicalPath} is ${existing.size} bytes and cannot be backed up under the ${MAX_READ_BYTES}-byte limit`
      );
    }
    backupPath = `${absolute}.bak`;
    // The .bak and .nova-tmp paths are DERIVED, so the containment check above
    // never saw them: if either already exists as a symlink, writing would
    // follow it straight out of the config directory. Remove any existing entry
    // (the link itself, never its target) and create exclusively, so a symlink
    // planted between the checks cannot be followed either.
    await removeIfPresent(backupPath);
    await writeFile(backupPath, await readFile(absolute), { flag: "wx" });
    // A config file deliberately kept at 0600 (because it holds tokens that are
    // not in secrets.yaml) must not become world-readable just because HA NOVA
    // touched it. chmod after the write, not a mode flag: the flag is masked by
    // the process umask, chmod is not.
    await chmod(backupPath, existing.mode & 0o777);
    // The backup carries the same content, so it deserves the same ownership —
    // best effort here: a .bak the relay owns is a nuisance, not a lockout.
    await chown(backupPath, existing.uid, existing.gid).catch(() => undefined);
  }

  await mkdir(dirname(absolute), { recursive: true });
  // Write to a temp file and rename: a crash mid-write must never leave Home
  // Assistant with a half-written configuration file.
  const tempPath = `${absolute}.nova-tmp`;
  await removeIfPresent(tempPath);
  await writeFile(tempPath, content, { encoding: "utf8", flag: "wx" });
  if (existing) {
    // Same reason as the backup: the rename would otherwise replace a 0600 file
    // with a default-mode one, silently widening access to its contents.
    await chmod(tempPath, existing.mode & 0o777);
    await preserveOwnership(tempPath, existing, logicalPath);
  }
  await rename(tempPath, absolute);

  return {
    written: true,
    size: Buffer.byteLength(content, "utf8"),
    created: existing === null,
    // The reported path must be the one that EXISTS. When the write went
    // through a symlink, the backup sits beside the real target, not beside the
    // link — reporting the link's path would send the rollback flow to a file
    // that was never created, losing the safety net exactly when it is needed.
    backup: backupPath ? await toLogicalPath(configRoot, backupPath) : null
  };
}

// Removes a derived path (.bak / .nova-tmp) if anything is there — including a
// symlink, in which case unlink removes the LINK, not whatever it points at.
/**
 * A rename replaces the file with a NEW inode owned by whoever wrote it. In the
 * standalone image the relay runs as `nova`, so a Home Assistant config file
 * owned by another user would come back owned by the relay — and Home Assistant
 * could then no longer write its own file.
 *
 * chown fixes that when the process is allowed to (root, e.g. the App). When it
 * is not, the write is ABORTED before the rename: the original file is left
 * exactly as it was, and the caller is told why. Locking Home Assistant out of
 * its own configuration is not a trade worth making for a successful-looking
 * write.
 */
async function preserveOwnership(
  tempPath: string,
  existing: Stats,
  logicalPath: string
): Promise<void> {
  const sameOwner =
    existing.uid === (process.getuid?.() ?? existing.uid) &&
    existing.gid === (process.getgid?.() ?? existing.gid);
  if (sameOwner) {
    return;
  }

  try {
    await chown(tempPath, existing.uid, existing.gid);
  } catch {
    await removeIfPresent(tempPath);
    throw new HttpError(
      403,
      "FILE_OWNER_MISMATCH",
      `${logicalPath} is owned by another user and the relay cannot preserve that ownership — replacing it would leave Home Assistant unable to write its own file. Nothing was changed.`
    );
  }
}

async function removeIfPresent(path: string): Promise<void> {
  try {
    await unlink(path);
  } catch (error) {
    if (!isNotFound(error)) {
      throw error;
    }
  }
}

// Maps a real path back to the logical /config/... path a skill speaks.
async function toLogicalPath(configRoot: string, absolutePath: string): Promise<string> {
  const realRoot = await realpath(configRoot);
  const rel = relative(realRoot, absolutePath).split(sep).join("/");
  return `${LOGICAL_PREFIX}/${rel}`;
}
export async function deleteFile(absolute: string, logicalPath: string): Promise<unknown> {
  // lstat, not stat: delete removes the LINK, so the guard must look at the
  // link, not at what it points at. With stat, a symlink to a directory would
  // trip the directory guard and become undeletable — even though unlinking it
  // touches nothing but the link itself.
  const info = await withNotFound(logicalPath, () => lstat(absolute));
  if (info.isDirectory()) {
    throw new HttpError(
      400,
      "FILE_IS_DIRECTORY",
      "the relay does not delete directories — remove files individually"
    );
  }
  // A caller that may not CREATE a .py has no business DELETING one either. The
  // boundary applies to real files; a symlink is just a link and may always be
  // cleaned up, whatever it is called.
  if (info.isFile()) {
    assertWritableExtension(logicalPath);
  }
  await unlink(absolute);
  return { deleted: true };
}
async function withNotFound<T>(logicalPath: string, fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    if (isNotFound(error)) {
      throw new HttpError(404, "FILE_NOT_FOUND", `not found: ${logicalPath}`);
    }
    // list_dir on a regular file throws ENOTDIR. That is an ordinary client
    // mistake — turning it into a 500 would blame the server for a bad request.
    if ((error as NodeJS.ErrnoException | undefined)?.code === "ENOTDIR") {
      throw new HttpError(400, "FILE_NOT_DIRECTORY", `${logicalPath} is a file — use read_file`);
    }
    throw error;
  }
}
