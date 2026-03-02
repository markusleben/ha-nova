import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { parseIntentMatrix } from "./helpers/intent-matrix.js";

describe("ha cross-skill integration", () => {
  it("keeps automation write companions aligned with CRUD and best-practices docs", () => {
    const matrix = parseIntentMatrix();
    const automationCrud = readFileSync("skills/ha-automation-crud.md", "utf8");
    const bestPractices = readFileSync("skills/ha-automation-best-practices.md", "utf8");

    const create = matrix.get("automation.create");
    const update = matrix.get("automation.update");

    expect(create?.companions).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md",
      "$NOVA_REPO_ROOT/skills/ha-safety.md",
    ]);
    expect(update?.companions).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md",
      "$NOVA_REPO_ROOT/skills/ha-safety.md",
    ]);

    expect(automationCrud).toContain("Required Companion Skill for Writes");
    expect(automationCrud).toContain("ha-automation-best-practices");
    expect(bestPractices).toContain("Refresh Snapshot Gate (Mandatory)");
    expect(bestPractices).toContain("`delete` operations are exempt from the refresh gate");
  });

  it("keeps installed skill mirror identical to repo router", () => {
    const repoSkill = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(repoSkill).toBe(installedSkill);
  });
});
