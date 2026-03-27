import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("client install docs contract", () => {
  const readme = readFileSync("README.md", "utf8");
  const governance = readFileSync("docs/reference/documentation-governance.md", "utf8");
  const claudeInstall = readFileSync(".claude/INSTALL.md", "utf8");
  const codexInstall = readFileSync(".codex/INSTALL.md", "utf8");
  const geminiInstall = readFileSync(".gemini/INSTALL.md", "utf8");
  const opencodeInstall = readFileSync(".opencode/INSTALL.md", "utf8");

  it("keeps the README at product level, not client-migration level", () => {
    expect(readme).not.toContain("claude install");
    expect(readme).not.toContain("Git for Windows");
    expect(readme).not.toContain("npm.cmd");
    expect(readme).not.toContain("WSL");
    expect(readme).not.toContain("5 tested clients");
    expect(readme).toContain("Current validation matrix:");
    expect(readme).toContain("ha-nova uninstall --purge");
    expect(readme).toContain("**Stable install (recommended)**");
    expect(readme).toContain("Copy the one-liner for your OS");
    expect(readme).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(readme).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(readme).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
  });

  it("documents Claude Windows prerequisites and migration only in the Claude install doc", () => {
    expect(claudeInstall).toContain("Windows");
    expect(claudeInstall).toContain("tested on Windows for this release");
    expect(claudeInstall).toContain("**Stable install (recommended)**");
    expect(claudeInstall).toContain("Copy the one-liner for your OS");
    expect(claudeInstall).toContain("Windows uses a single supported install path: `install.ps1`.");
    expect(claudeInstall).toContain("HA NOVA installs itself on your computer; the Home Assistant connection steps stay guided in the browser.");
    expect(claudeInstall).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(claudeInstall).toContain("Git for Windows");
    expect(claudeInstall).toContain("WSL");
    expect(claudeInstall).toContain("claude install");
    expect(claudeInstall).toContain("npm.cmd");
    expect(claudeInstall).toContain("macOS / Linux only");
    expect(claudeInstall).toContain("install-local-skills.sh claude");
    expect(claudeInstall).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(claudeInstall).toContain("Default behavior for real installs:");
    expect(claudeInstall).toContain("came with your installed HA NOVA version");
    expect(claudeInstall).toContain("HA NOVA itself tells you when a newer release exists");
    expect(claudeInstall).toContain("run `ha-nova update` and then restart Claude");
    expect(claudeInstall).toContain("Local validation / private-RC behavior:");
    expect(claudeInstall).toContain("ha-nova setup claude");
    expect(claudeInstall).toContain("ha-nova doctor");
    expect(claudeInstall).not.toContain("Claude plugin updates then follow the repo");
    expect(claudeInstall).not.toContain("claude plugin marketplace add https://github.com/markusleben/ha-nova");
    expect(claudeInstall).toContain("Claude Desktop in the **Code** tab uses this same Claude integration path.");
    expect(claudeInstall).toContain("Local repo checkout (macOS / Linux only)");
    expect(claudeInstall).toContain("ha-nova uninstall --purge");
    expect(claudeInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(claudeInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(claudeInstall).not.toContain("winget");
    expect(claudeInstall).not.toContain("use the repo root instead");
  });

  it("keeps other client install docs product-focused and platform-aware", () => {
    expect(codexInstall).toContain("Windows");
    expect(geminiInstall).toContain("Windows");
    expect(opencodeInstall).toContain("Windows");
    expect(codexInstall).toContain("**Stable install (recommended)**");
    expect(geminiInstall).toContain("**Stable install (recommended)**");
    expect(opencodeInstall).toContain("**Stable install (recommended)**");
    expect(codexInstall).toContain("Copy the one-liner for your OS");
    expect(geminiInstall).toContain("Copy the one-liner for your OS");
    expect(opencodeInstall).toContain("Copy the one-liner for your OS");
    expect(codexInstall).toContain("Windows uses a single supported install path: `install.ps1`.");
    expect(geminiInstall).toContain("Windows uses a single supported install path: `install.ps1`.");
    expect(opencodeInstall).toContain("Windows uses a single supported install path: `install.ps1`.");
    expect(codexInstall).toContain("HA NOVA installs itself on your computer; the Home Assistant connection steps stay guided in the browser.");
    expect(geminiInstall).toContain("HA NOVA installs itself on your computer; the Home Assistant connection steps stay guided in the browser.");
    expect(opencodeInstall).toContain("HA NOVA installs itself on your computer; the Home Assistant connection steps stay guided in the browser.");
    expect(codexInstall).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(geminiInstall).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(opencodeInstall).toContain("https://github.com/markusleben/ha-nova/releases/latest");
    expect(opencodeInstall).toContain("WSL");
    expect(codexInstall).toContain("still early and not fully tested yet");
    expect(geminiInstall).toContain("basic Windows testing for this release");
    expect(opencodeInstall).toContain("still early and not fully tested yet");
    expect(opencodeInstall).toContain("install the OpenCode client separately first");
    expect(codexInstall).toContain("install the Codex client separately");
    expect(geminiInstall).toContain("install the Gemini client separately");
    expect(codexInstall).toContain("does not show an HA NOVA update notice automatically yet");
    expect(geminiInstall).toContain("does not show an HA NOVA update notice automatically yet");
    expect(opencodeInstall).toContain("does not show an HA NOVA update notice automatically yet");
    expect(codexInstall).toContain("ha-nova check-update");
    expect(geminiInstall).toContain("ha-nova check-update");
    expect(opencodeInstall).toContain("ha-nova check-update");
    expect(codexInstall).toContain("ha-nova uninstall --purge");
    expect(geminiInstall).toContain("ha-nova uninstall --purge");
    expect(opencodeInstall).toContain("ha-nova uninstall --purge");
    expect(codexInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(codexInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(geminiInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(geminiInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(opencodeInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(opencodeInstall).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(codexInstall).not.toContain("winget");
    expect(geminiInstall).not.toContain("winget");
    expect(opencodeInstall).not.toContain("winget");
    expect(opencodeInstall).not.toContain("App installation, authentication, and skill setup");
  });

  it("classifies per-client install docs as active derived install overlays", () => {
    expect(governance).toContain("`.claude/INSTALL.md`, `.codex/INSTALL.md`, `.gemini/INSTALL.md`, `.opencode/INSTALL.md`");
    expect(governance).toContain("client-specific install overlays; derived active docs");
  });
});
