import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// The Cards section of output-rules.md is the single visual design system every
// skill's previews, delete prompts, and results render through. These pins lock
// the card shapes and the boundary that keeps review/read output out of them.
describe("output design system (Cards)", () => {
  const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");

  it("defines the four cards with the fixed emoji vocabulary", () => {
    expect(outputRules).toContain("## Cards (Write-Flow Visual System)");
    expect(outputRules).toContain("one of four cards");
    expect(outputRules).toContain("**Preview Card**");
    expect(outputRules).toContain("**Delete Card**");
    expect(outputRules).toContain("**Result Card**");
    expect(outputRules).toContain("**Test Plan Card**");
    expect(outputRules).toContain("📝 Preview:");
    expect(outputRules).toContain("📝 Test:");
    expect(outputRules).toContain("🗑️  Delete:");
    expect(outputRules).toContain("✅ Saved:");
    expect(outputRules).toContain("⚠️  Nothing saved yet.");
    expect(outputRules).toContain("⚠️  Nothing deleted yet.");
    expect(outputRules).toContain("💡 suggestion");
    expect(outputRules).toContain("nothing else decorative, never color");
  });

  it("pads variation-selector emoji with two spaces in every template", () => {
    // 🗑️ and ⚠️ end in U+FE0F; many terminals render them double-width over
    // the next column and visually swallow a single following space.
    expect(outputRules).toContain("take two spaces before the following text");
    const templates = [
      outputRules,
      readFileSync("skills/ha-nova/write-safety.md", "utf8"),
      readFileSync("skills/yaml-config/SKILL.md", "utf8"),
    ];
    for (const content of templates) {
      // No template line (these start with the emoji) may follow a VS16 emoji
      // with exactly one space; inline prose mentions are exempt.
      expect(content).not.toMatch(/^[🗑⚠]️ [^ ]/mu);
    }
  });

  it("names the typed keyword a confirmation code toward users, never a token", () => {
    expect(outputRules).toContain('is the "confirmation code" (localized');
    expect(outputRules).toContain('Never call it a "token" in user-facing output');
    const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(contextSkill).toContain('call it the "confirmation code" (localized), never a "token"');
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

  it("defines the shared read-flow shapes", () => {
    expect(outputRules).toContain("## Report Shape (Read & Analysis Results)");
    expect(outputRules).toContain("## List Frame (Inventories & Discovery)");
    expect(outputRules).toContain("## Suggestion Block");
    // Answer first, canonical closing slot, honest truncation, diagnosis lead.
    expect(outputRules).toContain("lead with the answer");
    expect(outputRules).toContain("`Next step` (localized; never `Next Step`)");
    expect(outputRules).toContain("state the cap whenever rows are truncated");
    expect(outputRules).toContain("root cause (or ranked hypotheses)");
  });

  it("rolls the suggestion block into every offer site", () => {
    const write = readFileSync("skills/write/SKILL.md", "utf8");
    const helper = readFileSync("skills/helper/SKILL.md", "utf8");
    const review = readFileSync("skills/review/SKILL.md", "utf8");
    const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
    expect(write).toContain("as the Suggestion Block");
    expect(helper).toContain("render them as the Suggestion Block");
    expect(helper).toContain('💡 Suggested defaults for "{name}"');
    // Review keeps its plain sectioned header; only the item shape is shared.
    expect(review).toContain("Suggestion Block item shape");
    expect(writeSafety).toContain("separate Suggestion Block item");
    // One shape everywhere: title + what + why, capped, skippable, never forced.
    expect(outputRules).toContain("short title + what it does + why it helps");
    expect(outputRules).toContain("omit the block entirely");
    // The producing agent knows how its items will render.
    const resolveAgent = readFileSync("skills/ha-nova/agents/resolve-agent.md", "utf8");
    expect(resolveAgent).toContain("renders these as the Suggestion Block");
  });

  it("routes the freeform read skills through the shared shapes", () => {
    for (const skill of [
      "diagnose",
      "media",
      "notify",
      "camera",
      "mqtt",
      "assist",
      "admin",
      "external-sources",
      "dashboard",
      "organize",
    ]) {
      const content = readFileSync(`skills/${skill}/SKILL.md`, "utf8");
      expect(content, `skills/${skill}/SKILL.md must render the Report shape`).toContain(
        "Report shape",
      );
    }
    for (const skill of ["entity-discovery", "read"]) {
      const content = readFileSync(`skills/${skill}/SKILL.md`, "utf8");
      expect(content, `skills/${skill}/SKILL.md must render the List Frame`).toContain(
        "List Frame",
      );
    }
  });

  it("folds the test plan card into the central card system", () => {
    const testRun = readFileSync("skills/ha-nova/test-run.md", "utf8");
    const write = readFileSync("skills/write/SKILL.md", "utf8");
    expect(testRun).toContain("Test Plan Card of `skills/ha-nova/output-rules.md`");
    expect(write).toContain("Test Plan Card mechanics: `skills/ha-nova/test-run.md`");
    expect(outputRules).toContain("skills/ha-nova/test-run.md");
  });

  it("keeps the canonical Next step label free of drift", () => {
    const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(contextSkill).not.toContain("Next Step");
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
