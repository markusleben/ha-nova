import { constants } from "node:fs";
import { access, mkdir, readdir, readFile, realpath, rename, stat, unlink, writeFile } from "node:fs/promises";
import { dirname, isAbsolute, join, normalize, relative, resolve, sep } from "node:path";

import type { FileAccessConfig } from "../../config/file-access.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

export interface FilesHandlerOptions {
  fileAccess: FileAccessConfig;
}

type FileAction = "list_dir" | "read_file" | "write_file" | "delete_file";

const WRITE_ACTIONS = new Set<FileAction>(["write_file", "delete_file"]);

const MAX_READ_BYTES = 1024 * 1024;
const MAX_WRITE_BYTES = 1024 * 1024;
const MAX_DIR_ENTRIES = 500;
const LOGICAL_PREFIX = "/config";

/**
 * Paths that are NEVER readable or writable, whatever the mode says. These hold
 * credentials, Home Assistant's own state store, or data that is meaningless to
 * an editor and dangerous to corrupt. The relay stays dumb — this is a
 * transport boundary, not domain logic.
 */
const DENIED_SEGMENTS = [".storage", ".cloud", ".ssh", ".git", "deps", "ssl", "tts", "backups"];

/**
 * Prefix matches, not exact names: an editor, a backup script — or this relay's
 * own .bak convention — turns `secrets.yaml` into `secrets.yaml.bak`,
 * `secrets.yaml~` or `secrets.yaml.old`, and every one of those holds the same
 * credentials. An exact-name deny would serve them and quietly defeat the
 * guarantee that secrets stay unreachable. The same applies to the recorder
 * database's -wal/-shm siblings and to `.env` copies.
 */
const DENIED_PATTERNS = [
  /^secrets\.ya?ml/i,
  /^home-assistant_v2\.db/,
  /^\.env/,
  /\.log(\.\d+)?$/
];

export function createFilesHandler(options: FilesHandlerOptions): RouteHandler {
  return async ({ body }: RouteContext) => {
    const { mode, configRoot } = options.fileAccess;
    if (mode === "off") {
      throw new HttpError(
        403,
        "FILE_ACCESS_DISABLED",
        "File access is disabled. Set the 'file_access' option (read or readwrite) on the NOVA Relay App and restart it."
      );
    }

    const request = parseFilesRequest(body);
    if (WRITE_ACTIONS.has(request.action) && mode !== "readwrite") {
      throw new HttpError(
        403,
        "FILE_ACCESS_READONLY",
        `File access is set to 'read'. Set 'file_access' to 'readwrite' to allow ${request.action}.`
      );
    }

    const target = await resolveTargetPath(configRoot, request.path, request.action);

    switch (request.action) {
      case "list_dir":
        return await listDir(target, request.path);
      case "read_file":
        return await readTextFile(target, request.path);
      case "write_file":
        return await writeTextFile(target, request.path, request.content, request.backup);
      case "delete_file":
        return await deleteFile(target, request.path);
    }
  };
}

interface FilesRequest {
  action: FileAction;
  path: string;
  content: string;
  backup: boolean;
}

function parseFilesRequest(body: unknown): FilesRequest {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must be an object");
  }
  const raw = body as Record<string, unknown>;

  const action = raw.action;
  if (
    action !== "list_dir" &&
    action !== "read_file" &&
    action !== "write_file" &&
    action !== "delete_file"
  ) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "action must be one of: list_dir, read_file, write_file, delete_file"
    );
  }

  const path = raw.path;
  if (typeof path !== "string" || path.trim().length === 0 || path.length > 2048) {
    throw new HttpError(400, "VALIDATION_ERROR", "path must be a non-empty string");
  }

  let content = "";
  if (action === "write_file") {
    if (typeof raw.content !== "string") {
      throw new HttpError(400, "VALIDATION_ERROR", "write_file requires a string 'content'");
    }
    content = raw.content;
    if (Buffer.byteLength(content, "utf8") > MAX_WRITE_BYTES) {
      throw new HttpError(
        400,
        "FILE_TOO_LARGE",
        `content exceeds the ${MAX_WRITE_BYTES}-byte write limit`
      );
    }
  }

  // A non-boolean 'backup' (e.g. the string "true", or 1) must NOT silently
  // disable the rollback copy — backups are the safety net for every overwrite.
  if (raw.backup !== undefined && typeof raw.backup !== "boolean") {
    throw new HttpError(400, "VALIDATION_ERROR", "backup must be a boolean");
  }
  const backup = raw.backup ?? true;

  return { action, path, content, backup };
}

