import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

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

  it("requires the preview narrative to cover every touched collection (issue #390)", () => {
    expect(outputRules).toContain(
      "each added, removed, replaced, or modified entry described by its effect",
    );
    expect(outputRules).toContain("never only by count or type name");
    expect(outputRules).toContain("coverage beats the three-sentence guideline");
  });

  it("shows the change table with the canonical header and verbatim-row rule", () => {
    expect(outputRules).toContain("| Field | Before | After |");
    expect(outputRules).toContain("|---|---|---|");
    expect(outputRules).toContain("pasted verbatim from `ha-nova diff`");
    expect(outputRules).toContain("never invented and never re-aligned");
  });

  it("keeps the options line and confirmation-code prompt literal", () => {
    expect(outputRules).toContain("Options: apply · show yaml · cancel");
    expect(outputRules).toContain("To delete, reply exactly: confirm:<token>");
    expect(outputRules).toContain("the confirmation-code prompt is always the last line");
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

  it("makes every skill with a typed-confirmation flow reference the Cards contract (issue #389)", () => {
    // Self-maintaining adoption lint: any sub-skill carrying a confirm: flow
    // must name the Cards contract. skills/ha-nova defines the contract itself.
    // Negative prose ("not card-framed") does not count as a reference.
    const CARD_REF = /(Preview Card|Delete Card|Result Card|Test Plan Card|Cards defined there)/;
    const skillDirs = readdirSync("skills").filter(
      (d) => d !== "ha-nova" && statSync(join("skills", d)).isDirectory(),
    );
    for (const name of skillDirs) {
      const content = readFileSync(`skills/${name}/SKILL.md`, "utf8");
      if (!content.includes("confirm:")) continue;
      expect(
        CARD_REF.test(content),
        `skills/${name}/SKILL.md contains a confirm: flow but never references the Cards contract (output-rules.md → Cards)`,
      ).toBe(true);
    }
  });

  it("maps every mutation and restore flow onto a card (coverage matrix, issue #389)", () => {
    expect(outputRules).toContain("Card coverage");
    expect(outputRules).toContain("no\nwrite flow renders outside this system");
    expect(outputRules).toContain(
      "| Create / update (any supported family) | Preview Card → Result Card |",
    );
    expect(outputRules).toContain(
      "| Delete / destructive operation (typed confirmation code) | Delete Card → Result Card |",
    );
    // Natural-confirmation removals (snapshot prune, todo item removes) never
    // pull the Delete Card's typed gate.
    expect(outputRules).toContain(
      "| Natural-confirmation removals (e.g. snapshot prune, todo item removes) | Preview Card → Result Card |",
    );
    expect(outputRules).toContain(
      "| Batch mutation (manifest-gated) | Batch Cards (`skills/ha-nova/batch-safety.md`) |",
    );
    expect(outputRules).toContain(
      "| Snapshot restore (`skills/ha-nova/config-snapshots.md`) | Preview Card → Result Card |",
    );
    expect(outputRules).toContain("| Post-write test offer | Test Plan Card |");
    // Runtime actions never downgrade the high-consequence typed gate.
    expect(outputRules).toContain(
      "high-consequence actions escalate to the typed confirmation code",
    );
    // Restore flows are covered explicitly, not by convention.
    const configSnapshots = readFileSync("skills/ha-nova/config-snapshots.md", "utf8");
    expect(configSnapshots).toContain("render as the Preview and Result Cards");
  });

  it("keeps each canonical card template structurally sound (issue #389)", () => {
    // Parse the fenced template after each card marker; fail when a required
    // section or icon is missing, duplicated, or reordered.
    const cardTemplate = (name: string): string[] => {
      const idx = outputRules.indexOf(`**${name}**`);
      expect(idx, `${name} marker missing`).toBeGreaterThan(-1);
      const fenceStart = outputRules.indexOf("```", idx);
      expect(fenceStart, `${name} template fence missing`).toBeGreaterThan(-1);
      const fenceEnd = outputRules.indexOf("```", fenceStart + 3);
      expect(fenceEnd, `${name} template fence unterminated`).toBeGreaterThan(fenceStart);
      return outputRules
        .slice(fenceStart + 3, fenceEnd)
        .trim()
        .split("\n");
    };

    const preview = cardTemplate("Preview Card");
    expect(preview[0]).toMatch(/^📝 Preview:/);
    expect(preview.filter((l) => l.startsWith("⚠️")), "exactly one status line").toHaveLength(1);
    const previewOptions = preview.filter((l) => l.startsWith("Options:"));
    expect(previewOptions, "exactly one action block").toHaveLength(1);
    expect(preview[preview.length - 1], "action block closes the card").toBe(previewOptions[0]);
    expect(
      preview.findIndex((l) => l.startsWith("⚠️")),
      "status line precedes the action block",
    ).toBe(preview.length - 2);

    const del = cardTemplate("Delete Card");
    expect(del[0]).toMatch(/^🗑️ {2}Delete:/);
    expect(del.filter((l) => l.startsWith("⚠️")), "exactly one status line").toHaveLength(1);
    expect(del[del.length - 1], "confirmation prompt closes the card").toBe(
      "To delete, reply exactly: confirm:<token>",
    );
    expect(del.findIndex((l) => l.startsWith("⚠️")), "status precedes the prompt").toBe(
      del.length - 2,
    );

    const result = cardTemplate("Result Card");
    expect(result[0]).toMatch(/^✅ Saved:/);
    expect(result.filter((l) => l.startsWith("✅")), "exactly one result title").toHaveLength(1);

    const testPlan = cardTemplate("Test Plan Card");
    expect(testPlan[0]).toMatch(/^📝 Test:/);
    const recommended = testPlan.findIndex((l) => l.startsWith("1 (recommended)"));
    const second = testPlan.findIndex((l) => l.startsWith("2 —"));
    const skip = testPlan.findIndex((l) => l.startsWith("skip"));
    expect(recommended, "recommended option present").toBeGreaterThan(0);
    expect(second, "options stay ordered").toBeGreaterThan(recommended);
    expect(skip, "skip closes the menu").toBeGreaterThan(second);
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
