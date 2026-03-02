import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha entity-discovery contract", () => {
  it("uses relay-cli bootstrap instead of repo-root env eval", () => {
    const content = readFileSync(".agents/skills/ha-nova-entity-discovery/SKILL.md", "utf8");

    expect(content).toContain("~/.config/ha-nova/relay health");
    expect(content).toContain("npm run onboarding:macos");
    expect(content).not.toContain("git rev-parse");
    expect(content).not.toContain("macos-onboarding.sh");
  });

  it("uses one-shot ws get_states and explicit ambiguity handling", () => {
    const content = readFileSync(".agents/skills/ha-nova-entity-discovery/SKILL.md", "utf8");

    expect(content).toContain("~/.config/ha-nova/relay ws");
    expect(content).toContain('{"type":"get_states"}');
    expect(content).toContain("never guess entity IDs");
    expect(content).toContain("ask one selection question");
  });
});
