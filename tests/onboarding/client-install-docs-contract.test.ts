import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

function expectOverlayToPointBackToReadme(doc: string): void {
  expect(doc).toContain("This page only covers");
  expect(doc).toContain("README.md");
  expect(doc).toContain("https://github.com/markusleben/ha-nova/releases/latest");
  expect(doc).not.toContain("**Stable install (recommended)**");
  expect(doc).not.toContain("ha-nova uninstall --purge");
  expect(doc).not.toContain("legacy-uninstall");
  expect(doc).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
  expect(doc).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
}

describe("client install docs contract", () => {
  const readme = readFileSync("README.md", "utf8");
  const governance = readFileSync("docs/reference/documentation-governance.md", "utf8");
  const claudeInstall = readFileSync(".claude/INSTALL.md", "utf8");
  const codexInstall = readFileSync(".codex/INSTALL.md", "utf8");
  const geminiInstall = readFileSync(".gemini/INSTALL.md", "utf8");
  const opencodeInstall = readFileSync(".opencode/INSTALL.md", "utf8");

  it("keeps the README at product level, not client-migration level", () => {
    expect(readme).not.toContain("claude install");
    expect(readme).not.toContain("npm.cmd");
    expect(readme).not.toContain("WSL");
    expect(readme).not.toContain("5 tested clients");
    expect(readme).toContain("Current validation matrix:");
    expect(readme).toContain("ha-nova uninstall --purge");
    expect(readme).toContain("**Stable install (recommended)**");
    expect(readme).toContain("Copy the one-liner for your OS");
    expect(readme).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(readme).toContain("HA NOVA itself does not need Node.js for normal use, but some AI clients do.");
    expect(readme).toContain("Git for Windows / Git Bash");
    expect(readme).toContain("Gemini CLI needs Node.js");
    expect(readme).toContain("Default `ha-nova uninstall` is now a standard remove.");
    expect(readme).toContain("Use `--purge` for the old full-cleanup behavior.");
    expect(readme).toContain("at least one supported client is already installed and runnable");
    expect(readme).toContain("no second terminal command is needed");
    expect(readme).toContain("HA NOVA still installs locally and tells you which client prerequisite is still missing.");
    expect(readme).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(readme).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
  });

  it("keeps client overlays scoped to client-specific deltas", () => {
    expectOverlayToPointBackToReadme(claudeInstall);
    expectOverlayToPointBackToReadme(codexInstall);
    expectOverlayToPointBackToReadme(geminiInstall);
    expectOverlayToPointBackToReadme(opencodeInstall);
  });

  it("documents Claude-specific setup, Windows caveats, and local marketplace behavior", () => {
    expect(claudeInstall).toContain("Claude Code");
    expect(claudeInstall).toContain("Claude Desktop");
    expect(claudeInstall).toContain("Code");
    expect(claudeInstall).toContain("Git for Windows");
    expect(claudeInstall).toContain("Git Bash");
    expect(claudeInstall).toContain("Add to PATH");
    expect(claudeInstall).toContain("irm https://claude.ai/install.ps1 | iex");
    expect(claudeInstall).toContain("claude --version");
    expect(claudeInstall).toContain("claude doctor");
    expect(claudeInstall).toContain("skips Claude on Windows");
    expect(claudeInstall).toContain("WSL");
    expect(claudeInstall).toContain("CLAUDE_CODE_GIT_BASH_PATH");
    expect(claudeInstall).toContain("settings.json");
    expect(claudeInstall).toContain("claude install");
    expect(claudeInstall).toContain("npm.cmd uninstall -g @anthropic-ai/claude-code");
    expect(claudeInstall).toContain("ha-nova setup claude");
    expect(claudeInstall).toContain("ha-nova update");
    expect(claudeInstall).toContain("ha-nova doctor");
    expect(claudeInstall).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(claudeInstall).toContain("install-local-skills.sh claude");
    expect(claudeInstall).toContain("claude --plugin-dir");
    expect(claudeInstall).toContain("/ha-nova:read");
    expect(claudeInstall).not.toContain("ha-nova setup codex");
    expect(claudeInstall).not.toContain("ha-nova setup gemini");
    expect(claudeInstall).not.toContain("ha-nova setup opencode");
  });

  it("documents Codex, Gemini, and OpenCode deltas without inheriting Claude-only behavior", () => {
    expect(codexInstall).toContain("Codex CLI");
    expect(codexInstall).toContain("Install the Codex client separately");
    expect(codexInstall).toContain("ha-nova setup codex");
    expect(codexInstall).toContain("ha-nova check-update");
    expect(codexInstall).toContain("still early and not fully tested yet");

    expect(geminiInstall).toContain("Gemini CLI");
    expect(geminiInstall).toContain("Install the Gemini client separately");
    expect(geminiInstall).toContain("ha-nova setup gemini");
    expect(geminiInstall).toContain("ha-nova check-update");
    expect(geminiInstall).toContain("basic Windows validation");
    expect(geminiInstall).toContain("Node.js");
    expect(geminiInstall).toContain("node --version");
    expect(geminiInstall).toContain("gemini --version");
    expect(geminiInstall).toContain("open a fresh PowerShell window");
    expect(geminiInstall).toContain("skips Gemini");
    expect(geminiInstall).toContain("~/.gemini/skills/ha-nova-*");

    expect(opencodeInstall).toContain("OpenCode");
    expect(opencodeInstall).toContain("Install the OpenCode client separately");
    expect(opencodeInstall).toContain("ha-nova setup opencode");
    expect(opencodeInstall).toContain("ha-nova check-update");
    expect(opencodeInstall).toContain("WSL");
    expect(opencodeInstall).toContain("still early and not fully tested yet");

    expect(codexInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(geminiInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(opencodeInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
  });

  it("classifies per-client install docs as active derived install overlays", () => {
    expect(governance).toContain("`.claude/INSTALL.md`, `.codex/INSTALL.md`, `.gemini/INSTALL.md`, `.opencode/INSTALL.md`");
    expect(governance).toContain("client-specific install overlays");
    expect(governance).toContain("cover client deltas only");
    expect(governance).toContain("point back to `README.md`");
  });

  it("keeps README and Windows client overlays aligned on prerequisites and validation depth", () => {
    expect(readme).toContain("Claude currently has the deepest Windows validation.");
    expect(readme).toContain("Gemini CLI has basic Windows validation for this release.");
    expect(geminiInstall).toContain("Gemini has basic Windows validation for this release.");
    expect(claudeInstall).toContain("Claude is the Windows path we have tested most for this release.");
  });
});
