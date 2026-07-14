import { lstat, mkdir, readdir, readFile, realpath, rm, stat, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { promisify } from "node:util";
import { gunzip, gzip } from "node:zlib";

import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

const gzipAsync = promisify(gzip);
const gunzipAsync = promisify(gunzip);

export const MAX_SNAPSHOT_FILES = 500;
export const MAX_TOTAL_BYTES = 50 * 1024 * 1024;
export const AUTO_NAME_PREFIX = "auto-";
const DEFAULT_PRUNE_MAX_AGE_DAYS = 30;
const DEFAULT_PRUNE_MAX_FILES = 100;
// Ten years: far above any sensible retention, far below the Date range a
// huge value would blow past (turning a validation issue into a 500).
const MAX_PRUNE_AGE_DAYS = 3650;
const DAY_MS = 24 * 60 * 60 * 1000;

const SLUG_PATTERN = /^[a-z0-9-]{1,64}$/;
// The only addressable shape: <category>/<name>-<stamp>.json.gz — everything
// else is rejected before any filesystem call, which is the containment story
// (no free-form paths ever reach resolve()).
const FILE_PATTERN = /^([a-z0-9-]{1,64})\/([a-z0-9-]{1,64})-(\d{8}T\d{9}Z)\.json\.gz$/;

export interface BackupsHandlerOptions {
  snapshotRoot: string;
  now?: () => number;
}

interface SnapshotEntry {
  file: string;
  category: string;
  name: string;
  bytes: number;
  created_at: string;
}

/**
 * Generic config-snapshot blob store. The relay never parses the stored data —
 * opaque JSON in, opaque JSON out; which items get captured, diffed, and
 * restored is entirely the skills' business (docs/work/2026-07-14-config-snapshots-spec.md).
 */
export function createBackupsHandler(options: BackupsHandlerOptions): RouteHandler {
  const now = options.now ?? Date.now;
  return async ({ body }: RouteContext) => {
    const request = parseBackupsRequest(body);
    switch (request.action) {
      case "save":
        return await saveSnapshot(options.snapshotRoot, request.category, request.name, request.data, now);
      case "load":
        return await loadSnapshot(options.snapshotRoot, request.file);
      case "list":
        return await listSnapshots(options.snapshotRoot, request.category);
      case "delete":
        return await deleteSnapshot(options.snapshotRoot, request.file);
      case "prune":
        return await pruneSnapshots(options.snapshotRoot, request, now);
    }
  };
}

type BackupsRequest =
  | { action: "save"; category: string; name: string; data: unknown }
  | { action: "load"; file: string }
  | { action: "list"; category?: string }
  | { action: "delete"; file: string }
  | {
      action: "prune";
      category?: string;
      maxAgeDays: number;
      maxFiles: number;
      keepNamed: boolean;
    };

function parseBackupsRequest(body: unknown): BackupsRequest {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must be an object");
  }
  const raw = body as Record<string, unknown>;
  const action = raw.action;

  switch (action) {
    case "save": {
      const category = parseSlug(raw.category, "category");
      const name = parseSlug(raw.name, "name");
      if (!("data" in raw) || raw.data === undefined) {
        throw new HttpError(400, "VALIDATION_ERROR", "save requires a 'data' JSON value");
      }
      return { action, category, name, data: raw.data };
    }
    case "load":
    case "delete":
      return { action, file: parseFileRef(raw.file) };
    case "list":
      return {
        action,
        ...(raw.category !== undefined ? { category: parseSlug(raw.category, "category") } : {})
      };
    case "prune": {
      const maxAgeDays = parsePositiveInt(raw.max_age_days, "max_age_days", DEFAULT_PRUNE_MAX_AGE_DAYS, MAX_PRUNE_AGE_DAYS);
      const maxFiles = parsePositiveInt(raw.max_files, "max_files", DEFAULT_PRUNE_MAX_FILES, MAX_SNAPSHOT_FILES);
      if (raw.keep_named !== undefined && typeof raw.keep_named !== "boolean") {
        throw new HttpError(400, "VALIDATION_ERROR", "keep_named must be a boolean");
      }
      return {
        action,
        maxAgeDays,
        maxFiles,
        keepNamed: raw.keep_named ?? true,
        ...(raw.category !== undefined ? { category: parseSlug(raw.category, "category") } : {})
      };
    }
    default:
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        "action must be one of: save, load, list, delete, prune"
      );
  }
}

