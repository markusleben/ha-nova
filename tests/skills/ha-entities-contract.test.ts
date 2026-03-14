import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha entity-discovery contract", () => {
  it("uses relay-cli bootstrap instead of repo-root env eval", () => {
    const content = readFileSync("skills/entity-discovery/SKILL.md", "utf8");

    expect(content).toContain("ha-nova relay health");
    expect(content).toContain("ha-nova setup");
    expect(content).not.toContain("git rev-parse");
    expect(content).not.toContain("macos-onboarding.sh");
  });

  it("prefers entity registry over get_states and handles ambiguity", () => {
    const content = readFileSync("skills/entity-discovery/SKILL.md", "utf8");

    expect(content).toContain("entity_registry/list");
    expect(content).toContain("Never dump raw");
    expect(content).toContain("never guess entity IDs");
    expect(content).toContain("ask one selection question");
  });
});
