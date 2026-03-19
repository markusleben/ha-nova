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
const flowSchemasDoc = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/helper-flow-schemas.md"),
  "utf-8",
);
const architectureDoc = readFileSync(
  resolve(__dirname, "../../docs/reference/skill-architecture.md"),
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

  describe("config-entry foundation", () => {
    it("documents the two-family helper split", () => {
      expect(skillDoc).toContain("Storage-based family");
      expect(skillDoc).toContain("Config-entry family (PR1 foundation)");
      expect(architectureDoc).toContain("two explicit helper families");
      expect(architectureDoc).toContain("Config-entry family (PR1 foundation)");
      expect(architectureDoc).toContain("metadata only");
    });

    it("references the dedicated config-entry flow schema doc", () => {
      expect(skillDoc).toContain("helper-flow-schemas.md");
      expect(flowSchemasDoc).toContain("PR1 foundation");
      expect(flowSchemasDoc).toContain("Canonical write identity: `entry_id`");
    });

    it("limits the foundation slice to six single-step config-entry domains", () => {
      for (const domain of [
        "utility_meter",
        "derivative",
        "integration",
        "min_max",
        "threshold",
        "tod",
      ]) {
        expect(skillDoc).toContain(domain);
        expect(flowSchemasDoc).toContain(domain);
      }

      expect(skillDoc).toContain("does **not** support update yet");
      expect(skillDoc).toContain("Not supported in this PR1 slice");
    });

    it("keeps config-entry read scope metadata-only in this slice", () => {
      expect(skillDoc).toContain("Read is metadata-only in this slice");
      expect(skillDoc).toContain("Do not claim full domain-specific config readback");
      expect(skillDoc).toContain("Config-entry state:");
      expect(skillDoc).toContain("Read scope:");
      expect(skillDoc).toContain("metadata only in this slice");
      expect(flowSchemasDoc).toContain("observed field inventory");
      expect(flowSchemasDoc).toContain("not a complete validation schema");
      expect(flowSchemasDoc).toContain("Canonical list/metadata-read item");
    });

    it("uses config-entry-first verification for the config-entry family", () => {
      expect(skillDoc).toContain("config-entry layer first");
      expect(skillDoc).toContain("Capture a pre-create baseline");
      expect(skillDoc).toContain("<entries-before-file>");
      expect(skillDoc).toContain("re-read `config_entries/get` into `<entries-after-file>`");
      expect(skillDoc).toContain("if the terminal flow result includes `entry_id`");
      expect(skillDoc).toContain("if the terminal flow result omits `entry_id`");
      expect(skillDoc).toContain("diff `config_entries/get` before vs after by `entry_id`");
      expect(skillDoc).toContain("exactly one new `entry_id` appeared");
      expect(skillDoc).toContain("metadata is consistent with the requested create");
      expect(skillDoc).toContain("ambiguous create verification");
      expect(skillDoc).toContain("fallback tie-breakers only");
      expect(skillDoc).toContain("Entity disappearance is secondary evidence only");
      expect(flowSchemasDoc).toContain("Verification source of truth");
      expect(flowSchemasDoc).toContain("terminal flow result confirmed in the after-read");
      expect(flowSchemasDoc).toContain("before/after `entry_id` diff");
      expect(flowSchemasDoc).toContain("pre-create `config_entries/get` baseline");
      expect(flowSchemasDoc).toContain("exactly one new `entry_id` appeared");
      expect(flowSchemasDoc).toContain("ambiguous create verification");
      expect(flowSchemasDoc).toContain("entry absent in `config_entries/get`");
    });

    it("splits flow-start and flow-submit payload contracts", () => {
      expect(skillDoc).toContain("<start-payload-file>");
      expect(skillDoc).toContain("<submit-payload-file>");
      expect(skillDoc).toContain("handler-start body only");
      expect(skillDoc).toContain("extract `flow_id` before continuing");
      expect(skillDoc).toContain("form fields only");
      expect(flowSchemasDoc).toContain("capture `flow_id` from the start response");
      expect(flowSchemasDoc).toContain("handler-start body");
      expect(flowSchemasDoc).toContain("form-submit body");
    });
  });
});
