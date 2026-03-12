import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha safety contract", () => {
  it("enforces tiered confirmations and no-guessing policy", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/ha-nova-write/SKILL.md", "utf8");

    expect(router).toContain("Never guess entity IDs, service names, or config IDs.");
    expect(router).toContain("create`/`update`: natural confirmation");
    expect(router).toContain("token confirmation `confirm:<token>`");
    expect(writeSkill).toContain("Confirmation: create/update=natural, delete=tokenized `confirm:<token>`");
    expect(writeSkill).toContain("No guessing entity_ids; resolve or ask");
  });

  it("requires structured failure output", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/ha-nova-write/SKILL.md", "utf8");

    expect(router).toContain("what failed");
    expect(router).toContain("why it failed");
    expect(router).toContain("next concrete step");
    expect(writeSkill).toContain("No raw curl/JSON in output.");
    expect(writeSkill).toContain("## References");
  });

  it("enforces fallback skill as mandatory for raw relay writes", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const fallback = readFileSync("skills/ha-nova-fallback/SKILL.md", "utf8");

    // Context skill: dispatch table marks fallback as mandatory
    expect(context).toContain("mandatory fallback");
    expect(context).toContain("never skip");

    // Context skill: safety baseline blocks raw relay writes without a skill
    expect(context).toContain("No raw relay writes without a skill");
    expect(context).toContain("ha-nova:ha-nova-fallback");

    // Context skill: concrete scary example (lovelace overwrite)
    expect(context).toContain("lovelace/config/save");
    expect(context).toContain("full-document overwrites");
    expect(context).toContain("Skipping it risks data loss");

    // Fallback skill: description says mandatory
    expect(fallback).toContain("Mandatory fallback");

    // Fallback skill: anti-patterns
    expect(fallback).toContain("Anti-Patterns");
    expect(fallback).toContain("lovelace/config/save");
    expect(fallback).toContain("trial-and-error");
    expect(fallback).toContain("Probing write endpoints");

    // Fallback skill: write safety by endpoint type
    expect(fallback).toContain("Full-document overwrite");
    expect(fallback).toContain("Field-level list replace");
    expect(fallback).toContain("Write Safety by Endpoint Type");
  });

  it("requires concise correction of invalid Home Assistant premises", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/ha-nova-write/SKILL.md", "utf8");
    const fallbackSkill = readFileSync("skills/ha-nova-fallback/SKILL.md", "utf8");

    expect(router).toContain("invalid Home Assistant premises");
    expect(router).toContain("briefly and technically");
    expect(writeSkill).toContain("invalid Home Assistant premise");
    expect(writeSkill).toContain("before continuing");
    expect(fallbackSkill).toContain("invalid Home Assistant premises");
    expect(fallbackSkill).toContain("wrong premise");
  });
});
