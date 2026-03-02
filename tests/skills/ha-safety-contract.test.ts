import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-safety contract", () => {
  it("allows deterministic auto-select for unique exact matches", () => {
    const safety = readFileSync("skills/ha-safety.md", "utf8");

    expect(safety).toContain("If there is exactly one exact match, auto-select it and continue.");
    expect(safety).toContain("If multiple candidates remain, present candidates and ask user to pick one.");
    expect(safety).toContain("confirm:<token>");
    expect(safety).toContain("Use binary gate wording:");
  });
});