function parseSlug(input: unknown, field: string): string {
  if (typeof input !== "string" || !SLUG_PATTERN.test(input)) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      `${field} must be a lowercase slug ([a-z0-9-], max 64 chars)`
    );
  }
  return input;
}

function parseFileRef(input: unknown): string {
  if (typeof input !== "string" || !FILE_PATTERN.test(input)) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "file must be a snapshot reference of the form <category>/<name>-<stamp>.json.gz"
    );
  }
  return input;
}

function parsePositiveInt(input: unknown, field: string, fallback: number, max: number): number {
  if (input === undefined) {
    return fallback;
  }
  if (typeof input !== "number" || !Number.isInteger(input) || input < 1 || input > max) {
    throw new HttpError(400, "VALIDATION_ERROR", `${field} must be an integer between 1 and ${max}`);
  }
  return input;
}

function snapshotPath(root: string, file: string): string {
  // FILE_PATTERN already excludes separators/dots beyond the fixed shape;
  // the resolve() check is defense in depth, mirroring files-paths.ts.
  const absolute = resolve(join(root, file));
  const rootAbsolute = resolve(root);
  if (absolute !== rootAbsolute && !absolute.startsWith(`${rootAbsolute}/`)) {
    throw new HttpError(400, "VALIDATION_ERROR", "file must stay inside the snapshot store");
  }
  return absolute;
}

// The lexical check above cannot see symlinks: a symlinked category dir on a
// restored/mounted volume would pass it and route the fs call outside the
// store. Resolve the file's parent with realpath and require it to sit under
// the real root. A missing parent is fine — the later fs call yields ENOENT,
// which the callers map to 404 (or mkdir it first, for save).
async function assertRealParent(root: string, absolute: string): Promise<void> {
  let realRoot: string;
  let realParent: string;
  try {
    realRoot = await realpath(resolve(root));
    realParent = await realpath(dirname(absolute));
  } catch (error) {
    if (isEnoent(error)) {
      return;
    }
    throw error;
  }
  if (realParent !== realRoot && !realParent.startsWith(`${realRoot}/`)) {
    throw new HttpError(400, "VALIDATION_ERROR", "file must stay inside the snapshot store");
  }
  // A pre-existing LEAF symlink would still route readFile/rm outside the
  // store even with a clean parent — refuse it outright.
  try {
    const leaf = await lstat(absolute);
    if (leaf.isSymbolicLink()) {
      throw new HttpError(400, "VALIDATION_ERROR", "file must stay inside the snapshot store");
    }
  } catch (error) {
    if (error instanceof HttpError) {
      throw error;
    }
    if (!isEnoent(error)) {
      throw error;
    }
  }
}

function formatStamp(ms: number): string {
  // 20260714T123456789Z — sortable, path-safe, millisecond precision.
  const iso = new Date(ms).toISOString(); // 2026-07-14T12:34:56.789Z
  return iso.replace(/[-:.]/g, "");
}

function stampToIso(stamp: string): string {
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})(\d{3})Z$/.exec(stamp);
  if (!m) {
    return new Date(0).toISOString();
  }
  return `${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}.${m[7]}Z`;
}

async function collectEntries(root: string, category?: string): Promise<SnapshotEntry[]> {
  const entries: SnapshotEntry[] = [];
  let categories: string[];
  try {
    const dirents = await readdir(root, { withFileTypes: true });
    categories = dirents
      .filter((d) => d.isDirectory() && SLUG_PATTERN.test(d.name))
      .map((d) => d.name)
      .filter((name) => category === undefined || name === category);
  } catch (error) {
    if (isEnoent(error)) {
      return [];
    }
    throw error;
  }

  for (const cat of categories) {
    let files: string[];
    try {
      files = await readdir(join(root, cat));
    } catch (error) {
      if (isEnoent(error)) {
        continue;
      }
      throw error;
    }
    for (const fname of files) {
      const ref = `${cat}/${fname}`;
      const match = FILE_PATTERN.exec(ref);
      if (!match) {
        continue;
      }
      const info = await stat(join(root, cat, fname));
      entries.push({
        file: ref,
        category: cat,
        name: match[2] ?? "",
        bytes: info.size,
        created_at: stampToIso(match[3] ?? "")
      });
    }
  }

  entries.sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0));
  return entries;
}

