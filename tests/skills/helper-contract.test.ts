// tests/skills/helper-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const skillDoc = readFileSync(
  resolve(__dirname, "../../skills/helper/SKILL.md"),
  "utf-8",
);
const schemasDoc = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/helper-schemas.md"),
  "utf-8",
);

describe("helper contract", () => {
  describe("use-case defaults on create", () => {
    it("includes use-case defaults step in create flow", () => {
      expect(skillDoc).toContain("Use-case defaults");
      expect(skillDoc).toContain("create only, skip on update/delete");
    });

    it("references helper-schemas.md for default principles", () => {
      expect(skillDoc).toContain("helper-schemas.md");
      expect(skillDoc).toContain("Suggested Defaults");
    });

    it("limits suggestions to max 4 with grouping heuristic", () => {
      expect(skillDoc).toContain("max 4 as numbered list");
      expect(skillDoc).toContain("Group related fields into one item");
    });

    it("supports accept all, partial, or skip", () => {
      expect(skillDoc).toContain("Accept all");
      expect(skillDoc).toContain('pick by number');
      expect(skillDoc).toContain('"skip"');
    });

    it("merges accepted defaults before preview", () => {
      expect(skillDoc).toContain("merge into payload BEFORE preview");
    });

    it("silently skips when no defaults inferable", () => {
      expect(skillDoc).toContain("No useful defaults inferable");
      expect(skillDoc).toContain("silently skip");
    });
  });

  describe("helper-schemas suggested defaults", () => {
    it("documents principles per helper type", () => {
      expect(schemasDoc).toContain("Suggested Defaults");
      expect(schemasDoc).toContain("input_number");
      expect(schemasDoc).toContain("input_boolean");
      expect(schemasDoc).toContain("timer");
      expect(schemasDoc).toContain("counter");
      expect(schemasDoc).toContain("input_select");
    });

    it("uses correct HA field names", () => {
      expect(schemasDoc).toContain("unit_of_measurement");
      // counter uses minimum/maximum, NOT min/max
      expect(schemasDoc).toContain("minimum");
      expect(schemasDoc).toContain("maximum");
    });

    it("documents timer restore semantics", () => {
      expect(schemasDoc).toContain("restore: true");
      expect(schemasDoc).toContain("restore: false");
    });

    it("includes examples table", () => {
      expect(schemasDoc).toContain("Target temperature");
      expect(schemasDoc).toContain("Motion timeout");
    });
  });
});
