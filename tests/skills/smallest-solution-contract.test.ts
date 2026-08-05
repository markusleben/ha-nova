import { existsSync, readFileSync, readdirSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #452: the smallest complete solution is the silent default across all
// write-capable skills — one shared contract, wired at solution-design time
// (context-skill-only is insufficient because sub-skills load independently;
// output-format-only is too late to govern selection).

const contract = readFileSync("skills/ha-nova/smallest-solution.md", "utf8");
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");
const resolveAgent = readFileSync("skills/ha-nova/agents/resolve-agent.md", "utf8");
const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");

describe("smallest complete solution contract (#452)", () => {
  it("states the selection rules, silently applied", () => {
    expect(contract).toContain("Applied silently");
    expect(contract).toContain("Simple never means partial");
    expect(contract).toContain("hypothetical future needs");
    // Safety and verification are never trimmed as complexity.
    expect(contract).toContain("Never treat as optional complexity");
    expect(contract).toContain("verification");
    // Explicit advanced requirements stay fully represented.
    expect(contract).toContain("fully represented");
    // Native primitives and honest reuse without coupling.
    expect(contract).toContain("native Home Assistant primitives");
    expect(contract).toContain("couple unrelated behaviour");
    expect(contract).toContain("Never overload an existing helper");
  });

  it("caps unsolicited improvements at two, evidence-backed, smallest first", () => {
    expect(contract).toContain("at most TWO per logical task");
    expect(contract).toContain("smallest intervention first");
    expect(contract).toContain("do not\n  invent suggestions");
    expect(contract).toContain("BEFORE its single preview and\n  write cycle");
    expect(contract).toContain('Not "unsolicited"');
  });

  it("wires the contract at every solution-design entry point", () => {
    // write: draft baseline + suggestion cap + always-load reference
    expect(writeSkill).toContain(
      "Every draft follows `skills/ha-nova/smallest-solution.md`",
    );
    expect(writeSkill).toContain("max 2, smallest intervention first");
    expect(writeSkill).not.toContain("max 4, numbered/menu");
    expect(writeSkill).toContain("`skills/ha-nova/smallest-solution.md`\n");
    // resolve agent generates at most two evidence-backed enhancements
    expect(resolveAgent).toContain("at most 2 concrete optional improvements");
    expect(resolveAgent).toContain("never invent suggestions to fill the list");
    // output rules differentiate improvement vs value-default caps
    expect(outputRules).toContain("unsolicited improvement suggestions max 2");
    expect(outputRules).toContain("value-default suggestions");
    // helper: unconditional draft rule, plus value defaults stay at 4 while
    // feature offers route to the contract
    expect(helperSkill).toContain(
      "Drafts follow `skills/ha-nova/smallest-solution.md`",
    );
    expect(helperSkill).toContain("max 2");
    // Every skill with the canonical write flow loads independently and must
    // carry the draft rule itself — the pre-preview sentence marks that class,
    // so new mutating skills are enforced automatically.
    const skillFiles = readdirSync("skills", { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => `skills/${entry.name}/SKILL.md`)
      .filter((path) => existsSync(path));
    const writeFlowSkills = skillFiles.filter((path) =>
      readFileSync(path, "utf8").includes(
        "authorize drafting and preview only",
      ),
    );
    expect(writeFlowSkills.length).toBeGreaterThanOrEqual(23);
    for (const path of writeFlowSkills) {
      expect(
        readFileSync(path, "utf8"),
        `${path} has the write flow and must wire the draft rule`,
      ).toContain(
        /skills\/(write|helper)\//.test(path)
          ? "`skills/ha-nova/smallest-solution.md`"
          : "Drafts follow `skills/ha-nova/smallest-solution.md`",
      );
    }
    // context skill carries the baseline for dispatch-level awareness
    expect(contextSkill).toContain("smallest complete solution as the silent default");
  });
});
