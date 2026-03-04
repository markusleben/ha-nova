import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha safety contract", () => {
  it("enforces tiered confirmations and no-guessing policy", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/ha-nova/write/SKILL.md", "utf8");

    expect(router).toContain("Never guess entity IDs, service names, or config IDs.");
    expect(router).toContain("create`/`update`: natural confirmation");
    expect(router).toContain("token confirmation `confirm:<token>`");
    expect(writeSkill).toContain("Confirmation: create/update=natural, delete=tokenized `confirm:<token>`.");
    expect(writeSkill).toContain("No guessing entity_ids; resolve or ask");
  });

  it("requires structured failure output", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/ha-nova/write/SKILL.md", "utf8");

    expect(router).toContain("what failed");
    expect(router).toContain("why it failed");
    expect(router).toContain("next concrete step");
    expect(writeSkill).toContain("No raw curl/JSON in output.");
    expect(writeSkill).toContain("## References");
  });
});
