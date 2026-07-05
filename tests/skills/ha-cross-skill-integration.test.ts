import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha cross-skill integration", () => {
  it("routes write flow through resolve + preview + apply + review phases", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(writeSkill).toContain("Phase 1: Resolve (Agent)");
    expect(writeSkill).toContain("Phase 2: Preview + Confirm (Main Thread)");
    expect(writeSkill).toContain("Phase 3: Apply + Verify (Agent)");
    expect(writeSkill).toContain("Phase 4: Post-Write Review");
    expect(writeSkill).toContain("skills/review/SKILL.md");
    expect(writeSkill).toContain("full-replacement merge (base=current, overlay=user changes)");
    expect(writeSkill).toContain("confirm:<token>");
    expect(writeSkill).toContain("Changes slot");
    expect(writeSkill).toContain("show yaml");
    expect(writeSkill).toContain("cancel");
    expect(writeSkill).toContain("Fallback: If agent dispatch unavailable");
  });

  it("keeps write skill wired to shared relay + best-practices references", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(writeSkill).toContain("skills/ha-nova/relay-api.md");
    expect(writeSkill).toContain("skills/ha-nova/best-practices.md");
    expect(writeSkill).toContain("skills/ha-nova/agents/resolve-agent.md");
    expect(writeSkill).toContain("skills/ha-nova/agents/apply-agent.md");
    expect(writeSkill).toContain("skills/review/SKILL.md");
    expect(writeSkill).toContain("config/entity_registry/get");
    expect(writeSkill).toContain("list_for_display` only for search/disambiguation");
  });

  it("keeps write skill concise and phase-driven", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(writeSkill).toContain("## Bootstrap (once per session)");
    expect(writeSkill).toContain("ha-nova relay health");
    expect(writeSkill).toContain("If this fails, run onboarding: `ha-nova setup`.");
    expect(writeSkill).toContain("## Flow");
    expect(writeSkill).toContain("Fallback: If agent dispatch unavailable");
    expect(writeSkill).toContain("## Safety");
    expect(writeSkill).toContain("Agents must use Relay only; no MCP, no direct HA API");
    expect(writeSkill).toContain('description: Use when creating, updating, or deleting');
    expect(writeSkill).not.toContain("RELAY_BASE_URL");
    expect(writeSkill).not.toContain("RELAY_AUTH_TOKEN");
    expect(writeSkill).not.toContain("## Lazy Load Contract");
    expect(writeSkill).not.toContain("## Relay API Injection Rules");
  });

  it("includes proactive suggestions and pre-write checks in write skill", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    // Phase 1 extracts suggested_enhancements from resolve agent
    expect(writeSkill).toContain("suggested_enhancements");

    // Phase 2 Step 3a: suggestions flow
    expect(writeSkill).toContain("3a) Suggestions");
    expect(writeSkill).toContain("max 4, numbered");
    expect(writeSkill).toContain('or "skip"');
    expect(writeSkill).toContain("merge accepted into config BEFORE preview");
    expect(writeSkill).toContain("SUGGESTED_ENHANCEMENTS: none");
    expect(writeSkill).toContain("skip for `delete`");

    // Phase 2 Step 3b: pre-write static checks
    expect(writeSkill).toContain("3b) Static Checks");
    expect(writeSkill).toContain("analytically on the draft YAML");
    expect(writeSkill).toContain("🔴 findings");
    expect(writeSkill).toContain("🟠🟡 findings");
    expect(writeSkill).toContain("dedup in Phase 4");
    expect(writeSkill).toContain("REST/UI write can break dependent variables");
    expect(writeSkill).toContain("do not block the write");
    expect(writeSkill).toContain("do not require extra confirmation");
    expect(writeSkill).toContain("final else branch is only reached when the earlier entity-state branches are false");
    expect(writeSkill).toContain('Pre-write check: no issues worth flagging before save.');
    expect(writeSkill).toContain('Pre-write check: this draft may not behave as intended.');
  });

  it("includes HA normalization awareness and dedup in post-write review", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    // HA plural aliasing awareness
    expect(writeSkill).toContain("Compare read-back vs draft as normalized objects, not raw JSON strings");
    expect(writeSkill).toContain("key order is irrelevant");
    expect(writeSkill).toContain("trigger");
    expect(writeSkill).toContain("triggers");
    expect(writeSkill).toContain("plural aliasing");

    // Dedup rule with example
    expect(writeSkill).toContain("Dedup");
    expect(writeSkill).toContain("MUST NOT repeat");
    expect(writeSkill).toContain("check type");
    expect(writeSkill).toContain("storage-sensitive R-18 subset");
    expect(writeSkill).toContain("persisted runtime risk");
    expect(writeSkill).toContain("report it again");
    expect(writeSkill).toContain("inspect traces after the next real run");
    expect(writeSkill).toContain("Do not auto-trigger or auto-read traces");
    expect(writeSkill).toContain("`R-19` follows normal dedup");

    // Post-write review reports only substance — empty sections are omitted, not shown as "none" buckets
    expect(writeSkill).toContain("Findings");
    expect(writeSkill).toContain("Collision check");
    expect(writeSkill).toContain("Advisory");
    expect(writeSkill).toContain('never print an empty "none" bucket');
    expect(writeSkill).toContain("collapse to one localized confirmation line");
    expect(writeSkill).toContain("Never emit `Questions to consider`, `Suggestions`, or `Instant help` post-write");
    expect(writeSkill).toContain("never repeat an item across **Findings** and **Advisory**");
    expect(writeSkill).toContain("skills/ha-nova/output-rules.md");
    // The old always-on "none" buckets must be gone (that was the noise the maintainer flagged).
    expect(writeSkill).not.toContain('localized equivalent of "No related items found."');
    expect(writeSkill).not.toContain('localized equivalent of "No conflicts found."');
    expect(writeSkill).not.toContain('localized equivalent of "No additional advisories."');
  });

  it("keeps write flows constrained to one ambiguity question and no unrequested rewrites", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
    const refactorGuide = readFileSync("skills/ha-nova/safe-refactoring.md", "utf8");
    const templateGuidelines = readFileSync("skills/ha-nova/template-guidelines.md", "utf8");

    expect(writeSkill).toContain("unrelated structure, aliases, or formatting");
    expect(writeSkill).toContain("Treat notification copy as user-authored content");
    expect(writeSkill).toContain("must not restyle, relocalize, or restructure existing text");
    expect(writeSkill).toContain("change only the requested copy");
    expect(writeSkill).toContain("ask one blocking question");
    expect(writeSkill).toContain("second ambiguity question");
    expect(refactorGuide).toContain("directly affected consumers");
    expect(refactorGuide).toContain("Do not rewrite, rename, disable, or delete unrelated configs");
    expect(templateGuidelines).toContain("preserve existing notification wording and templates exactly");
  });

  it("keeps write preview output terminal-friendly and action-oriented", () => {
    const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");
    const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");

    expect(outputRules).toContain("Terminal-Friendly Shape");
    expect(outputRules).toContain("Localize section headings and labels to the user's language");
    expect(outputRules).toContain("Do not mix English slot labels such as `Changes`, `Options`, or `Pre-write check` into a German response");
    expect(outputRules).toContain("Prefer plain short labels over decorative Markdown headings");
    expect(outputRules).toContain("preview summary, changes or summary, pre-write check/impact, save status, options");
    expect(outputRules).toContain("Use stable localized labels for those slots across a conversation");
    expect(outputRules).toContain("same label and in the same order");
    expect(outputRules).toContain("omit that slot instead of printing an empty placeholder");
    expect(outputRules).toContain("nothing has been saved yet before showing the options");
    expect(outputRules).toContain("literal `apply`, `show yaml`, and `cancel`");
    expect(outputRules).toContain("Keep delete previews structured in this order");
    expect(outputRules).toContain("Delete previews must say nothing has been deleted yet");
    expect(outputRules).toContain("never render `apply`, `show yaml`, `cancel`, or a selectable menu");
    expect(outputRules).toContain("Internal task, card, or payload labels are execution artifacts too.");
    expect(outputRules).toContain("Automation Payload");
    expect(outputRules).toContain("Apply And Verify");
    expect(writeSafety).toContain("Save-status slot: explicitly say that nothing has been saved yet");
    expect(writeSafety).toContain("historical slot name, not a required literal Markdown heading");
    expect(writeSafety).toContain("<localized options label>:");
  });

  it("keeps create cleanup honest without implying backup-only rollback", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
    const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");

    expect(writeSkill).toContain("Creates → cleanup via normal HA NOVA delete flow");
    expect(writeSkill).toContain("preview, `confirm:<token>`, and absence verification");
    expect(writeSafety).toContain("A create is undone by deleting the new item through the");
    expect(writeSafety).toContain("manual deletion or a full Home Assistant Backup restore is the only cleanup path");
    expect(writeSafety).toContain("Do not call this `revert`");
    expect(writeSafety).toContain("A delete has no HA NOVA `revert`");
    expect(writeSafety).toContain("suitable existing Home Assistant Backup");
  });

  it("keeps write flow aligned to unique_id-first resolution and runtime verification", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
    const resolveAgent = readFileSync("skills/ha-nova/agents/resolve-agent.md", "utf8");
    const applyAgent = readFileSync("skills/ha-nova/agents/apply-agent.md", "utf8");

    expect(resolveAgent).toContain("resolve `unique_id` from entity registry first");
    expect(resolveAgent).toContain("do not probe config endpoints with the slug first");

    expect(applyAgent).toContain("actual `entity_id`");
    expect(applyAgent).toContain("/api/states/{entity_id}");
    expect(applyAgent).toContain("runtime_state");

    expect(writeSkill).toContain("entity_id -> unique_id");
    expect(writeSkill).toContain("resolve actual `entity_id` by matching `unique_id == <target_id>`");
    expect(writeSkill).toContain("matching `unique_id == <target_id>`");
    expect(writeSkill).toContain("do not silently assume the requested slug won");
  });
});
