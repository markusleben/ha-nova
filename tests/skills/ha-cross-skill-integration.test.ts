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
});
