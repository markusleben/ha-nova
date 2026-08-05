import { mkdirSync, mkdtempSync, readdirSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  InsecureFileError,
  ensurePrivateDir,
  readPrivateFileSync,
  writeFileAtomicSync,
} from "../../nova/src/storage/atomic-file.js";

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-atomic-"));
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("atomic-file", () => {
  it("writes atomically and reads back the exact bytes", () => {
    const path = join(dir, "sub", "record.json");
    const payload = Buffer.from(JSON.stringify({ a: 1, b: "x".repeat(1000) }));
    writeFileAtomicSync(path, payload);
    expect(readPrivateFileSync(path, 1 << 20)?.equals(payload)).toBe(true);
  });

  it("creates the parent directory as owner-only (0700) and the file 0600", () => {
    const path = join(dir, "nested", "creds.bin");
    writeFileAtomicSync(path, "secret");
    expect(statSync(join(dir, "nested")).mode & 0o777).toBe(0o700);
    expect(statSync(path).mode & 0o777).toBe(0o600);
  });

  it("overwrites an existing file in place (no leftover temp files)", () => {
    const path = join(dir, "r.json");
    writeFileAtomicSync(path, "v1");
    writeFileAtomicSync(path, "v2");
    expect(readPrivateFileSync(path, 1 << 20)?.toString()).toBe("v2");
    expect(readdirSync(dir).filter((n) => n.includes(".tmp-"))).toEqual([]);
  });

  it("removes its temp file when the atomic write fails (no orphan snapshots)", () => {
    // Force the rename step to fail: the target "file" is a non-empty
    // directory. Retry loops on a persistently failing disk must not
    // accumulate one orphaned temp snapshot per attempt.
    const target = join(dir, "as-dir");
    mkdirSync(join(target, "sub"), { recursive: true });
    expect(() => writeFileAtomicSync(target, "x")).toThrow();
    expect(readdirSync(dir).filter((n) => n.includes(".tmp-"))).toEqual([]);
  });

  it("returns null for a missing file", () => {
    expect(readPrivateFileSync(join(dir, "nope"), 1 << 20)).toBeNull();
  });

  it("refuses to read a symlink (swap defense)", () => {
    const real = join(dir, "real.json");
    writeFileSync(real, "data");
    const link = join(dir, "link.json");
    symlinkSync(real, link);
    expect(() => readPrivateFileSync(link, 1 << 20)).toThrow(InsecureFileError);
  });

  it("refuses to read a non-regular file (directory)", () => {
    const d = join(dir, "adir");
    mkdirSync(d);
    expect(() => readPrivateFileSync(d, 1 << 20)).toThrow(InsecureFileError);
  });

  it("rejects a file larger than the cap", () => {
    const path = join(dir, "big.bin");
    writeFileAtomicSync(path, Buffer.alloc(2048, 7));
    expect(() => readPrivateFileSync(path, 1024)).toThrow(InsecureFileError);
  });

  it("ensurePrivateDir is idempotent and enforces 0700", () => {
    const d = join(dir, "x", "y");
    ensurePrivateDir(d);
    ensurePrivateDir(d);
    expect(statSync(d).mode & 0o777).toBe(0o700);
  });
});
