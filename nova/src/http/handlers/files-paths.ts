import { access, readdir, realpath, stat } from "node:fs/promises";
import { constants } from "node:fs";
import { basename, dirname, isAbsolute, join, normalize, relative, resolve, sep } from "node:path";

import { HttpError } from "../errors.js";

export type FileAction = "list_dir" | "read_file" | "write_file" | "delete_file" | "restore_file" | "list_deleted";

// What a resolved request points at: its real path, its logical path after
// decoding and normalization, and whether it lies in the www area.
export interface ResolvedTarget {
  absolute: string;
  logical: string;
  www: boolean;
}

// www is served to every browser, so nothing is ever written there. Its
// images and videos may be listed, deleted with a kept copy, and put back
// (files-www.ts); community holds HACS's frontend code and stays denied.
const WWW_ACTIONS = new Set<FileAction>(["list_dir", "delete_file", "restore_file"]);

export const LOGICAL_PREFIX = "/config";

/**
 * Paths that are NEVER readable or writable, whatever the mode says. These hold
 * credentials, Home Assistant's own state store, or data that is meaningless to
 * an editor and dangerous to corrupt. The relay stays dumb — this is a
 * transport boundary, not domain logic.
 */
const DENIED_SEGMENTS = [
  ".storage",
  ".cloud",
  ".ssh",
  ".git",
  "deps",
  "ssl",
  "tts",
  "backups",
  // Home Assistant EXECUTES what lives here. custom_components holds Python that
  // runs in the HA process, python_scripts likewise, and www is served to every
  // browser session. Allowing writes there would turn "edit my YAML" into
  // arbitrary code execution on the user's home server — a different capability
  // entirely, and not one this endpoint exists to grant. Inside www only
  // WWW_ACTIONS pass (wwwArea, files-www.ts).
  "custom_components",
  "python_scripts",
  "www"
];

/**
 * Writes are additionally restricted to configuration formats. Reads are already
 * confined by the deny list, but a write is the dangerous direction: a file the
 * relay would never serve back can still be planted by name (a .sh a shell
 * command picks up, a .py dropped somewhere HA scans). The skill that uses this
 * endpoint edits YAML — nothing else needs to be writable.
 */
const WRITABLE_EXTENSIONS = [".yaml", ".yml", ".conf", ".json", ".txt", ".md"];

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
  // Any .log variant: Home Assistant itself writes home-assistant.log.fault,
  // rotation adds .log.1, and backups add .log.bak — an "ends with .log" rule
  // would serve every one of them.
  /\.log(\.|$)/i
];

/**
 * Maps the logical "/config/..." path a skill speaks to a real path inside the
 * mounted config root, and refuses everything that tries to leave it. Traversal
 * defense mirrors the /core path hardening: reject encoded tokens, decode
 * iteratively, then verify containment with realpath so a symlink cannot point
 * outside either.
 */
export async function resolveTargetPath(configRoot: string, logicalPath: string, action: FileAction): Promise<ResolvedTarget> {
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

  assertNotDenied(normalized, action);
  const logical = normalized === "." ? LOGICAL_PREFIX : `${LOGICAL_PREFIX}/${normalized.split(sep).join("/")}`;
  const named = normalized.split(sep)[0] === "www";

  const absolute = resolve(join(configRoot, normalized));
  assertInsideRoot(configRoot, absolute);

  // A delete removes the LINK itself, never what it points at — so the final
  // component must NOT be resolved: a dangling link, or one pointing outside
  // the root or at a denied path, must still be removable, and unlinking it
  // touches nothing else.
  //
  // The DIRECTORIES on the way there are a different matter: they are followed
  // by unlink, so /config/escape-dir -> /tmp/outside would otherwise let a
  // delete reach /tmp/outside/loot.txt. The parent is therefore resolved and
  // checked exactly like any other path; only the leaf is left alone.
  if (action === "delete_file" || action === "restore_file") {
    try {
      const realRoot = await realpath(configRoot);
      const realParent = await realpath(dirname(absolute));
      assertInsideRoot(realRoot, realParent);
      assertNotDenied(relative(realRoot, realParent), action);
      const www = (await wwwArea(realRoot, realParent, action)) || named;
      return { absolute: join(realParent, basename(absolute)), logical, www };
    } catch (error) {
      if (isNotFound(error)) {
        throw new HttpError(404, "FILE_NOT_FOUND", `not found: ${logicalPath}`);
      }
      throw error;
    }
  }

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
    assertNotDenied(relative(realRoot, real), action);
    const www = (await wwwArea(realRoot, real, action)) || named;

    // A write to an EXISTING path goes through the link to its real target: a
    // rename would otherwise replace the symlink with a regular file, silently
    // breaking the user's structure while leaving the real file stale — and the
    // read side already followed the link, so the flow would be incoherent.
    // The target was just validated above. Deletes deliberately do NOT do this:
    // removing a link must remove the link, not what it points at.
    if (action === "write_file" && probe === absolute) {
      // The extension guard ran against the requested NAME. A link called
      // safe.yaml can point at publish.sh, and writing through it would hand
      // the caller the executable file the guard exists to protect — the same
      // laundering the deny list is re-checked for, one line above. Re-check it
      // against what the path really resolves to.
      assertWritableExtension(real);
      return { absolute: real, logical, www };
    }
  } catch (error) {
    if (isNotFound(error)) {
      throw new HttpError(404, "FILE_NOT_FOUND", `not found: ${logicalPath}`);
    }
    throw error;
  }

  return { absolute, logical, www: named };
}

