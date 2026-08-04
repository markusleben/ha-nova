import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #440: Home Status is a table-first, cause-oriented report with progressive
// detail — actionable findings name their exact targets.

const health = readFileSync("skills/health/SKILL.md", "utf8");
const availability = readFileSync(
  "skills/health/availability-analysis.md",
  "utf8",
);
const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");

describe("home status table-first redesign (#440)", () => {
  it("declares both report modes with the Explained+Private default", () => {
    expect(health).toContain("Default: `Explained + Private`.");
    expect(health).toContain("always visible");
    expect(health).toContain("`Compact`");
    expect(health).toContain("`Full`");
    expect(health).toContain("Census participation never affects");
  });

  it("orders the report cause-before-symptom in ten blocks", () => {
    expect(health).toContain("## Report Order (table-first, cause before symptom)");
    expect(health).toContain("4. Integration findings with their owned entity impact");
    expect(health).toContain("5. Remaining entity availability findings");
    expect(health).toContain("10. One safest next step");
    // Universal block shape and non-color severity.
    expect(health).toContain("assessment BEFORE the table");
    expect(health).toContain("Severity never depends on color alone");
  });

  it("names exact targets for repairs and never invents them", () => {
    expect(health.replace(/\s+/g, " ")).toContain("exact repository display name");
    expect(health).toContain("one shared restart resolves several rows");
    expect(health).toContain('"not supplied by source"');
    expect(health).toContain("never fuzzy-match update entities");
  });

  it("keeps low values in their four semantic classes", () => {
    expect(health).toContain("replaceable device battery");
    expect(health).toContain("vehicle/storage SOC");
    expect(health).toContain("unclassified low percentage");
    expect(health).toContain("never from an entity name");
  });

  it("assigns every raw row exactly once and reconciles exactly", () => {
    expect(availability).toContain("## Assignment precedence (every raw row exactly once)");
    expect(availability).toContain("the category sum MUST equal\nthe raw total");
    expect(availability).toContain("`unattributed`");
    expect(availability).toContain('say\n"associated states"');
  });

  it("shares the progressive-detail contract in output-rules", () => {
    expect(outputRules).toContain("## Progressive Detail");
    expect(outputRules).toContain("total count, the shown count, the omitted count");
    expect(outputRules).toContain("results may have changed");
  });

  it("keeps missing data explicit", () => {
    expect(health).toContain('"not evaluated"');
    expect(health).toContain('"source unavailable"');
    expect(health).toContain("never the full outage");
  });
});
