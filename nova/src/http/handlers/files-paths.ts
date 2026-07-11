import { access, realpath } from "node:fs/promises";
import { constants } from "node:fs";
import { basename, dirname, isAbsolute, join, normalize, relative, resolve, sep } from "node:path";

import { HttpError } from "../errors.js";

export type FileAction = "list_dir" | "read_file" | "write_file" | "delete_file";

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
  // entirely, and not one this endpoint exists to grant.
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
export async function resolveTargetPath(configRoot: string, logicalPath: string, action: FileAction): Promise<string> {
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

  // A delete removes the LINK itself, never what it points at — so the final
  // component must NOT be resolved: a dangling link, or one pointing outside
  // the root or at a denied path, must still be removable, and unlinking it
  // touches nothing else.
  //
  // The DIRECTORIES on the way there are a different matter: they are followed
  // by unlink, so /config/escape-dir -> /tmp/outside would otherwise let a
  // delete reach /tmp/outside/loot.txt. The parent is therefore resolved and
  // checked exactly like any other path; only the leaf is left alone.
  if (action === "delete_file") {
    try {
      const realRoot = await realpath(configRoot);
      const realParent = await realpath(dirname(absolute));
      assertInsideRoot(realRoot, realParent);
      assertNotDenied(relative(realRoot, realParent));
      return join(realParent, basename(absolute));
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
    assertNotDenied(relative(realRoot, real));

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
      return real;
    }
  } catch (error) {
    if (isNotFound(error)) {
      throw new HttpError(404, "FILE_NOT_FOUND", `not found: ${logicalPath}`);
    }
    throw error;
  }

  return absolute;
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
