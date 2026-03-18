import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("project docs contract", () => {
  const project = readFileSync("PROJECT.md", "utf8");

  it("describes the active Claude installation model", () => {
    expect(project).toContain("Claude Desktop");
    expect(project).toContain("Code tab");
    expect(project).toContain("same Claude integration path");
    expect(project).toContain("GitHub marketplace path");
    expect(project).not.toContain("planned after Codex/Claude Code flow is stable");
    expect(project).not.toContain("no public package yet");
    expect(project).not.toContain("registers the local bundle as a Claude plugin");
  });

  it("tracks the current skill inventory and reference set", () => {
    expect(project).toContain("fallback");
    expect(project).toContain("automation-patterns.md");
    expect(project).not.toContain("guide, onboarding");
  });

  it("scopes the English-only policy to skills and skill-like source docs", () => {
    expect(project).toContain("skills and skill-like source docs stay English-only");
    expect(project).not.toContain("English-only across the whole project");
  });
});
