import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const reviewSkill = readFileSync("skills/review/SKILL.md", "utf8");
const reviewChecks = readFileSync("skills/review/checks.md", "utf8");
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");
const contributingDoc = readFileSync("CONTRIBUTING.md", "utf8");
const templateGuidelines = readFileSync("skills/ha-nova/template-guidelines.md", "utf8");
const bestPractices = readFileSync("skills/ha-nova/best-practices.md", "utf8");
const automationPatterns = readFileSync(
  "skills/ha-nova/automation-patterns.md",
  "utf8",
);

describe("review contract", () => {
  it("binds Quick-Fix service calls to active-preview confirmation", () => {
    expect(reviewSkill).toContain("Ask for natural confirmation bound to this exact service-call preview");
    expect(reviewSkill).toContain("same tier as `ha-nova:service-call`");
    expect(reviewSkill).not.toContain("service calls are reversible");
  });

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
  });

  it("hoists the internal-code suppression rule into a top-level output guardrail", () => {
    expect(reviewChecks).toContain("## Output Guardrail (Critical)");
    expect(reviewChecks).toContain("skills/ha-nova/output-rules.md");
    expect(reviewChecks).toContain("not user-facing labels");
    expect(reviewChecks).toContain("describe each finding in plain language");
    expect(reviewChecks).toContain("not only during formal review runs");
    expect(reviewSkill).toContain("describe findings in plain language");
    expect(outputRules).toContain("Never show internal check codes");
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
    expect(writeSkill).toContain("H-01..H-11");
    expect(helperSkill).toContain("H-01..H-10");
    expect(reviewChecks).toContain("Helper (storage-based family): H-01..H-11");
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
    expect(reviewChecks).toContain("Helper (config-entry family): minimal config-entry review");
    expect(reviewChecks).toContain("do not apply H-01..H-11");
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
    expect(reviewChecks).toContain("in Step 2, derive collision candidates from `linked_entities[]`, not from config actions");
    expect(reviewSkill).toContain("config-entry helper metadata item: use `linked_entities[]` from the canonical metadata item; do not attempt action extraction");
    expect(reviewSkill).toContain("helper (config-entry family): use up to 3 `linked_entities[]` from the canonical metadata item");
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
    expect(reviewChecks).toContain("R-01..R-28");
    expect(architectureDoc).toContain("R-01..R-28");
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
    expect(reviewChecks).toContain("R-17 is an intra-config branch comparison only");
    expect(architectureDoc).toContain("R-17` is intra-config only");
  });

  it("documents storage-sensitive sibling variable dependencies as R-18", () => {
    expect(reviewChecks).toContain("R-18 [HIGH]");
    expect(reviewChecks).toContain("Same-block sibling variable dependency");
    expect(reviewChecks).toContain("same `variables:` mapping");
    expect(reviewChecks).not.toContain("sorts alphabetically after");
    expect(reviewChecks).toContain("Traverse all `variables:` mappings");
    expect(reviewChecks).toContain("`{% set %}` locals");
    expect(reviewChecks).toContain("script `fields`");
    expect(reviewChecks).toContain("cross action boundaries");
    expect(reviewChecks).toContain("self-contained template");
    expect(reviewChecks).toContain("ordered `variables` actions");
    expect(reviewSkill).toContain("Traverse all `variables:` mappings");
    expect(reviewChecks).toContain("future write fragility");
    expect(reviewChecks).toContain("persisted runtime risk");
    expect(reviewChecks).toContain("concrete variable pair");
    expect(templateGuidelines).toContain("Do not rely on sibling-variable order inside one `variables:` mapping");
    expect(templateGuidelines).toContain("self-contained template with internal `{% set %}`");
    expect(templateGuidelines).toContain("ordered `variables` actions");
    expect(architectureDoc).toContain("`R-18` is same-mapping only");
  });

  it("documents the issue-274 semantic and timing checks (R-26..R-28)", () => {
    // Valid config, clean save, matching read-back — and still never matches
    // legitimate runtime states: the silent regression class from issue #274.
    expect(reviewChecks).toContain("R-26 [MEDIUM → HIGH]: Exact-state equality narrower than the stated intent");
    expect(reviewChecks).toContain("a person at zone `work` has state `work`, not `not_home`");
    expect(reviewChecks).toContain("## R-26 Evidence Boundary");
    expect(reviewChecks).toContain("never flag intent-matching literals");
    expect(reviewChecks).toContain("never claim the automation is currently broken");
    // A fixed delay documents a hope, not completion.
    expect(reviewChecks).toContain("R-27 [MEDIUM]: Fixed `delay:` standing in for asynchronous completion");
    expect(reviewChecks).toContain("Do not flag delays that are themselves the intent");
    // Startup paths read states before integrations report them.
    expect(reviewChecks).toContain("R-28 [MEDIUM]: Startup race");
    expect(reviewChecks).toContain("`unknown`, `unavailable`, or stale-restored");
  });

  it("documents boolean-string template comparisons as R-23", () => {
    expect(reviewChecks).toContain("R-23 [MEDIUM]");
    expect(reviewChecks).toContain("Boolean-like template compared to a string literal");
    expect(reviewChecks).toContain("including reversed comparisons");
    expect(reviewChecks).toContain("`'True' == avg_valid`");
    expect(reviewChecks).toContain("Do not flag bare boolean comparisons");
    expect(reviewChecks).toContain("`is sameas true`");
    expect(reviewChecks).toContain("R-23 applies only to boolean-like templates");
    expect(writeSkill).toContain("If R-23 matches");
    expect(architectureDoc).toContain("`R-23` catches boolean-like templates");
  });

  it("documents the removed legacy template platform syntax as R-25", () => {
    expect(reviewChecks).toContain("R-25 [HIGH]");
    expect(reviewChecks).toContain("Legacy template platform syntax (removed in HA 2026.6)");
    expect(reviewChecks).toContain("## R-25 Evidence Boundary");
    // Pasted-YAML applicability guard: stored configs never carry this syntax.
    expect(reviewChecks).toContain("Apply only when reviewing pasted or draft YAML");
    expect(reviewChecks).toContain("do not fetch or scan `configuration.yaml`");
    // Version fetch is on-demand only, never a routine per-review call.
    expect(reviewChecks).toContain("only when this check actually fires");
    expect(reviewChecks).toContain("`value_template` → `state`");
    expect(reviewChecks).toContain("R-25 applies only to pasted or draft YAML");
    expect(reviewChecks).toContain("phrase the finding version-sensitively");
  });

  it("documents the legacy automation key modernize advisory as M-05", () => {
    expect(reviewChecks).toContain("M-05 [LOW]");
    expect(reviewChecks).toContain("Legacy automation syntax keys");
    expect(reviewChecks).toContain("renamed to `trigger:` in HA 2024.10");
    // Advisory only — both forms still work; never an error, never a rewrite trigger.
    expect(reviewChecks).toContain("never as an error, and never rewrite a config just to modernize");
    expect(reviewChecks).toContain("M-05 is a modernize advisory");
  });

  it("documents capacity-like available_energy advisories as R-24", () => {
    expect(reviewChecks).toContain("R-24 [LOW]");
    expect(reviewChecks).toContain("Capacity-like variable reads `available_energy`");
    expect(reviewChecks).toContain("Available charge may not be nominal or maximum battery capacity");
    expect(reviewChecks).toContain("Do not assume a specific integration");
    expect(reviewChecks).toContain("R-24 is advisory only");
    expect(writeSkill).toContain("If R-24 matches");
    expect(architectureDoc).toContain("`R-24` is a low-severity capacity-source advisory");
  });

  it("keeps raw trace internals optional and defensive during review", () => {
    expect(reviewSkill).toContain("ha-nova trace latest/list/get --json");
    expect(reviewSkill).toContain("they are enough for run selection, result status, timestamp, item binding, and most review findings");
    expect(reviewSkill).toContain("Inspect raw trace internals only when step-level evidence is required");
    expect(reviewSkill).toContain("Raw trace nodes can be arrays of event records");
    expect(reviewSkill).toContain("type-check before reading `path`, `result`, `changed_variables`, or `error`");
    expect(reviewSkill).toContain("avoid large jq projections as the standard path");
  });

  it("documents unreachable trigger fallbacks as R-19", () => {
    expect(reviewChecks).toContain("R-19 [MEDIUM]");
    expect(reviewChecks).toContain("Unreachable `trigger.id` in bare `else` branch");
    expect(reviewChecks).toContain("terminal bare `else`");
    expect(reviewChecks).toContain("move the `trigger.id` check into an explicit `elif`");
    expect(reviewChecks).toContain("refactor to `choose` + `condition: trigger`");
    expect(reviewChecks).toContain("R-19 applies only to Jinja2 chains with `if` plus at least one `elif`");
    expect(reviewChecks).toContain("final else branch is only reached when the earlier entity-state branches are false");
    expect(architectureDoc).toContain("`R-19` is branch-structure reachability only");
    expect(templateGuidelines).toContain("Direct `trigger.id` check in a terminal bare `else`");
  });

  it("documents the reverse-branch re-entry guard check as R-21", () => {
    expect(reviewChecks).toContain("R-21 [HIGH]");
    expect(reviewChecks).toContain("re-entry guard on capture-state flag");
    expect(reviewChecks).toContain("## R-21 Evidence Boundary");
    expect(reviewChecks).toContain("R-17 territory, not R-21");
    expect(automationPatterns).toContain(
      "guard the reverse (restore) branch on that flag",
    );
  });

  it("documents the persistence model and the restart-restore check as R-22", () => {
    expect(reviewChecks).toContain("R-22 [HIGH]");
    expect(reviewChecks).toContain("Restart-dependent restore from transient storage");
    expect(reviewChecks).toContain("## R-22 Evidence Boundary");
    expect(reviewChecks).toContain("Persistence Model");
    expect(bestPractices).toContain("## Persistence Model (restart survival)");
    expect(bestPractices).toContain("`scene.create` runtime snapshots");
    expect(bestPractices).toContain("timer` only with `restore: true");
    expect(automationPatterns).toContain("## Save / Restore Patterns");
  });

  it("documents the standalone questions-versus-suggestions split", () => {
    expect(reviewSkill).toContain("### Step 5: Explorative Questions");
    expect(reviewSkill).toContain("### Step 6: Suggestion Synthesis");
    expect(reviewSkill).toContain("Questions to consider");
    expect(reviewSkill).toContain("Suggestions");
    expect(reviewSkill).toContain('localized equivalent of "No follow-up questions right now."');
    expect(reviewSkill).toContain('localized equivalent of "No confident suggestions."');
    expect(reviewSkill).toContain("Fix existing");
    expect(reviewSkill).toContain("Simplify existing");
    expect(reviewSkill).toContain("Extend existing");
    expect(reviewSkill).toContain("Add new");
    expect(outputRules).toContain("Questions to consider");
    expect(outputRules).toContain("Suggestions");
    expect(architectureDoc).toContain("Questions to consider");
    expect(architectureDoc).toContain("intervention depth");
    expect(reviewSkill).toContain("Why: ...");
    expect(reviewSkill).toContain("Fix: ...");
  });

  it("documents compact post-write empty-state semantics without rule codes", () => {
    expect(outputRules).toContain("Never show internal check codes");
    expect(outputRules).toContain("findings, summaries, clean states, pre-write verdicts");
    // Post-write review omits empty sections instead of printing "none" buckets.
    expect(architectureDoc).toContain('never print an empty "none" bucket');
    expect(architectureDoc).toContain("collapse to one scope-honest confirmation line");
    expect(writeSkill).toContain('never print an empty "none" bucket');
    expect(writeSkill).toContain("collapse to one scope-honest confirmation line");
    // Standalone review still states a clean result (it answers an explicit request).
    expect(outputRules).toContain('"no issues found" result is useful');
  });

  it("documents the design-intent gate before remove or simplify suggestions", () => {
    expect(reviewSkill).toContain("Design-intent gate for remove/simplify ideas");
    expect(reviewSkill).toContain("treat existing logic as deliberate until proven otherwise");
    expect(reviewSkill).toContain("downgrade the idea into Questions to consider");
  });
  it("keeps standalone config-entry helper review aligned to the 9 helper-owned domains", () => {
    expect(reviewSkill).toContain("supported config-entry family: domain is one of `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`");
    expect(reviewChecks).toContain("Helper (config-entry family): minimal config-entry review");
  });

  it("keeps standalone bulk review separate from post-write output", () => {
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
    expect(reviewSkill).toContain("Section 5 — Questions to consider");
    expect(reviewSkill).toContain("Section 6 — Suggestions");
    expect(reviewSkill).toContain("Section 7 — Summary");
    expect(reviewSkill).toContain("Section 8 — Instant help");
  });
});
