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

  describe("config-entry helper family", () => {
    it("uses the shared terminal-friendly preview shape for helper writes", () => {
      expect(skillDoc).toContain("shared write-preview shape");
      expect(skillDoc).toContain("Changes slot");
      expect(skillDoc).toContain("explicit not-saved-yet line");
      expect(skillDoc).toContain("Options block (`apply`, `show yaml`, `cancel`)");
      expect(skillDoc).toContain("explicit not-deleted-yet line before the confirmation token");
      expect(skillDoc).not.toContain("as a `## Changes` diff");
    });

    it("binds helper writes to active-preview confirmation", () => {
      expect(skillDoc).toContain("Active Preview Confirmation");
      expect(skillDoc).toContain("bound to this exact preview");
      expect(skillDoc).toContain("Destructive cleanup still requires `confirm:<token>`");
    });

    it("documents the two-family helper split", () => {
      expect(skillDoc).toContain("Storage-based family");
      expect(skillDoc).toContain("Config-entry family");
      expect(architectureDoc).toContain("two explicit helper families");
      expect(architectureDoc).toContain("Config-entry family");
      expect(architectureDoc).toContain("current editable options snapshot");
    });

    it("references the dedicated config-entry flow schema doc", () => {
      expect(skillDoc).toContain("helper-flow-schemas.md");
      expect(flowSchemasDoc).toContain("full supported config-entry family (10 domains)");
      expect(flowSchemasDoc).toContain("Canonical write identity: `entry_id`");
    });

    it("documents full config-entry helper ownership for the 10 supported domains", () => {
      for (const domain of [
        "utility_meter",
        "derivative",
        "integration",
        "min_max",
        "threshold",
        "tod",
        "statistics",
        "group",
        "history_stats",
        "template",
      ]) {
        expect(skillDoc).toContain(domain);
        expect(flowSchemasDoc).toContain(domain);
      }

      expect(skillDoc).toContain("CRUD support for 10 domains:");
      expect(skillDoc).toContain("verified for the `sensor` subtype");
      expect(skillDoc).not.toContain("does **not** support update yet");
      // Template authoring safety: broken templates render entities unavailable.
      expect(skillDoc).toContain("skills/ha-nova/template-guidelines.md");
      expect(skillDoc).toContain("post-write verification must read the rendered state");
      expect(flowSchemasDoc).toContain("`name` is NOT editable via the options flow");
      // Menu-flow safety machinery covers template like group (16 unverified subtypes).
      expect(skillDoc).toContain("for unobserved `group` or `template` subtypes, this first confirmation authorizes only the non-persisting menu-step submit");
      expect(skillDoc).toContain("for any other `group` or `template` subtype, plan only the menu choice");
      // Pre-create resolution of entities inside the state template.
      expect(skillDoc).toContain("an unknown reference is a blocking question, not a submit");
      // Rendered-state verification is operationalized, not just promised.
      expect(skillDoc).toContain("report it as a template defect while the config-entry write itself still counts as passed");
    });

    it("uses options-flow snapshots for config-entry readback", () => {
      expect(skillDoc).toContain("start an options flow");
      expect(skillDoc).toContain("current editable options snapshot");
      expect(skillDoc).toContain("Supports options-flow editing:");
      expect(skillDoc).toContain("Supports Options-Flow Editing");
      expect(skillDoc).toContain("Current flow step:");
      expect(skillDoc).toContain("Current editable fields:");
      expect(skillDoc).toContain("mark its value as unavailable instead of guessing");
      expect(skillDoc).toContain("Config-entry state:");
      expect(flowSchemasDoc).toContain("observed field inventory");
      expect(flowSchemasDoc).toContain("not a complete validation schema");
      expect(flowSchemasDoc).toContain("Current editable options readback");
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
      expect(skillDoc).toContain("reopen the options flow");
      expect(skillDoc).toContain("description.suggested_value");
      expect(flowSchemasDoc).toContain("Verification source of truth");
      expect(flowSchemasDoc).toContain("terminal flow result confirmed in the after-read");
      expect(flowSchemasDoc).toContain("before/after `entry_id` diff");
      expect(flowSchemasDoc).toContain("pre-create `config_entries/get` baseline");
      expect(flowSchemasDoc).toContain("exactly one new `entry_id` appeared");
      expect(flowSchemasDoc).toContain("ambiguous create verification");
      expect(flowSchemasDoc).toContain("entry absent in `config_entries/get`");
      expect(flowSchemasDoc).toContain("reopened options flow shows the requested changed fields");
    });

    it("enforces the config-entry helper allowlist before delete", () => {
      expect(skillDoc).toContain("Resolve target to the canonical config-entry helper item");
      expect(skillDoc).toContain("Enforce the helper-domain allowlist before any delete");
      expect(skillDoc).toContain("allowed here: `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`");
      expect(skillDoc).toContain("do not call `DELETE /api/config/config_entries/entry/{entry_id}` for out-of-scope domains");
      expect(skillDoc).toContain("hand off to `ha-nova:fallback` for any other config-entry domain");
      expect(skillDoc).toContain("`entry_id` only when needed to disambiguate duplicate titles/domains");
      expect(skillDoc).not.toContain("unsupported `group` subtype path");
    });

    it("splits flow-start and flow-submit payload contracts", () => {
      expect(skillDoc).toContain("<start-payload-file>");
      expect(skillDoc).toContain("<submit-payload-file>");
      expect(skillDoc).toContain("handler-start body only");
      expect(skillDoc).toContain("extract `flow_id` before continuing");
      expect(skillDoc).toContain("fail loud if the start response did not return `flow_id`");
      expect(skillDoc).toContain("form fields only");
      expect(flowSchemasDoc).toContain("capture `flow_id` from the start response");
      expect(flowSchemasDoc).toContain("handler-start body");
      expect(flowSchemasDoc).toContain("form-submit body");
    });

    it("documents multi-step create flows and required-field carry-forward for updates", () => {
      expect(skillDoc).toContain("for `group` or `template` with subtype `sensor`, include the required `next_step_id` menu choice and the observed final form");
      expect(skillDoc).toContain("for any other `group` or `template` subtype, plan only the menu choice before the flow starts");
      expect(flowSchemasDoc).toContain("end-to-end CRUD was proven locally for the `sensor` subtype only");
      expect(skillDoc).toContain("for `statistics` and `history_stats`, prepare every later step body before preview");
      expect(skillDoc).toContain("for unobserved `group` or `template` subtypes, say that the final subtype form will be previewed after the menu step returns live fields");
      expect(skillDoc).toContain("this first confirmation authorizes only the non-persisting menu-step submit");
      expect(skillDoc).toContain("if that menu step leads to an unobserved `group` or `template` subtype form, stop and preview the live subtype fields before the terminal submit");
      expect(skillDoc).toContain("ask for a second natural confirmation before sending the terminal subtype-specific payload");
      expect(skillDoc).toContain("carry forward unchanged required fields");
      expect(skillDoc).toContain("fail loud as unsupported update for that field on this HA version");
      expect(skillDoc).toContain("do not silently ignore non-exposed requested fields");
      expect(skillDoc).toContain("fail loud instead of guessing its current value");
      expect(skillDoc).toContain("do not submit read-only fields");
      expect(skillDoc).toContain("if the start response explicitly shows that options editing is unsupported");
      expect(skillDoc).toContain("surface that relay/HA error directly instead of relabeling it as unsupported");
      expect(skillDoc).toContain("two-key window invariant");
      expect(skillDoc).toContain("drop the old third key explicitly");
      expect(skillDoc).toContain("fail loud as unverifiable update on this HA version");
      expect(flowSchemasDoc).toContain("submit `next_step_id`");
      expect(flowSchemasDoc).toContain("step_id: state_characteristic");
      expect(flowSchemasDoc).toContain("step_id: state");
      expect(flowSchemasDoc).toContain("treat the current value as unavailable instead of guessed");
      expect(flowSchemasDoc).toContain("fail loud instead of guessing the current value");
      expect(flowSchemasDoc).toContain("if the user requests a non-exposed field, fail loud as unsupported update for that field on this HA version");
      expect(flowSchemasDoc).toContain("two-key window invariant across `start`, `end`, and `duration`");
      expect(flowSchemasDoc).toContain("drop the previous third key explicitly before submit");
      expect(flowSchemasDoc).toContain("fail loud as unverifiable update on this HA version");
    });

    it("adds ambiguity and dependency guards to config-entry update/delete flows", () => {
      expect(skillDoc).toContain("if multiple candidates remain after resolution, stop and ask one blocking question");
      expect(skillDoc).toContain("never guess between duplicate titles or ambiguous linked-entity matches");
      expect(skillDoc).toContain("Run a pre-delete dependency check");
      expect(skillDoc).toContain("run `search/related` against up to 3 linked entities before confirmation");
      expect(skillDoc).toContain("dependency check coverage is limited");
    });

    it("pins the fallback ownership boundary against reintroducing helper-owned domains", () => {
      const fallbackDoc = readFileSync(
        resolve(__dirname, "../../skills/fallback/SKILL.md"),
        "utf-8",
      );
      const supportedTypesLine = fallbackDoc
        .split("\n")
        .find((line) => line.includes("Supported types in this fallback section:"));

      expect(supportedTypesLine).toBeDefined();
      expect(supportedTypesLine).toContain("`trend`");
      expect(supportedTypesLine).toContain("`random`");
      expect(supportedTypesLine).toContain("`filter`");
      expect(supportedTypesLine).toContain("`generic_thermostat`");
      expect(supportedTypesLine).toContain("`switch_as_x`");
      expect(supportedTypesLine).toContain("`generic_hygrostat`");
      expect(supportedTypesLine).not.toContain("`utility_meter`");
      expect(supportedTypesLine).not.toContain("`derivative`");
      expect(supportedTypesLine).not.toContain("`integration`");
      expect(supportedTypesLine).not.toContain("`min_max`");
      expect(supportedTypesLine).not.toContain("`threshold`");
      expect(supportedTypesLine).not.toContain("`tod`");
      expect(supportedTypesLine).not.toContain("`statistics`");
      expect(supportedTypesLine).not.toContain("`group`");
      expect(supportedTypesLine).not.toContain("`history_stats`");
      expect(supportedTypesLine).not.toContain("`template`");
      expect(fallbackDoc).toContain("Delete unsupported config-entry helper by entry_id");
    });
  });
});