// By identity, not by name: a symlink or a bind mount can show www, or its
// community folder, under another path. Every folder from real up is compared
// with <root>/www and <root>/www/community by device and inode. community is
// denied for every action; www only for those outside WWW_ACTIONS. True when
// real lies in www.
async function wwwArea(realRoot: string, real: string, action: FileAction): Promise<boolean> {
  const id = (info: { dev: number; ino: number } | null): string | null => (info === null ? null : `${info.dev}:${info.ino}`);
  const www = id(await stat(join(realRoot, "www")).catch(() => null));
  if (www === null) {
    return false;
  }
  const community = id(await stat(join(realRoot, "www", "community")).catch(() => null));
  let inside = false;
  for (let dir = real; ; dir = dirname(dir)) {
    const here = id(await stat(dir).catch(() => null));
    if (here !== null && here === community) {
      throw deniedSegment("community");
    }
    inside ||= here === www;
    if (dirname(dir) === dir) {
      break;
    }
  }
  if (inside && !WWW_ACTIONS.has(action)) {
    throw deniedSegment("www");
  }
  return inside;
}
/**
 * The Relay's data folder (the phone link, the safety check's credentials) is
 * never reached through /files, by any name: a bind mount can show it inside
 * the configuration folder under another path, so every folder from the target
 * up to the root is compared by identity, not by name. A data folder that
 * can't be read refuses.
 */
export async function assertOutsideData(target: string, dataDir: string | undefined): Promise<void> {
  if (dataDir === undefined) {
    return;
  }
  const denied = new HttpError(403, "FILE_PATH_DENIED", "the Relay's own data folder is never readable or writable");
  const names = await readdir(dataDir).catch(() => null);
  const data = await stat(dataDir).catch(() => null);
  if (names === null || data === null) {
    throw denied;
  }
  const entries = await Promise.all(names.map((name) => stat(join(dataDir, name)).catch(() => null)));
  const guarded = new Set([data, ...entries].flatMap((info) => (info === null ? [] : [info.dev + ":" + info.ino])));
  const real = await realpath(await deepestExisting(target)).catch(() => null);
  if (real === null) {
    throw denied;
  }
  for (let dir = real; ; dir = dirname(dir)) {
    const info = await stat(dir).catch(() => null);
    if (info !== null && guarded.has(info.dev + ":" + info.ino)) {
      throw denied;
    }
    if (dirname(dir) === dir) {
      return;
    }
  }
}

export function assertWritableExtension(logicalPath: string): void {
  const name = logicalPath.split("/").pop() ?? "";
  const dot = name.lastIndexOf(".");
  const extension = dot > 0 ? name.slice(dot).toLowerCase() : "";
  if (!WRITABLE_EXTENSIONS.includes(extension)) {
    throw new HttpError(
      403,
      "FILE_TYPE_DENIED",
      `the relay only writes configuration files (${WRITABLE_EXTENSIONS.join(", ")}) — refusing to write '${name}'`
    );
  }
}

function assertNotDenied(relativePath: string, action: FileAction): void {
  const segments = relativePath.split(sep).filter(Boolean);
  if (segments[0] === "www" && segments[1]?.toLowerCase() === "community") {
    throw deniedSegment("community");
  }
  const media = segments[0] === "www" && WWW_ACTIONS.has(action);
  for (const segment of segments) {
    if (DENIED_SEGMENTS.includes(segment) && !(media && segment === "www")) {
      throw deniedSegment(segment);
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

function deniedSegment(segment: string): HttpError {
  const why: Record<string, string> = {
    www: "'www' is served to every browser: the relay lists it and deletes its images and videos with a kept copy, but never reads or writes there",
    community: "'www/community' holds HACS's frontend cards and is never accessible through the relay"
  };
  return new HttpError(
    403,
    "FILE_PATH_DENIED",
    why[segment] ?? `'${segment}' is never accessible through the relay (credentials, Home Assistant's own state store, or binary data)`
  );
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
export function isNotFound(error: unknown): boolean {
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
