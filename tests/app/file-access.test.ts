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
});
