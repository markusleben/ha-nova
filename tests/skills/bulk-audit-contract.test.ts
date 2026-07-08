import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const bulkPatterns = readFileSync("skills/ha-nova/bulk-patterns.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const discoverySkill = readFileSync("skills/entity-discovery/SKILL.md", "utf8");
const readSkill = readFileSync("skills/read/SKILL.md", "utf8");
const reviewSkill = readFileSync("skills/review/SKILL.md", "utf8");

describe("bulk audit contract", () => {
  it("defines shared selector semantics and deterministic workset rules", () => {
    expect(bulkPatterns).toContain("Shared contract for multi-target inventory and audit workflows");
    expect(bulkPatterns).toContain("`prefix`");
    expect(bulkPatterns).toContain("`domain`");
    expect(bulkPatterns).toContain("`area`");
    expect(bulkPatterns).toContain("`label`");
    expect(bulkPatterns).toContain("sort by domain, then `entity_id`");
    expect(bulkPatterns).toContain("canonical key `area_id`");
    expect(bulkPatterns).toContain("do not expect a generic `id` field");
    expect(bulkPatterns).toContain("Display at most 20 inventory rows");
    expect(bulkPatterns).toContain("Audit at most 5 targets in one workset");
    expect(bulkPatterns).toContain("One standalone bulk-review request may audit exactly one workset only");
    expect(bulkPatterns).toContain("Never continue automatically into a second workset inside the same response");
    expect(bulkPatterns).toContain("matched N / audited 5 / remaining R");
    expect(bulkPatterns).toContain("Bulk review stays read-only");
    expect(bulkPatterns).toContain("Do not offer Quick-Fix in bulk mode");
    expect(bulkPatterns).toContain('query `search/related` with `item_type:"area"`');
    expect(bulkPatterns).toContain("object keyed by type");
    expect(bulkPatterns).toContain("Helper-in-area is not a first-class bulk selector contract");
    expect(bulkPatterns).toContain("Dedupe the resolved shortlist on canonical `entity_id`");
    expect(bulkPatterns).toContain("Materialize the current audit workset before any config, state, or collision reads");
    expect(bulkPatterns).toContain("Never read configs for matched-but-non-audited remainder targets outside the current audit workset");
    expect(bulkPatterns).toContain("Never resolve `unique_id` values or build a config snapshot for matched-but-non-audited remainder targets outside the current audit workset");
    expect(bulkPatterns).toContain("If collision classification needs one extra target outside the matched remainder set");
    expect(bulkPatterns).toContain("clearly marked as related/collision evidence in the transcript");
    expect(bulkPatterns).toContain("do not prefetch configs, states, or related-item evidence for the remainder");
    expect(bulkPatterns).toContain("## Reference Filters");
    expect(bulkPatterns).toContain("prefer one temp directory with fixed names");
    expect(bulkPatterns).toContain("POSIX examples");
    expect(bulkPatterns).toContain("equivalent native file-writing form while keeping the same filenames and file contents");
    expect(bulkPatterns).toContain("let the first Relay call emit the wrapped inventory object directly");
    expect(bulkPatterns).toContain("prefer `ha-nova relay jq --file` over ad-hoc Node or Python parsers");
    expect(bulkPatterns).toContain("Do not feed the wrapped result file back through the original shortlist jq program");
    expect(bulkPatterns).toContain("keep shared filenames serial or give each concurrent probe its own dedicated payload filename");
    expect(bulkPatterns).toContain("Avoid precedence-sensitive chained string-building filters for multiple fields");
    expect(bulkPatterns).toContain('split(".")[1]');
    expect(bulkPatterns).toContain('(.data.automation // []) | sort');
    expect(bulkPatterns).toContain("plain JSON array of automation `entity_id` strings");
    expect(bulkPatterns).toContain("Keep that array shape for workset trimming and count calculations");
    expect(bulkPatterns).toContain('select(any((.labels // [])[]; . == "label_alpha"))');
    expect(bulkPatterns).toContain('Replace `"label_alpha"` with the resolved canonical `label_id` literal');
    expect(bulkPatterns).toContain('values: ($rows[0:20] | map(.entity_id))');
    expect(bulkPatterns).toContain("rows: ($rows[0:20])");
    expect(bulkPatterns).toContain("When the shortlist is still a plain JSON array of `entity_id` strings");
  });

  it("keeps entity discovery as the bulk inventory surface", () => {
    expect(discoverySkill).toContain("bulk inventory by `prefix`, `domain`, `area`, or `label`");
    expect(discoverySkill).toContain("skills/ha-nova/bulk-patterns.md");
    expect(discoverySkill).toContain("save the shortlist with `--out <result-file>`");
    expect(discoverySkill).toContain("domain, then entity_id");
    expect(discoverySkill).toContain("matched count");
    expect(discoverySkill).toContain("returns the entity array directly in `.data`");
    expect(discoverySkill).toContain("not explicit `prefix` matching");
    expect(discoverySkill).toContain("startswith(...)");
    expect(discoverySkill).toContain("do not trim to 20 inside the initial selector filter");
    expect(discoverySkill).toContain("dedupe first, then sort deterministically, then compute the exact matched count, then apply the 20-row display cap");
    expect(discoverySkill).toContain("do not fetch full YAML for every matched item in one response");
    expect(discoverySkill).toContain("use `search/related` as the primary shortlist source");
    expect(discoverySkill).toContain("Treat the response as a keyed object, not an array");
    expect(discoverySkill).toContain("helper-area semantics are explicitly defined");
    expect(discoverySkill).toContain("area-first bulk discovery by room/area uses `search/related` on the resolved area before keyword heuristics");
    expect(discoverySkill).not.toContain("area → `search/related` on the resolved area third");
  });

  it("keeps read bulk scope inventory-only", () => {
    expect(readSkill).toContain("Multi-target is inventory-only");
    expect(readSkill).toContain("bulk-patterns.md");
    expect(readSkill).toContain("keep full YAML reads single-target only");
    expect(readSkill).toContain("return the compact table only");
    expect(readSkill).toContain('stay inside `read` and follow the area-first `search/related` flow');
    expect(readSkill).toContain("prefer copying `skills/ha-nova/config-body-filter.jq`");
    expect(readSkill).toContain("do not create alternate config-filter filenames");
    expect(readSkill).not.toContain("use entity-discovery skill's area-first `search/related` flow");
  });

  it("adds review bulk mode with aggregate sections and no quick-fix", () => {
    expect(reviewSkill).toContain("Bulk Mode Gate");
    expect(reviewSkill).toContain("If the user asks for a bulk audit by `prefix`, `domain`, `area`, or `label`");
    expect(reviewSkill).toContain("resolved targets `> 1`: enter aggregate multi-target review mode automatically");
    expect(reviewSkill).toContain("more than one resolved target -> current review set = the resolved targets in deterministic order, trimmed to the first 5 only when more than 5 targets match");
    expect(reviewSkill).not.toContain("2-3 resolved targets -> current review set = all resolved targets, reviewed serially in deterministic order");
    expect(reviewSkill).not.toContain("more than 3 resolved targets -> current review set = first 5 targets only");
    expect(reviewSkill).toContain("skills/ha-nova/bulk-patterns.md");
    expect(reviewSkill).toContain("audit only the current workset (max 5 targets)");
    expect(reviewSkill).toContain("stop after that one workset; do not start a second batch in the same standalone request");
    expect(reviewSkill).toContain("do not resolve unique_ids, read configs, read states, or run collision scans for matched-but-non-audited remainder targets outside the current review set");
    expect(reviewSkill).toContain("a narrow collision explanation may inspect one extra target outside the matched remainder");
    expect(reviewSkill).toContain("clearly marked as related/collision evidence in the transcript");
    expect(reviewSkill).toContain("never build a full matched-set config cache or evidence snapshot; if caching helps, cache only the current review set");
    expect(reviewSkill).toContain("resolve `unique_id`, config, state, and related-item evidence per target inside the current workset only; no prefetch for the remaining matched targets");
    expect(reviewSkill).toContain("paste that jq program body exactly as shown");
    expect(reviewSkill).toContain("Preferred: copy `skills/ha-nova/config-body-filter.jq`");
    expect(reviewSkill).toContain("POSIX shell example only:");
    expect(reviewSkill).toContain("overwrite the same `config_filter_file` with the exact canonical line before the first config read");
    expect(reviewSkill).toContain("do not create probe variants or alternate filenames");
    expect(reviewSkill).toContain("do not compare it against a shell-escaped string");
    expect(reviewSkill).toContain('do not wrap it in an `if [ "$line" != ... ]` guard');
    expect(reviewSkill).toContain("do not ask a clarifying question just because multiple matches remain");
    expect(reviewSkill).toContain(
      "skip Steps 4-6 entirely; bulk mode does not offer Quick-Fix, exploratory questions, or single-target suggestion synthesis"
    );
    expect(reviewSkill).toContain("wait for an explicit follow-up request before continuing");
    expect(reviewSkill).toContain("Quick-Fix is single-target only");
    expect(reviewSkill).toContain("For resolved targets `> 1`, return exactly these 6 sections");
    expect(reviewSkill).toContain("Section 1 — Scope");
    expect(reviewSkill).toContain("Section 2 — Summary");
    expect(reviewSkill).toContain("Section 3 — High-Risk Findings");
    expect(reviewSkill).toContain("Section 4 — Repeated Patterns");
    expect(reviewSkill).toContain("Section 5 — Items Checked");
    expect(reviewSkill).toContain("Section 6 — Collisions by Cluster");
    expect(reviewSkill).toContain("bulk workset max 5 targets");
    expect(reviewSkill).toContain("Never build a config snapshot for the full matched set during bulk review");
    expect(reviewSkill).toContain("Summary");
  });

  it("keeps context routing and safety aligned to the bulk split", () => {
    expect(contextSkill).toContain("scale manually with the same rules");
    expect(contextSkill).toContain('"Show all automations with prefix routine_"');
    expect(contextSkill).toContain("ha-nova:entity-discovery");
    expect(contextSkill).toContain('"Review all automations in area Area Alpha"');
    expect(contextSkill).toContain("area-first aggregate review when more than one target resolves");
    expect(contextSkill).not.toContain("resolved set is >3");
    expect(contextSkill).toContain("Bulk review is the exception: it stays read-only and does not offer Quick-Fix.");
    expect(contextSkill).toContain("skills/ha-nova/bulk-patterns.md");
  });
});