async function saveSnapshot(
  root: string,
  category: string,
  name: string,
  data: unknown,
  now: () => number
): Promise<{ file: string; bytes: number; created_at: string }> {
  const compressed = await gzipAsync(Buffer.from(JSON.stringify(data), "utf8"));

  const existing = await collectEntries(root);
  const totalBytes = existing.reduce((sum, entry) => sum + entry.bytes, 0);
  if (existing.length + 1 > MAX_SNAPSHOT_FILES || totalBytes + compressed.length > MAX_TOTAL_BYTES) {
    throw new HttpError(
      400,
      "SNAPSHOT_STORE_FULL",
      `Snapshot store is at its cap (${MAX_SNAPSHOT_FILES} files / ${MAX_TOTAL_BYTES} bytes) — run the prune action first.`
    );
  }

  const stampMs = now();
  const stamp = formatStamp(stampMs);
  const file = `${category}/${name}-${stamp}.json.gz`;
  const absolute = snapshotPath(root, file);
  await mkdir(join(root, category), { recursive: true });
  await assertRealParent(root, absolute);
  try {
    await writeFile(absolute, compressed, { flag: "wx" });
  } catch (error) {
    if (isEexist(error)) {
      throw new HttpError(409, "SNAPSHOT_EXISTS", "A snapshot with this name landed in the same millisecond — retry.");
    }
    throw error;
  }
  return { file, bytes: compressed.length, created_at: stampToIso(stamp) };
}

async function loadSnapshot(
  root: string,
  file: string
): Promise<{ data: unknown; bytes: number; created_at: string }> {
  const absolute = snapshotPath(root, file);
  await assertRealParent(root, absolute);
  let compressed: Buffer;
  try {
    compressed = await readFile(absolute);
  } catch (error) {
    if (isEnoent(error)) {
      throw new HttpError(404, "SNAPSHOT_NOT_FOUND", `No snapshot at ${file}`);
    }
    throw error;
  }
  let data: unknown;
  try {
    const raw = await gunzipAsync(compressed);
    data = JSON.parse(raw.toString("utf8"));
  } catch {
    throw new HttpError(
      500,
      "SNAPSHOT_CORRUPT",
      `Snapshot ${file} could not be decompressed/parsed — delete it and take a fresh one.`
    );
  }
  const match = FILE_PATTERN.exec(file);
  return {
    data,
    bytes: compressed.length,
    created_at: stampToIso(match?.[3] ?? "")
  };
}

async function listSnapshots(root: string, category?: string): Promise<SnapshotEntry[]> {
  return await collectEntries(root, category);
}

async function deleteSnapshot(root: string, file: string): Promise<{ deleted: true }> {
  const absolute = snapshotPath(root, file);
  await assertRealParent(root, absolute);
  try {
    await rm(absolute);
  } catch (error) {
    if (isEnoent(error)) {
      throw new HttpError(404, "SNAPSHOT_NOT_FOUND", `No snapshot at ${file}`);
    }
    throw error;
  }
  return { deleted: true };
}

async function pruneSnapshots(
  root: string,
  request: { category?: string; maxAgeDays: number; maxFiles: number; keepNamed: boolean },
  now: () => number
): Promise<{ deleted: string[] }> {
  const entries = await collectEntries(root, request.category);
  const prunable = entries.filter(
    (entry) => !request.keepNamed || entry.name.startsWith(AUTO_NAME_PREFIX)
  );

  const cutoffIso = new Date(now() - request.maxAgeDays * DAY_MS).toISOString();
  const deleted: string[] = [];
  const kept: SnapshotEntry[] = [];
  for (const entry of prunable) {
    if (entry.created_at < cutoffIso) {
      deleted.push(entry.file);
    } else {
      kept.push(entry);
    }
  }
  // kept is newest-first (collectEntries sort); everything beyond max_files goes too.
  for (const entry of kept.slice(request.maxFiles)) {
    deleted.push(entry.file);
  }

  for (const file of deleted) {
    await rm(snapshotPath(root, file), { force: true });
  }
  return { deleted };
}

function isEnoent(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && (error as { code?: string }).code === "ENOENT");
}

function isEexist(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && (error as { code?: string }).code === "EEXIST");
}