/**
 * Maps the logical "/config/..." path a skill speaks to a real path inside the
 * mounted config root, and refuses everything that tries to leave it. Traversal
 * defense mirrors the /core path hardening: reject encoded tokens, decode
 * iteratively, then verify containment with realpath so a symlink cannot point
 * outside either.
 */
async function resolveTargetPath(configRoot: string, logicalPath: string, action: FileAction): Promise<string> {
  const decoded = decodeIterative(logicalPath);
  if (containsControlChars(decoded)) {
    throw new HttpError(400, "FILE_PATH_INVALID", "path contains control characters");
  }
  if (decoded.includes("\\")) {
    throw new HttpError(400, "FILE_PATH_INVALID", "path must use forward slashes");
  }
  if (!decoded.startsWith(`${LOGICAL_PREFIX}/`) && decoded !== LOGICAL_PREFIX) {
    throw new HttpError(400, "FILE_PATH_INVALID", `path must start with ${LOGICAL_PREFIX}/`);
  }

  const relativePart = decoded.slice(LOGICAL_PREFIX.length).replace(/^\/+/, "");
  const normalized = normalize(relativePart);
  if (normalized.startsWith("..") || normalized.split(sep).includes("..")) {
    throw new HttpError(400, "FILE_PATH_INVALID", "path must stay inside the config directory");
  }

  assertNotDenied(normalized);

  const absolute = resolve(join(configRoot, normalized));
  assertInsideRoot(configRoot, absolute);

  // realpath resolves symlinks: a link inside the config dir must not be a way
  // out of it. For write_file the target may not exist yet, so the deepest
  // existing ancestor is checked instead.
  const probe = action === "write_file" ? await deepestExisting(absolute) : absolute;
  try {
    const realRoot = await realpath(configRoot);
    const real = await realpath(probe);
    assertInsideRoot(realRoot, real);
    // Staying inside the root is NOT enough: a symlink with an innocent name
    // (/config/exposed) can point at an always-denied path inside the root
    // (/config/secrets.yaml, .storage/auth). The deny list was applied to the
    // requested NAME; it must be applied again to what the path actually
    // resolves to, or a symlink becomes a bypass.
    assertNotDenied(relative(realRoot, real));
  } catch (error) {
    if (isNotFound(error)) {
      throw new HttpError(404, "FILE_NOT_FOUND", `not found: ${logicalPath}`);
    }
    throw error;
  }

  return absolute;
}

function assertNotDenied(relativePath: string): void {
  const segments = relativePath.split(sep).filter(Boolean);
  for (const segment of segments) {
    if (DENIED_SEGMENTS.includes(segment)) {
      throw new HttpError(
        403,
        "FILE_PATH_DENIED",
        `'${segment}' is never accessible through the relay (credentials, Home Assistant's own state store, or binary data)`
      );
    }
  }
  const name = segments[segments.length - 1] ?? "";
  if (DENIED_PATTERNS.some((pattern) => pattern.test(name))) {
    throw new HttpError(
      403,
      "FILE_PATH_DENIED",
      `'${name}' is never accessible through the relay`
    );
  }
}

// Containment is the last line of defense: whatever the path looked like, the
// resolved target must be the root itself or live under it.
function assertInsideRoot(root: string, candidate: string): void {
  const rel = relative(resolve(root), resolve(candidate));
  if (rel === "") {
    return;
  }
  if (rel.startsWith("..") || isAbsolute(rel)) {
    throw new HttpError(400, "FILE_PATH_INVALID", "path must stay inside the config directory");
  }
}

async function deepestExisting(absolute: string): Promise<string> {
  let current = absolute;
  for (let i = 0; i < 64; i++) {
    try {
      await access(current, constants.F_OK);
      return current;
    } catch {
      const parent = dirname(current);
      if (parent === current) {
        return current;
      }
      current = parent;
    }
  }
  return current;
}

