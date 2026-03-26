import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("project docs contract", () => {
  const project = readFileSync("PROJECT.md", "utf8");
  const readme = readFileSync("README.md", "utf8");
  const skillArchitecture = readFileSync("docs/reference/skill-architecture.md", "utf8");

  it("describes the active Claude installation model", () => {
    expect(project).toContain("Claude Desktop");
    expect(project).toContain("Code tab");
    expect(project).toContain("same Claude integration path");
    expect(project).toContain("locally staged release payload");
    expect(project).not.toContain("GitHub marketplace path");
    expect(project).not.toContain("planned after Codex/Claude Code flow is stable");
    expect(project).not.toContain("no public package yet");
    expect(project).not.toContain("registers the local bundle as a Claude plugin");
  });

  it("keeps the Windows public contract at published-and-proven instead of publication-only", () => {
    expect(project).toContain("published and proven");
    expect(project).not.toContain("actually published as a real package");
  });

  it("keeps Linux support wording conservative until a real Secret Service machine was live-tested", () => {
    expect(readme).toContain("Linux uses the same installer and CI smoke path, but this release is not yet fully live-validated on a real Linux machine.");
    expect(readme).toContain("Linux support currently means shared installer/update flow plus CI smoke");
    expect(project).toContain("full release validation still depends on a real Secret Service-backed Linux run");
    expect(project).not.toContain("Linux: fully validated");
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

  it("documents the remaining shell-adjacent helpers as dev shims, not a second product lifecycle", () => {
    expect(skillArchitecture).toContain("Active dev helpers:");
    expect(skillArchitecture).toContain("scripts/dev-sync.sh");
    expect(skillArchitecture).toContain("~/.config/ha-nova/version-check");
    expect(skillArchitecture).toContain("generated");
    expect(skillArchitecture).not.toContain("scripts/update.sh");
    expect(skillArchitecture).not.toContain("scripts/version-check.sh");
    expect(skillArchitecture).toContain("no end-user installer contract");
    expect(skillArchitecture).not.toContain("macos-onboarding.sh");
    expect(skillArchitecture).not.toContain("macos-setup.sh");
  });
});
