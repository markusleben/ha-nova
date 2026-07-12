import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// The Cards section of output-rules.md is the single visual design system every
// skill's previews, delete prompts, and results render through. These pins lock
// the card shapes and the boundary that keeps review/read output out of them.
describe("output design system (Cards)", () => {
  const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");

  it("defines the three cards with the fixed emoji vocabulary", () => {
    expect(outputRules).toContain("## Cards (Write-Flow Visual System)");
    expect(outputRules).toContain("**Preview Card**");
    expect(outputRules).toContain("**Delete Card**");
    expect(outputRules).toContain("**Result Card**");
    expect(outputRules).toContain("📝 Preview:");
    expect(outputRules).toContain("🗑️ Delete:");
    expect(outputRules).toContain("✅ Saved:");
    expect(outputRules).toContain("⚠️ Nothing saved yet.");
    expect(outputRules).toContain("⚠️ Nothing deleted yet.");
    expect(outputRules).toContain("nothing else decorative, never color");
  });

  it("shows the change table with the canonical header and verbatim-row rule", () => {
    expect(outputRules).toContain("| Field | Before | After |");
    expect(outputRules).toContain("|---|---|---|");
    expect(outputRules).toContain("pasted verbatim from `ha-nova diff`");
    expect(outputRules).toContain("never invented and never re-aligned");
  });

  it("keeps the options line and token prompt literal", () => {
    expect(outputRules).toContain("Options: apply · show yaml · cancel");
    expect(outputRules).toContain("To delete, reply exactly: confirm:<token>");
    expect(outputRules).toContain("the token prompt is always the last line");
  });

  it("maps existing per-skill slots into the cards centrally", () => {
    // This sentence is what lets dashboard/scene/organize/energy/updates adopt
    // the cards with zero per-skill edits — their slot lists describe card
    // content.
    expect(outputRules).toContain("`Planned change` is the changes block");
    expect(outputRules).toContain("`Save status` is the ⚠️ line");
    expect(outputRules).toContain("fold into the title line");
  });

  it("bounds the cards to write/action flows and keeps read output tabular", () => {
    expect(outputRules).toContain(
      "Cards frame writes and runtime actions only; review and read output keep their own sections.",
    );
    expect(outputRules).toContain("max 4 short columns");
    expect(outputRules).toContain("never raw JSON");
  });

  it("keeps the result card scope-honest", () => {
    expect(outputRules).toContain("Runtime behavior was not exercised.");
    expect(outputRules).toContain("Verification Honesty");
  });

  it("rolls the cards into the three special-format skills", () => {
    const serviceCall = readFileSync("skills/service-call/SKILL.md", "utf8");
    const yamlConfig = readFileSync("skills/yaml-config/SKILL.md", "utf8");
    const review = readFileSync("skills/review/SKILL.md", "utf8");
    // service-call: multi-value deltas become the mini table; single value
    // keeps the arrow one-liner; no show yaml for runtime actions.
    expect(serviceCall).toContain("`Field | Before | After` mini table");
    expect(serviceCall).toContain("one value keeps the arrow one-liner");
    expect(serviceCall).toContain("Preview Card (`apply · cancel`)");
    // yaml-config: layperson file preview — changed section only, never a
    // developer diff, whole file only on show yaml.
    expect(yamlConfig).toContain("## File-Change Preview");
    expect(yamlConfig).toContain("New section (the only part that changes):");
    expect(yamlConfig).toContain("Never a `-`/`+` unified diff");
    expect(yamlConfig).toContain("show yaml = the whole resulting file");
    // review keeps its pinned sections; only the quick-fix crosses into cards.
    expect(review).toContain("Review output is sectioned, not card-framed");
  });
});
