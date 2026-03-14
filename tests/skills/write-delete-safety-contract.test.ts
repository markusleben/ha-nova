import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const refactorGuide = readFileSync("skills/ha-nova/safe-refactoring.md", "utf8");

describe("write delete safety contract", () => {
  it("requires delete previews to surface consumer-check results before confirmation", () => {
    expect(writeSkill).toContain("Delete preview MUST include the consumer-check result before confirmation");
    expect(writeSkill).toContain("either the affected consumers or an explicit no-consumer result");
  });

  it("requires destructive writes to stay unclaimed until verification proves the target is gone", () => {
    expect(writeSkill).toContain("Do not report destructive success until verification proves the target is gone");
    expect(refactorGuide).toContain("A delete is not done until follow-up verification confirms the target is gone");
    expect(refactorGuide).toContain("Do not present a destructive change as complete when consumer impact is still unresolved");
  });
});
