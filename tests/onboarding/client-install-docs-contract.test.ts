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
  });

  it("documents Claude Windows prerequisites and migration only in the Claude install doc", () => {
    expect(claudeInstall).toContain("Windows");
    expect(claudeInstall).toContain("validated on Windows for this release");
    expect(claudeInstall).toContain("Current public Windows install path: `install.ps1`.");
    expect(claudeInstall).toContain("A `winget` manifest is generated for each release");
    expect(claudeInstall).toContain("published and proven on a fresh Windows VM");
    expect(claudeInstall).toContain("Git for Windows");
    expect(claudeInstall).toContain("WSL");
    expect(claudeInstall).toContain("claude install");
    expect(claudeInstall).toContain("npm.cmd");
    expect(claudeInstall).toContain("macOS / Linux only");
    expect(claudeInstall).toContain("install-local-skills.sh claude");
    expect(claudeInstall).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(claudeInstall).toContain("Default behavior for real installs:");
    expect(claudeInstall).toContain("installed HA NOVA release payload on disk");
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
    expect(claudeInstall).not.toContain("A `winget` package is planned");
    expect(claudeInstall).not.toContain("use the repo root instead");
  });

  it("keeps other client install docs product-focused and platform-aware", () => {
    expect(codexInstall).toContain("Windows");
    expect(geminiInstall).toContain("Windows");
    expect(opencodeInstall).toContain("Windows");
    expect(codexInstall).toContain("Current public Windows install path: `install.ps1`.");
    expect(geminiInstall).toContain("Current public Windows install path: `install.ps1`.");
    expect(opencodeInstall).toContain("Current public Windows install path: `install.ps1`.");
    expect(codexInstall).toContain("A `winget` manifest is generated for each release");
    expect(geminiInstall).toContain("A `winget` manifest is generated for each release");
    expect(opencodeInstall).toContain("A `winget` manifest is generated for each release");
    expect(codexInstall).toContain("published and proven on a fresh Windows VM");
    expect(geminiInstall).toContain("published and proven on a fresh Windows VM");
    expect(opencodeInstall).toContain("published and proven on a fresh Windows VM");
    expect(opencodeInstall).toContain("WSL");
    expect(codexInstall).toContain("experimental until explicit Windows smoke completes");
    expect(geminiInstall).toContain("smoke-validated for this release");
    expect(opencodeInstall).toContain("experimental until explicit Windows smoke completes");
    expect(opencodeInstall).toContain("install the OpenCode client separately first");
    expect(codexInstall).toContain("install the Codex client separately");
    expect(geminiInstall).toContain("install the Gemini client separately");
    expect(codexInstall).toContain("No automatic startup update banner yet on Codex.");
    expect(geminiInstall).toContain("No automatic startup update banner on Gemini yet.");
    expect(opencodeInstall).toContain("No automatic startup update banner yet on OpenCode.");
    expect(codexInstall).toContain("ha-nova check-update");
    expect(geminiInstall).toContain("ha-nova check-update");
    expect(opencodeInstall).toContain("ha-nova check-update");
    expect(codexInstall).toContain("ha-nova uninstall --purge");
    expect(geminiInstall).toContain("ha-nova uninstall --purge");
    expect(opencodeInstall).toContain("ha-nova uninstall --purge");
    expect(codexInstall).not.toContain("A `winget` package is planned");
    expect(geminiInstall).not.toContain("A `winget` package is planned");
    expect(opencodeInstall).not.toContain("A `winget` package is planned");
    expect(opencodeInstall).not.toContain("App installation, authentication, and skill setup");
  });

  it("classifies per-client install docs as active derived install overlays", () => {
    expect(governance).toContain("`.claude/INSTALL.md`, `.codex/INSTALL.md`, `.gemini/INSTALL.md`, `.opencode/INSTALL.md`");
    expect(governance).toContain("client-specific install overlays; derived active docs");
  });
});
