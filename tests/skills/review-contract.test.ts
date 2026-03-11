import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const reviewSkill = readFileSync("skills/review/SKILL.md", "utf8");
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
const reviewAgent = readFileSync("skills/ha-nova/agents/review-agent.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");
const contributingDoc = readFileSync("CONTRIBUTING.md", "utf8");

describe("review contract", () => {
  it("documents the internal check taxonomy", () => {
    expect(reviewSkill).toContain("Check Taxonomy (internal only)");
    expect(reviewSkill).toContain("Category letter = family");
    expect(reviewSkill).toContain("Severity is separate from the code");
    expect(reviewSkill).toContain("never show them in user-facing output");
  });

  it("documents the new helper threshold checks", () => {
    expect(reviewSkill).toContain("H-09 [MEDIUM → HIGH]");
    expect(reviewSkill).toContain("H-10 [LOW]");
    expect(reviewSkill).toContain("`>`/`>=` is risky near `min`");
    expect(reviewSkill).toContain("`<`/`<=` is risky near `max`");
    expect(reviewSkill).toContain("within `1 × step`");
  });

  it("documents live helper evidence for threshold checks", () => {
    expect(reviewSkill).toContain("Helper Threshold Evidence");
    expect(reviewSkill).toContain('/api/states/<helper_entity_id>');
    expect(reviewSkill).toContain("attributes.min");
    expect(reviewSkill).toContain("attributes.max");
    expect(reviewSkill).toContain("attributes.step");
    expect(reviewSkill).toContain("relative to `min`, not `value % step`");
    expect(reviewSkill).toContain("Do not emit R-10 just because H-09 matched");
  });

  it("keeps shared references aligned to H-01..H-10", () => {
    expect(writeSkill).toContain("H-01..H-10");
    expect(helperSkill).toContain("H-01..H-10");
    expect(reviewAgent).toContain("H-01..H-10");
    expect(architectureDoc).toContain("H-01..H-10");
  });

  it("documents contributor-facing taxonomy entry points", () => {
    expect(architectureDoc).toContain("## Review Check Taxonomy");
    expect(architectureDoc).toContain("`H` = Helper-specific");
    expect(architectureDoc).toContain("`R` = Reliability");
    expect(contributingDoc).toContain("Review Check Taxonomy");
    expect(contributingDoc).toContain("docs/reference/skill-architecture.md");
    expect(contributingDoc).toContain("skills/review/SKILL.md");
  });
});
