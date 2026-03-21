import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const reviewSkill = readFileSync("skills/review/SKILL.md", "utf8");
const reviewChecks = readFileSync("skills/review/checks.md", "utf8");
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
const reviewAgent = readFileSync("skills/ha-nova/agents/review-agent.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");
const contributingDoc = readFileSync("CONTRIBUTING.md", "utf8");
const templateGuidelines = readFileSync("skills/ha-nova/template-guidelines.md", "utf8");

describe("review contract", () => {
  it("keeps the review facade pointed at the externalized rule catalog", () => {
    expect(reviewSkill).toContain("Rule Catalog");
    expect(reviewSkill).toContain("skills/review/checks.md");
    expect(reviewSkill).not.toContain("H-09 [MEDIUM → HIGH]");
    expect(reviewSkill).not.toContain("H-10 [LOW]");
  });

  it("documents the internal check taxonomy in the catalog", () => {
    expect(reviewChecks).toContain("Check Taxonomy (internal only)");
    expect(reviewChecks).toContain("Category letter = family");
    expect(reviewChecks).toContain("Severity is separate from the code");
    expect(reviewChecks).toContain("never show them in user-facing output");
  });

  it("documents the new helper threshold checks in the catalog", () => {
    expect(reviewChecks).toContain("H-09 [MEDIUM → HIGH]");
    expect(reviewChecks).toContain("H-10 [LOW]");
    expect(reviewChecks).toContain("`>`/`>=` is risky near `min`");
    expect(reviewChecks).toContain("`<`/`<=` is risky near `max`");
    expect(reviewChecks).toContain("within `1 × step`");
  });

  it("documents live helper evidence for threshold checks in the catalog", () => {
    expect(reviewChecks).toContain("Helper Threshold Evidence");
    expect(reviewChecks).toContain('/api/states/<helper_entity_id>');
    expect(reviewChecks).toContain("attributes.min");
    expect(reviewChecks).toContain("attributes.max");
    expect(reviewChecks).toContain("attributes.step");
    expect(reviewChecks).toContain("relative to `min`, not `value % step`");
    expect(reviewChecks).toContain("Do not emit R-10 just because H-09 matched");
  });

  it("keeps shared references aligned to H-01..H-10", () => {
    expect(writeSkill).toContain("H-01..H-10");
    expect(helperSkill).toContain("H-01..H-10");
    expect(reviewSkill).toContain("Helper (storage-based family): H-01..H-10");
    expect(reviewAgent).toContain("**Helper (storage-based family):** H-01..H-10.");
    expect(architectureDoc).toContain("H-01..H-10");
  });

  it("keeps live-evidence helper checks staged in write/helper flows", () => {
    expect(writeSkill).toContain("H-01..H-08");
    expect(writeSkill).toContain("Defer H-09/H-10 to Phase 4");
    expect(helperSkill).toContain("Apply H-01..H-08 directly");
    expect(helperSkill).toContain("Only evaluate H-09/H-10");
    expect(helperSkill).toContain("direct helper-backed threshold");
    expect(helperSkill).toContain("Do not pretend H-01..H-10 apply here");
    expect(helperSkill).toContain("minimal config-entry post-write contract");
    expect(reviewSkill).toContain("Helper (config-entry family): minimal config-entry review");
    expect(reviewSkill).toContain("do not apply H-01..H-10");
    expect(reviewAgent).toContain("minimal config-entry review");
    expect(reviewAgent).toContain("Do not apply H-01..H-10");
  });

  it("documents config-entry helper target resolution before minimal review", () => {
    expect(reviewSkill).toContain("config-entry helper review remains minimal, but target resolution must still normalize to a real `entry_id`");
    expect(reviewSkill).toContain('{"type":"config_entries/get"}');
    expect(reviewSkill).toContain('{"type":"config/entity_registry/list"}');
    expect(reviewSkill).toContain("exact `entry_id`");
    expect(reviewSkill).toContain("linked `entity_id` by matching entity-registry `config_entry_id`");
    expect(reviewSkill).toContain("Skip this step for config-entry helpers — `entry_id` is already the canonical identity.");
    expect(reviewSkill).toContain("For config-entry helpers, persist the canonical metadata item from step 3 to `<target-file>`");
    expect(reviewSkill).toContain("For standalone config-entry helper review, skip this step entirely.");
    expect(reviewSkill).toContain("If the target already in context is a config-entry helper metadata item: skip Target Resolution entirely and go straight to the config-entry helper review lane in Step 1.");
    expect(reviewSkill).toContain("Do not attempt primary-controlled-entity state reads or Quick-Fix detection from that path.");
    expect(reviewSkill).toContain("in Step 2, derive collision candidates from `linked_entities[]`, not from config actions");
    expect(reviewSkill).toContain("config-entry helper metadata item: use `linked_entities[]` from the canonical metadata item; do not attempt action extraction");
    expect(reviewSkill).toContain("helper (config-entry family): use up to 3 `linked_entities[]` from the canonical metadata item");
    expect(reviewAgent).toContain("helper (config-entry family): `entry_id`");
    expect(reviewAgent).toContain("helper (config-entry family): canonical metadata item (`entry_id`, `domain`, `title`, `state`, `linked_entities[]`)");
    expect(reviewAgent).toContain("In Step 2, derive collision candidates from `linked_entities[]`, not from action extraction.");
    expect(reviewAgent).toContain("helper (config-entry family): use up to 3 `linked_entities[]` from `{CONFIG}`");
  });

  it("documents contributor-facing taxonomy entry points", () => {
    expect(architectureDoc).toContain("## Review Check Taxonomy");
    expect(architectureDoc).toContain("`H` = Helper-specific");
    expect(architectureDoc).toContain("`R` = Reliability");
    expect(contributingDoc).toContain("Review Check Taxonomy");
    expect(contributingDoc).toContain("docs/reference/skill-architecture.md");
    expect(contributingDoc).toContain("skills/review/SKILL.md");
    expect(contributingDoc).toContain("skills/review/checks.md");
  });

  it("documents templated event name traps in the review catalog and template guide", () => {
    expect(reviewChecks).toContain("R-16 [HIGH]");
    expect(reviewChecks).toContain("Templated event name");
    expect(reviewChecks).toContain("`event_type:` does not evaluate templates");
    expect(reviewSkill).toContain("R-01..R-17");
    expect(reviewAgent).toContain("R-01..R-17");
    expect(architectureDoc).toContain("R-01..R-17");
    expect(templateGuidelines).toContain("Event trigger names must be literal strings");
    expect(templateGuidelines).toContain("do not template `event_type:`");
  });

  it("documents narrow overwrite/rebound detection as R-17", () => {
    expect(reviewChecks).toContain("R-17 [MEDIUM → HIGH]");
    expect(reviewChecks).toContain("Intra-config overwrite/rebound risk");
    expect(reviewChecks).toContain("Never use collision-scan results to trigger R-17");
    expect(reviewChecks).toContain("`live/incremental`");
    expect(reviewChecks).toContain("`recompute/reset`");
    expect(reviewChecks).toContain("fixed preset branches");
    expect(reviewSkill).toContain("R-17 is an intra-config branch comparison only");
    expect(reviewAgent).toContain("Do not derive it from collision-scan matches");
    expect(architectureDoc).toContain("R-17` is intra-config only");
  });

  it("keeps standalone config-entry helper review aligned to the 9 helper-owned domains", () => {
    expect(reviewSkill).toContain("supported config-entry family: domain is one of `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`");
    expect(reviewSkill).toContain("Helper (config-entry family): minimal config-entry review");
    expect(reviewAgent).toContain("**Helper (config-entry family):** minimal config-entry review");
  });

  it("keeps standalone bulk review separate from post-write review-agent output", () => {
    expect(reviewSkill).toContain("Bulk Mode Gate");
    expect(reviewSkill).toContain("For resolved targets `> 1`, return exactly these 6 sections");
    expect(reviewSkill).toContain("Section 2 — Summary");
    expect(reviewSkill).toContain("bulk workset max 5 targets");
    expect(reviewSkill).toContain("more than one resolved target -> current review set = the resolved targets in deterministic order, trimmed to the first 5 only when more than 5 targets match");
    expect(reviewSkill).not.toContain("2-3 resolved targets -> current review set = all resolved targets, reviewed serially in deterministic order");
    expect(reviewSkill).not.toContain("more than 3 resolved targets -> current review set = first 5 targets only");
    expect(reviewSkill).toContain('use `search/related` with `item_type:"area"` before any registry-first fallback');
    expect(reviewSkill).toContain("keyed object (`automation`, `script`, `entity`, `device`, ...)");
    expect(reviewSkill).toContain("do not ask a clarifying question just because multiple matches remain");
    expect(reviewSkill).toContain("do not resolve unique_ids, read configs, read states, or run collision scans for matched-but-non-audited remainder targets outside the current review set");
    expect(reviewSkill).toContain("a narrow collision explanation may inspect one extra target outside the matched remainder");
    expect(reviewSkill).toContain("clearly marked as related/collision evidence in the transcript");
    expect(reviewSkill).toContain("never build a full matched-set config cache or evidence snapshot; if caching helps, cache only the current review set");
    expect(reviewSkill).toContain("resolve `unique_id`, config, state, and related-item evidence per target inside the current workset only; no prefetch for the remaining matched targets");
    expect(reviewSkill).toContain("Quick-Fix is single-target only");
    expect(reviewSkill).toContain("step 7 above");
    expect(reviewSkill).toContain("step 3 to `<target-file>`");
    expect(reviewSkill).toContain("Target Resolution step 5");
    expect(reviewSkill).toContain("Never build a config snapshot for the full matched set during bulk review");
    expect(reviewSkill).toContain("POSIX shell example");
    expect(reviewSkill).toContain("On Windows/PowerShell, use the native file-writing equivalent");
    expect(reviewSkill).toContain("keep shared temp files serial or use dedicated payload filenames per probe");
    expect(reviewSkill).toContain("save the raw `search/related` response first");
    expect(reviewSkill).toContain("separate follow-up step with `ha-nova relay jq --file <related-file> '<filter>'`");
    expect(reviewSkill).toContain("do not attach a complex `--jq-file` filter directly to the `search/related` relay call during collision scan");
    expect(reviewSkill).toContain("do not batch multiple audited config-body reads into one shell loop");
    expect(reviewSkill).toContain("run one dedicated command block per audited target");
    expect(reviewAgent).toContain("This agent is for single-target review only.");
    expect(reviewAgent).toContain("Ignore the standalone bulk-review mode");
    expect(reviewAgent).toContain("single-target output format");
  });
});
