import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha cross-skill integration", () => {
  it("routes write flow through resolve + preview + apply + review phases", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(writeSkill).toContain("Phase 1: Resolve (Agent)");
    expect(writeSkill).toContain("Phase 2: Preview + Confirm (Main Thread)");
    expect(writeSkill).toContain("Phase 3: Apply + Verify (Agent)");
    expect(writeSkill).toContain("Phase 4: Post-Write Review");
    expect(writeSkill).toContain("skills/review/SKILL.md");
    expect(writeSkill).toContain("full-replacement merge (base=current, overlay=user changes)");
    expect(writeSkill).toContain("confirm:<token>");
    expect(writeSkill).toContain("full YAML config");
    expect(writeSkill).toContain("Fallback: If agent dispatch unavailable");
  });

  it("keeps write skill wired to shared relay + best-practices references", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(writeSkill).toContain("skills/ha-nova/relay-api.md");
    expect(writeSkill).toContain("skills/ha-nova/best-practices.md");
    expect(writeSkill).toContain("skills/ha-nova/agents/resolve-agent.md");
    expect(writeSkill).toContain("skills/ha-nova/agents/apply-agent.md");
    expect(writeSkill).toContain("skills/review/SKILL.md");
  });

  it("keeps write skill concise and phase-driven", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(writeSkill).toContain("## Bootstrap (once per session)");
    expect(writeSkill).toContain("~/.config/ha-nova/relay health");
    expect(writeSkill).toContain("If this fails, run onboarding: `npm run onboarding:macos`.");
    expect(writeSkill).toContain("## Flow");
    expect(writeSkill).toContain("Fallback: If agent dispatch unavailable");
    expect(writeSkill).toContain("## Safety");
    expect(writeSkill).toContain("Agents must use Relay only; no MCP, no direct HA API");
    expect(writeSkill).toContain('description: Use when creating, updating, or deleting');
    expect(writeSkill).not.toContain("RELAY_BASE_URL");
    expect(writeSkill).not.toContain("RELAY_AUTH_TOKEN");
    expect(writeSkill).not.toContain("## Lazy Load Contract");
    expect(writeSkill).not.toContain("## Relay API Injection Rules");
  });

  it("includes proactive suggestions and pre-write checks in write skill", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    // Phase 1 extracts suggested_enhancements from resolve agent
    expect(writeSkill).toContain("suggested_enhancements");

    // Phase 2 Step 3a: suggestions flow
    expect(writeSkill).toContain("3a) Suggestions");
    expect(writeSkill).toContain("max 4, numbered");
    expect(writeSkill).toContain('or "skip"');
    expect(writeSkill).toContain("merge accepted into config BEFORE preview");
    expect(writeSkill).toContain("SUGGESTED_ENHANCEMENTS: none");
    expect(writeSkill).toContain("skip for `delete`");

    // Phase 2 Step 3b: pre-write static checks
    expect(writeSkill).toContain("3b) Static Checks");
    expect(writeSkill).toContain("analytically on the draft YAML");
    expect(writeSkill).toContain("CRITICAL/HIGH");
    expect(writeSkill).toContain("MEDIUM/LOW");
    expect(writeSkill).toContain("dedup in Phase 4");
  });

  it("includes HA normalization awareness and dedup in post-write review", () => {
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    // HA plural aliasing awareness
    expect(writeSkill).toContain("trigger");
    expect(writeSkill).toContain("triggers");
    expect(writeSkill).toContain("plural aliasing");

    // Dedup rule with example
    expect(writeSkill).toContain("Dedup");
    expect(writeSkill).toContain("MUST NOT repeat");
    expect(writeSkill).toContain("R-05");

    // Post-write review format template
    expect(writeSkill).toContain("Config Findings:");
    expect(writeSkill).toContain("Collision Scan:");
    expect(writeSkill).toContain("Advisory:");
  });
});