async function listDir(absolute: string, logicalPath: string): Promise<unknown> {
  const entries = await withNotFound(logicalPath, () => readdir(absolute, { withFileTypes: true }));
  const truncated = entries.length > MAX_DIR_ENTRIES;
  const shown = entries.slice(0, MAX_DIR_ENTRIES);

  const items = await Promise.all(
    shown.map(async (entry) => {
      const info = await stat(join(absolute, entry.name)).catch(() => null);
      return {
        name: entry.name,
        type: entry.isDirectory() ? "dir" : "file",
        size: info?.size ?? null
      };
    })
  );

  return truncated ? { entries: items, truncated: true } : { entries: items };
}

async function readTextFile(absolute: string, logicalPath: string): Promise<unknown> {
  const info = await withNotFound(logicalPath, () => stat(absolute));
  if (info.isDirectory()) {
    throw new HttpError(400, "FILE_IS_DIRECTORY", `${logicalPath} is a directory — use list_dir`);
  }
  if (info.size > MAX_READ_BYTES) {
    throw new HttpError(
      400,
      "FILE_TOO_LARGE",
      `${logicalPath} is ${info.size} bytes, above the ${MAX_READ_BYTES}-byte read limit`
    );
  }

  const buffer = await readFile(absolute);
  // Text only: a binary file would come back as mojibake, and the caller almost
  // certainly asked for the wrong path.
  if (buffer.includes(0)) {
    throw new HttpError(
      400,
      "FILE_NOT_TEXT",
      `${logicalPath} is not a UTF-8 text file — the relay only serves text through /files`
    );
  }

  return { content: buffer.toString("utf8"), size: info.size };
}

async function writeTextFile(
  absolute: string,
  logicalPath: string,
  content: string,
  backup: boolean
): Promise<unknown> {
  const existing = await stat(absolute).catch(() => null);
  if (existing?.isDirectory()) {
    throw new HttpError(400, "FILE_IS_DIRECTORY", `${logicalPath} is a directory`);
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
  }

  await mkdir(dirname(absolute), { recursive: true });
  // Write to a temp file and rename: a crash mid-write must never leave Home
  // Assistant with a half-written configuration file.
  const tempPath = `${absolute}.nova-tmp`;
  await removeIfPresent(tempPath);
  await writeFile(tempPath, content, { encoding: "utf8", flag: "wx" });
  await rename(tempPath, absolute);

  return {
    written: true,
    size: Buffer.byteLength(content, "utf8"),
    created: existing === null,
    backup: backupPath ? `${logicalPath}.bak` : null
  };
}

// Removes a derived path (.bak / .nova-tmp) if anything is there — including a
// symlink, in which case unlink removes the LINK, not whatever it points at.
async function removeIfPresent(path: string): Promise<void> {
  try {
    await unlink(path);
  } catch (error) {
    if (!isNotFound(error)) {
      throw error;
    }
  }
}

async function deleteFile(absolute: string, logicalPath: string): Promise<unknown> {
  const info = await withNotFound(logicalPath, () => stat(absolute));
  if (info.isDirectory()) {
    throw new HttpError(
      400,
      "FILE_IS_DIRECTORY",
      "the relay does not delete directories — remove files individually"
    );
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
    throw error;
  }
}

function isNotFound(error: unknown): boolean {
  return (error as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
}

// Mirrors the /core path hardening: control characters have no place in a path
// and are a classic way to smuggle something past a naive check.
function containsControlChars(value: string): boolean {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= 0x1f || code === 0x7f) {
      return true;
    }
  }
  return false;
}

function decodeIterative(value: string): string {
  let current = value;
  for (let i = 0; i < 5; i++) {
    let decoded: string;
    try {
      decoded = decodeURIComponent(current);
    } catch {
      throw new HttpError(400, "FILE_PATH_INVALID", "path is not valid percent-encoding");
    }
    if (decoded === current) {
      return current;
    }
    current = decoded;
  }
  throw new HttpError(400, "FILE_PATH_INVALID", "path is encoded too many times");
}
