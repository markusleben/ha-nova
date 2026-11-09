import { mkdirSync, mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { resolveFileAccess, type RootProbe } from "../../nova/src/config/file-access.js";

const mounted: RootProbe = { isDirectory: true, readable: true, writable: true };
const readOnlyMount: RootProbe = { isDirectory: true, readable: true, writable: false };
const unreadableMount: RootProbe = { isDirectory: true, readable: false, writable: false };
const nothing: RootProbe = { isDirectory: false, readable: false, writable: false };

describe("file access resolution", () => {
  it("defaults to off when the option is unset", () => {
    const result = resolveFileAccess({}, () => mounted);
    expect(result.mode).toBe("off");
    expect(result.warnings).toEqual([]);
  });

  it("enables the requested mode when the mount supports it", () => {
    const result = resolveFileAccess({ mode: "readwrite" }, () => mounted);
    expect(result.mode).toBe("readwrite");
    expect(result.configRoot).toBe("/config");
    expect(result.warnings).toEqual([]);
  });

  // A bind mount can be read-only or owned by another UID. Reporting
  // "readwrite" and then failing on the first write with EACCES would be a lie
  // that surfaces at the worst possible moment.
  it("falls back to read-only when the mount is not writable, and says why", () => {
    const result = resolveFileAccess({ mode: "readwrite" }, () => readOnlyMount);
    expect(result.mode).toBe("read");
    expect(result.warnings.join(" ")).toContain("not writable");
  });

  it("stays off when the mount cannot even be read", () => {
    const result = resolveFileAccess({ mode: "read" }, () => unreadableMount);
    expect(result.mode).toBe("off");
    expect(result.warnings.join(" ")).toContain("not readable");
  });

  it("stays off when nothing is mounted, rather than claiming a mode it cannot serve", () => {
    const result = resolveFileAccess({ mode: "readwrite" }, () => nothing);
    expect(result.mode).toBe("off");
    expect(result.configRoot).toBe("");
    expect(result.warnings.join(" ")).toContain("no Home Assistant configuration directory is mounted");
  });

  it("rejects an invalid mode without echoing the value into the message", () => {
    expect(() => resolveFileAccess({ mode: "yes-please" }, () => mounted)).toThrow(
      "file_access must be one of: off, read, readwrite"
    );
    expect(() => resolveFileAccess({ mode: "yes-please" }, () => mounted)).not.toThrow(/yes-please/);
  });

  it("honours an explicit CONFIG_ROOT override", () => {
    const seen: string[] = [];
    const result = resolveFileAccess(
      { mode: "read", configRootOverride: "/custom/ha" },
      (path) => {
        seen.push(path);
        return mounted;
      }
    );
    expect(result.configRoot).toBe("/custom/ha");
    expect(seen).toEqual(["/custom/ha"]);
  });
  // The Relay's data folder holds the phone link and the safety check's
  // credentials: a configuration folder that is it, holds it, or lies in it
  // (through a symlink too) would hand both to /files.
  it("stays off when the configuration folder overlaps the Relay's data folder", () => {
    const dir = mkdtempSync(join(tmpdir(), "hn-fa-"));
    try {
      mkdirSync(join(dir, "data", "inner"), { recursive: true });
      mkdirSync(join(dir, "config"));
      symlinkSync(join(dir, "data"), join(dir, "alias"));
      const data = join(dir, "data");
      for (const root of [data, dir, join(data, "inner"), join(dir, "alias"), "/"]) {
        const result = resolveFileAccess({ mode: "readwrite", configRootOverride: root, dataDir: data }, () => mounted);
        expect([root, result.mode, result.configRoot]).toEqual([root, "off", ""]);
        expect(result.warnings.join(" ")).toContain("overlaps the Relay's own data folder");
      }
      expect(resolveFileAccess({ mode: "readwrite", configRootOverride: join(dir, "config"), dataDir: data }, () => mounted).mode).toBe("readwrite");
      expect(resolveFileAccess({ mode: "readwrite", configRootOverride: join(dir, "data-other"), dataDir: data }, () => mounted).mode).toBe("readwrite");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
