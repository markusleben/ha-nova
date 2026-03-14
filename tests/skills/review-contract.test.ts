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
    expect(reviewAgent).toContain("H-01..H-10");
    expect(architectureDoc).toContain("H-01..H-10");
  });

  it("keeps live-evidence helper checks staged in write/helper flows", () => {
    expect(writeSkill).toContain("H-01..H-08");
    expect(writeSkill).toContain("Defer H-09/H-10 to Phase 4");
    expect(helperSkill).toContain("Apply H-01..H-08 directly");
    expect(helperSkill).toContain("Only evaluate H-09/H-10");
    expect(helperSkill).toContain("direct helper-backed threshold");
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
    expect(reviewSkill).toContain("R-01..R-16");
    expect(reviewAgent).toContain("R-01..R-16");
    expect(architectureDoc).toContain("R-01..R-16");
    expect(templateGuidelines).toContain("Event trigger names must be literal strings");
    expect(templateGuidelines).toContain("do not template `event_type:`");
  });
});
