import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const grouped = readFileSync("skills/ha-nova/grouped-change-set.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const batchSafety = readFileSync("skills/ha-nova/batch-safety.md", "utf8");

// Every skill that declares grouped support must point at the shared contract.
const OPTED_IN_SKILLS = [
  "write",
  "helper",
  "scene",
  "organize",
  "service-call",
];

describe("grouped change set contract (issue #391)", () => {
  it("is referenced by the context skill, output-rules, write-safety, batch-safety, and every opted-in skill", () => {
    expect(contextSkill).toContain("skills/ha-nova/grouped-change-set.md");
    expect(outputRules).toContain("skills/ha-nova/grouped-change-set.md");
    expect(writeSafety).toContain("skills/ha-nova/grouped-change-set.md");
    expect(batchSafety).toContain("skills/ha-nova/grouped-change-set.md");
    for (const skill of OPTED_IN_SKILLS) {
      const doc = readFileSync(`skills/${skill}/SKILL.md`, "utf8");
      expect(
        doc,
        `skills/${skill}/SKILL.md must reference the grouped contract`,
      ).toContain("skills/ha-nova/grouped-change-set.md");
    }
  });

  it("declares grouped support explicitly per skill in a capability matrix", () => {
    expect(grouped).toContain("## Capability Matrix (v1)");
    for (const skill of OPTED_IN_SKILLS) {
      expect(grouped).toContain(`| \`${skill}\` | yes |`);
    }
    expect(grouped).toContain("| all others | no |");
    expect(grouped).toContain("Never infer it");
  });

  it("groups only one logical task and allows mixed families unlike destructive batches", () => {
    expect(grouped).toContain(
      "Group ONLY operations that belong to one user-requested logical task",
    );
    expect(grouped).toContain("Mixed supported configuration families are allowed");
    expect(grouped).toContain(
      "destructive batches keep one resource family per manifest",
    );
    // The destructive invariant itself stays untouched.
    expect(batchSafety).toContain("One resource family per manifest");
  });

  it("caps the group at ten operations", () => {
    expect(grouped).toContain("**Cap: 10 operations.**");
    expect(grouped).toContain("split into explicit separate groups");
    expect(contextSkill).toContain("max 10 operations");
  });

  it("rejects destructive operations from the group", () => {
    expect(grouped).toContain("**No destructive operations.**");
    expect(grouped).toContain(
      "Anything that requires a confirmation code",
    );
    expect(grouped).toContain("keeps its own flow");
    expect(contextSkill).toContain(
      "operations requiring a confirmation code are rejected from the group",
    );
  });

  it("renders full previews with no intermediate menus and one final action block", () => {
    expect(grouped).toContain(
      "Render the full canonical Preview Card for EVERY operation",
    );
    expect(grouped).toContain("Intermediate previews carry NO options block");
    expect(grouped).toContain("exactly ONE final action block");
    expect(grouped).toContain("`apply · show yaml · cancel`");
    // The carve-out lives next to the rule it excepts.
    expect(outputRules).toContain(
      "Sole exception: intermediate previews inside a grouped change set",
    );
  });

  it("binds the natural confirmation to the exact displayed set", () => {
    expect(grouped).toContain("ordinary natural-language apply");
    expect(grouped).toContain("bound to the exact\n   displayed set");
    expect(grouped).toContain("expires the confirmation");
  });

  it("executes sequentially with an operation-specific pre-apply check", () => {
    expect(grouped).toContain("Sequential, in previewed order; fail fast");
    expect(grouped).toContain("Immediately before EACH operation");
    expect(grouped).toContain("on foreign change, STOP the group");
    // Per operation class: drift re-read, create-collision absence, liveness.
    expect(grouped).toContain("write-safety → Drift check");
    expect(grouped).toContain("still absent");
    expect(grouped).toContain("not `unavailable`/`unknown` before firing");
  });

  it("keeps a per-operation ledger and reports partial completion honestly", () => {
    expect(grouped).toContain("planned / applied / skipped / failed");
    expect(grouped).toContain("the exact applied subset");
    expect(grouped).toContain("what was not attempted");
    expect(grouped).toContain("never continue silently");
  });

  it("never claims atomicity or automatic rollback", () => {
    expect(grouped).toContain(
      "Never claim the group is atomic, transactional, or automatically revertible",
    );
    expect(grouped).toContain("Recovery stays per-operation");
    expect(grouped).toContain("not atomic");
  });

  it("keeps grouped cards inside the shared card system", () => {
    expect(grouped).toContain("output-rules.md` → Cards");
    expect(grouped).toContain("⚠️  Nothing saved yet");
    expect(grouped).toContain("Options: apply · show yaml · cancel");
    expect(grouped).toContain("Ledger:");
    expect(outputRules).toContain(
      "| Grouped change set (non-destructive, one logical task) |",
    );
  });
});
