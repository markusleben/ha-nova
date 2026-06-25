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
  const antigravityInstall = readFileSync(".antigravity/INSTALL.md", "utf8");
  const opencodeInstall = readFileSync(".opencode/INSTALL.md", "utf8");
  const hermesInstall = readFileSync(".hermes/INSTALL.md", "utf8");
  const hermesReleaseGate = readFileSync("docs/work/2026-04-24-hermes-release-claim-gating.md", "utf8");

  it("keeps the README at product level, not client-migration level", () => {
    expect(readme).not.toContain("claude install");
    expect(readme).not.toContain("npm.cmd");
    expect(readme).not.toContain("5 tested clients");
    expect(readme).toContain("ha-nova uninstall --purge");
    expect(readme).toContain("Copy the one-liner for your OS");
    expect(readme).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(readme).toContain("Git for Windows / Git Bash");
    expect(readme).toContain("Google Antigravity CLI must provide `agy`");
    // Hermes is now a listed (preview) client like the others; the gate is resolved.
    expect(readme).not.toContain("docs/reference/hermes-platform-validation.md");
    expect(readme).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(readme).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
  });

  it("keeps client overlays scoped to client-specific deltas", () => {
    expectOverlayToPointBackToReadme(claudeInstall);
    expectOverlayToPointBackToReadme(codexInstall);
    expectOverlayToPointBackToReadme(antigravityInstall);
    expectOverlayToPointBackToReadme(opencodeInstall);
    expectOverlayToPointBackToReadme(hermesInstall);
  });

  it("keeps reciprocal client overlay links on the current Antigravity overlay", () => {
    for (const overlay of [claudeInstall, codexInstall, opencodeInstall, hermesInstall]) {
      expect(overlay).toContain("Google Antigravity CLI: `.antigravity/INSTALL.md`");
      expect(overlay).not.toContain(".gemini/INSTALL.md");
    }
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
    expect(claudeInstall).not.toContain("ha-nova setup antigravity");
    expect(claudeInstall).not.toContain("ha-nova setup opencode");
  });

  it("documents Codex, Antigravity, OpenCode, and Hermes deltas without inheriting Claude-only behavior", () => {
    expect(codexInstall).toContain("Codex CLI");
    expect(codexInstall).toContain("Install the Codex client separately");
    expect(codexInstall).toContain("ha-nova setup codex");
    expect(codexInstall).toContain("ha-nova check-update");
    expect(codexInstall).not.toContain("claude install");
    expect(codexInstall).not.toContain("CLAUDE_CODE_GIT_BASH_PATH");
    expect(codexInstall).not.toContain("Git Bash");

    expect(antigravityInstall).toContain("Google Antigravity CLI");
    expect(antigravityInstall).toContain("Install the Antigravity client separately");
    expect(antigravityInstall).toContain("ha-nova setup antigravity");
    expect(antigravityInstall).toContain("ha-nova check-update");
    expect(antigravityInstall).toContain("agy --version");
    expect(antigravityInstall).toContain("open a fresh PowerShell window");
    expect(antigravityInstall).toContain("~/.gemini/config/skills/ha-nova-*");
    expect(antigravityInstall).toContain("ha-nova setup gemini");
    expect(antigravityInstall).not.toContain("claude install");
    expect(antigravityInstall).not.toContain("CLAUDE_CODE_GIT_BASH_PATH");
    expect(antigravityInstall).not.toContain("Git Bash");

    expect(opencodeInstall).toContain("OpenCode");
    expect(opencodeInstall).toContain("Install the OpenCode client separately");
    expect(opencodeInstall).toContain("ha-nova setup opencode");
    expect(opencodeInstall).toContain("ha-nova check-update");
    expect(opencodeInstall).toContain("WSL");
    expect(opencodeInstall).not.toContain("claude install");
    expect(opencodeInstall).not.toContain("CLAUDE_CODE_GIT_BASH_PATH");
    expect(opencodeInstall).not.toContain("Git Bash");

    expect(hermesInstall).toContain("Hermes Agent");
    expect(hermesInstall).toContain("ha-nova setup hermes");
    expect(hermesInstall).toContain("ha-nova check-update");
    expect(hermesInstall).toContain("What Supported Means Here");
    expect(hermesInstall).toContain("Platform Routing");
    expect(hermesInstall).toContain("Network Model");
    expect(hermesInstall).toContain("Supported with limitation");
    expect(hermesInstall).toContain("Not supported");
    expect(hermesInstall).toContain("Maintainer-validated");
    expect(hermesInstall).toContain("Community validation");
    expect(hermesInstall).toContain("GNOME Keyring");
    expect(hermesInstall).toContain("WSL2");
    expect(hermesInstall).toContain("Windows native");
    expect(hermesInstall).toContain("local-first HA NOVA client");
    expect(hermesInstall).toContain("same home network or a private VPN/overlay route");
    expect(hermesInstall).toContain("generic public VPS");
    expect(hermesInstall).toContain("Do not expose the HA NOVA Relay directly to the public internet.");
    expect(hermesInstall).toContain("docs/reference/hermes-platform-validation.md");
    expect(hermesInstall).toContain("~/.hermes/skills/ha-nova/");
    expect(hermesInstall).toContain("ha-nova-read");
    expect(hermesInstall).toContain("stays on this machine");
    expect(hermesInstall).toContain("not your Relay token");
    expect(hermesInstall).toContain("not sent to the Relay, Home Assistant, Hermes, or any AI provider");
    expect(hermesInstall).toContain("doctor points you to `ha-nova setup hermes`");
    expect(hermesInstall).toContain("skills_list");
    expect(hermesInstall).toContain("skill_view");

    expect(hermesInstall).toContain("Hermes Agent");
    expect(hermesInstall).toContain("early support (preview)");
    expect(hermesInstall).toContain("ha-nova setup hermes");
    expect(hermesInstall).toContain("ha-nova check-update");
    expect(hermesInstall).toContain("What Supported Means Here");
    expect(hermesInstall).toContain("Platform Routing");
    expect(hermesInstall).toContain("Network Model");
    expect(hermesInstall).toContain("GNOME Keyring");
    expect(hermesInstall).toContain("WSL2");
    expect(hermesInstall).toContain("Windows native");
    expect(hermesInstall).toContain("~/.hermes/skills/ha-nova/");
    expect(hermesInstall).toContain("ha-nova-read");
    expect(hermesInstall).toContain("skills_list");
    expect(hermesInstall).toContain("skill_view");

    expect(codexInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(antigravityInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(opencodeInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(hermesInstall).not.toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(hermesInstall).not.toContain("CLAUDE_CODE_GIT_BASH_PATH");
    expect(hermesInstall).not.toContain("ha-nova setup claude");
    expect(hermesInstall).not.toContain("ha-nova setup antigravity");
    expect(hermesInstall).not.toContain("ha-nova setup opencode");
    expect(hermesInstall).not.toContain("Git Bash");
  });

  it("classifies per-client install docs as active derived install overlays", () => {
    expect(governance).toContain("`.claude/INSTALL.md`, `.codex/INSTALL.md`, `.antigravity/INSTALL.md`, `.opencode/INSTALL.md`, `.hermes/INSTALL.md`");
    expect(governance).toContain("client-specific install overlays");
    expect(governance).toContain("cover client deltas only");
    expect(governance).toContain("point back to `README.md`");
  });

  it("keeps README and Windows client overlays aligned on prerequisites and Windows guidance", () => {
    expect(readme).toContain("Windows");
    expect(readme).toContain("Git for Windows / Git Bash");
    expect(readme).toContain("Google Antigravity CLI must provide `agy`");
    expect(claudeInstall).toContain("Windows Notes");
    expect(antigravityInstall).toContain("Windows Notes");
    expect(antigravityInstall).toContain("agy --version");
    expect(antigravityInstall).toContain("open a fresh PowerShell window");
    expect(claudeInstall).toContain("Git for Windows");
    expect(claudeInstall).toContain("Git Bash");
  });

  it("lists Hermes as a preview client now that the release-claim gate is resolved", () => {
    // v0.6.0 resolved the gate: Hermes is in the public README as a PREVIEW client,
    // honestly scoped (Linux validated; macOS/WSL2 experimental; native Windows unsupported).
    expect(hermesReleaseGate).toContain("Status: resolved (v0.6.0)");
    expect(hermesReleaseGate).toContain("listed in the public `README.md` as a **preview** client");
    expect(hermesInstall).toContain("client since v0.6.0");
    expect(readme).toContain("Hermes Agent");
    expect(readme).toContain("Hermes is in preview");
    expect(readme).toContain("native Windows isn't supported");
  });
});
