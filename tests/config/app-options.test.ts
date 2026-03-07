import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { readAppOptions } from "../../nova/src/config/app-options.js";

describe("readAppOptions", () => {
  it("returns parsed options when file exists", () => {
    const dir = mkdtempSync(join(tmpdir(), "ha-nova-options-"));
    const file = join(dir, "options.json");
    writeFileSync(file, JSON.stringify({ ha_llat: "token", feature_flag: true }), "utf8");

    const result = readAppOptions(file);

    expect(result).toEqual({
      ha_llat: "token",
      feature_flag: true
    });
  });

  it("returns empty object when file does not exist", () => {
    const result = readAppOptions("/tmp/ha-nova-non-existent-options.json");

    expect(result).toEqual({});
  });

  it("throws on invalid json", () => {
    const dir = mkdtempSync(join(tmpdir(), "ha-nova-options-"));
    const file = join(dir, "options.json");
    writeFileSync(file, "{ broken-json", "utf8");

    expect(() => readAppOptions(file)).toThrowError(
      `Failed to read app options from '${file}':`
    );
  });
});
