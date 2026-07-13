import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const batchSafety = readFileSync("skills/ha-nova/batch-safety.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");

// Every skill that declares batch support must point at the shared contract.
const OPTED_IN_SKILLS = [
  "write",
  "helper",
  "mqtt",
  "maintenance",
  "scene",
  "dashboard",
  "todo",
  "organize",
];

// Every destructive-capable skill must appear in the capability matrix, so
// batch support is always an explicit declaration, never an omission.
const MATRIX_SKILLS = [
  ...OPTED_IN_SKILLS,
  "yaml-config",
  "energy",
  "service-call",
  "backup",
  "updates",
  "admin",
  "fallback",
];

describe("batch safety contract (issue #327)", () => {
  it("is referenced by the context skill, write-safety, output-rules, and every opted-in skill", () => {
    expect(contextSkill).toContain("skills/ha-nova/batch-safety.md");
    expect(writeSafety).toContain("skills/ha-nova/batch-safety.md");
    expect(outputRules).toContain("skills/ha-nova/batch-safety.md");
    for (const skill of OPTED_IN_SKILLS) {
      const content = readFileSync(`skills/${skill}/SKILL.md`, "utf8");
      expect(content, `skills/${skill}/SKILL.md must reference batch-safety.md`).toContain(
        "skills/ha-nova/batch-safety.md",
      );
    }
  });

  it("declares batch support explicitly per skill in the capability matrix", () => {
    expect(batchSafety).toContain("## Capability Matrix (v1)");
    for (const skill of MATRIX_SKILLS) {
      expect(batchSafety, `matrix row for ${skill}`).toMatch(new RegExp(`^\\| \`${skill}\` \\|`, "m"));
    }
    expect(batchSafety).toContain("Never infer batch support");
    expect(batchSafety).toContain("Single-target flows stay the default");
  });

  it("binds the confirmation code to the exact manifest and invalidates it on drift", () => {
    expect(batchSafety).toContain("confirm:batch-<operation>-<family>-<count>-<digest>");
    expect(batchSafety).toContain("confirm:batch-delete-automations-8-a1b2c3d4");
    expect(batchSafety).toContain(
      "Any change to a target, payload, endpoint, scope, dependency result, or\nexecution order expires the confirmation",
    );
    expect(batchSafety).toContain("typed verbatim, never a menu");
    expect(contextSkill).toContain("confirm:batch-<operation>-<family>-<count>-<digest>");
    // User-facing naming stays aligned with the output rules.
    expect(batchSafety).toContain('the code is called the "confirmation code" (localized), never a "token"');
  });

  it("rejects late-expanding selectors and mixed resource families", () => {
    expect(batchSafety).toContain(
      'No selector, wildcard, prefix, label, area, query, or "all matching"\nexpression may be expanded after confirmation',
    );
    expect(batchSafety).toContain("One resource family per manifest");
    expect(batchSafety).toContain("mixed resource families");
    expect(batchSafety).toContain("selectors evaluated only at execution time");
  });

  it("enforces caps with a mandatory count/cap preview line and refuse-and-split", () => {
    expect(batchSafety).toContain("The preview always shows `count / cap`");
    expect(batchSafety).toContain("Above the cap, refuse and split");
    expect(batchSafety).toContain("**20**");
    expect(batchSafety).toContain("**100**");
  });

  it("requires sequential fail-fast execution with a per-target-verified ledger", () => {
    expect(batchSafety).toContain("Fail fast on the first unexpected result");
    expect(batchSafety).toContain("the completion ledger derives from per-target verification");
    expect(batchSafety).toContain("Verify every completed target individually");
    expect(batchSafety).toContain("succeeded, failed, not attempted");
    expect(batchSafety).toContain("Never claim the batch is atomic");
    expect(batchSafety).toContain("Do not blindly retry timeouts; verify state first");
    expect(batchSafety).toContain("never continue silently");
    expect(batchSafety).toContain("half-applied state");
  });

  it("forces resume onto a fresh manifest with a fresh confirmation", () => {
    expect(batchSafety).toContain("Resume operates only on a new manifest for the remaining targets");
    expect(batchSafety).toContain("new preview and a new confirmation code");
  });

  it("keeps recovery gates intact without weakening the typed confirmation", () => {
    expect(batchSafety).toContain("proportional backup gate");
    expect(batchSafety).toContain("never weakens the typed confirmation requirement");
    expect(batchSafety).toContain("State plainly when there is no automatic rollback");
  });

  it("pins the batch cards to the shared card rules", () => {
    expect(batchSafety).toContain("**Batch Delete Card**");
    expect(batchSafety).toContain("**Batch Result Card**");
    expect(batchSafety).toContain("the code prompt is always the last line");
    expect(batchSafety).toContain("🗑️  Delete: 8 automations");
    expect(batchSafety).toContain("⚠️  Nothing deleted yet.");
    expect(outputRules).toContain("Destructive batch previews and results render the Batch Cards");
  });

  it("keeps the v1 exclusions single-target", () => {
    for (const exclusion of [
      "user or account deletion",
      "whole integration removal",
      "Home Assistant Core/OS updates",
      "backup deletion",
      "arbitrary service calls that actuate devices",
      "MQTT command/`set` topics",
    ]) {
      expect(batchSafety).toContain(exclusion);
    }
    // service-call's grouped manifest is a distinct non-destructive tier.
    expect(batchSafety).toContain("its grouped manifest is non-destructive and separate from this contract");
  });

  it("keeps the domain guards of the opted-in skills", () => {
    const mqtt = readFileSync("skills/mqtt/SKILL.md", "utf8");
    expect(mqtt).toContain("topics taken only from `mqtt/device/debug_info`");
    expect(mqtt).toContain("command/`set` topics always stay single-target");
    const write = readFileSync("skills/write/SKILL.md", "utf8");
    expect(write).toContain("automations OR scripts, never mixed");
    const helper = readFileSync("skills/helper/SKILL.md", "utf8");
    expect(helper).toContain("storage and config-entry families never mix");
    const dashboard = readFileSync("skills/dashboard/SKILL.md", "utf8");
    expect(dashboard).toContain("dashboards, resources, and cards are separate families");
    const maintenance = readFileSync("skills/maintenance/SKILL.md", "utf8");
    expect(maintenance).toContain("one issue group or config entry per manifest");
    expect(maintenance).toContain("every safety gate below stays unchanged");
    expect(maintenance).toContain("Manifest-bound typed confirmation in the `confirm:batch-...` format");
    expect(maintenance).toContain("format, one config entry per manifest");
    const organize = readFileSync("skills/organize/SKILL.md", "utf8");
    expect(organize).toContain("except a confirmed batch manifest per `skills/ha-nova/batch-safety.md`");
  });
});
