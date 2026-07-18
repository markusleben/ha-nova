import { randomBytes } from "node:crypto";
import {
  closeSync,
  constants,
  fstatSync,
  fsyncSync,
  lstatSync,
  mkdirSync,
  openSync,
  readSync,
  renameSync,
  statSync,
  writeSync,
  type Stats,
} from "node:fs";
import { dirname } from "node:path";

// Durable, tamper-resistant file primitives for the device registry and the TLS
// identity under /data. Unlike the file-transport writer (files-ops.ts), these
// fsync both the file and its directory so a crash right after the write cannot
// lose or truncate a credential record. All private files are owner-only (0600)
// inside an owner-only directory (0700), and reads refuse anything that is not a
// real regular file (symlink swaps and fifos are rejected via lstat + fstat).

export class InsecureFileError extends Error {}

const FILE_MODE = 0o600;
const DIR_MODE = 0o700;

// Ensures the parent directory exists and is a real directory. A directory we
// create is 0700; a pre-existing one (e.g. /data, often 0755 root-owned) keeps
// its mode — the 0600 files inside are the actual secret protection.
export function ensurePrivateDir(dir: string): void {
  mkdirSync(dir, { recursive: true, mode: DIR_MODE });
  const st = statSync(dir);
  if (!st.isDirectory()) {
    throw new InsecureFileError(`${dir} is not a directory`);
  }
}

// Atomic, durable write: a fresh temp file (exclusive create, 0600) is fully
// written and fsynced, then renamed over the target, then the directory entry is
// fsynced so the rename itself survives a power loss.
export function writeFileAtomicSync(path: string, data: Buffer | string): void {
  const dir = dirname(path);
  ensurePrivateDir(dir);
  const buffer = typeof data === "string" ? Buffer.from(data, "utf8") : data;
  // A random suffix (not pid+counter) avoids colliding with a temp file left by
  // a crash mid-write: after a restart the pid can repeat and a counter resets,
  // which would make the exclusive "wx" open fail with EEXIST and block writes.
  const tempPath = `${path}.tmp-${randomBytes(9).toString("hex")}`;
  const fd = openSync(tempPath, "wx", FILE_MODE);
  try {
    // Loop until every byte is written: a single writeSync may be short, and a
    // torn temp file must never be renamed into place (invariant: no partial
    // record).
    let written = 0;
    while (written < buffer.length) {
      written += writeSync(fd, buffer, written, buffer.length - written);
    }
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
  renameSync(tempPath, path);
  fsyncDir(dir);
}

// Reads a private file, refusing symlinks and non-regular files. Returns null
// when the file does not exist.
export function readPrivateFileSync(path: string, maxBytes: number): Buffer | null {
  const lst = lstatOrNull(path);
  if (lst === null) {
    return null;
  }
  if (lst.isSymbolicLink()) {
    throw new InsecureFileError(`${path} is a symlink; refusing to read`);
  }

  // O_NOFOLLOW closes the lstat->open TOCTOU window: if the path is swapped to a
  // symlink after the lstat check, the open itself refuses to follow it.
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const st = fstatSync(fd);
    if (!st.isFile()) {
      throw new InsecureFileError(`${path} is not a regular file`);
    }
    if (st.size > maxBytes) {
      throw new InsecureFileError(`${path} exceeds ${maxBytes} bytes`);
    }
    const buffer = Buffer.allocUnsafe(st.size);
    let offset = 0;
    while (offset < st.size) {
      const read = readSync(fd, buffer, offset, st.size - offset, offset);
      if (read <= 0) {
        break;
      }
      offset += read;
    }
    return buffer.subarray(0, offset);
  } finally {
    closeSync(fd);
  }
}

function fsyncDir(dir: string): void {
  // Directory fsync is what makes the rename durable. Some platforms refuse to
  // open a directory for read; the relay only runs on Linux/macOS, but we
  // degrade quietly rather than fail an otherwise-complete write.
  let dirFd: number | null = null;
  try {
    dirFd = openSync(dir, "r");
    fsyncSync(dirFd);
  } catch {
    // best effort
  } finally {
    if (dirFd !== null) {
      closeSync(dirFd);
    }
  }
}

function lstatOrNull(path: string): Stats | null {
  try {
    return lstatSync(path);
  } catch (error) {
    if (isEnoent(error)) {
      return null;
    }
    throw error;
  }
}

function isEnoent(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    (error as NodeJS.ErrnoException).code === "ENOENT"
  );
}
