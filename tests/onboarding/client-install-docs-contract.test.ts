import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("client install docs contract", () => {
  const readme = readFileSync("README.md", "utf8");
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
  });

  it("documents Claude Windows prerequisites and migration only in the Claude install doc", () => {
    expect(claudeInstall).toContain("Windows");
    expect(claudeInstall).toContain("smoke-validated");
    expect(claudeInstall).toContain("Git for Windows");
    expect(claudeInstall).toContain("WSL");
    expect(claudeInstall).toContain("claude install");
    expect(claudeInstall).toContain("npm.cmd");
    expect(claudeInstall).toContain("macOS / Linux only");
    expect(claudeInstall).toContain("install-local-skills.sh claude");
    expect(claudeInstall).toContain("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1");
    expect(claudeInstall).toContain("Default behavior for real installs:");
    expect(claudeInstall).toContain("GitHub repo");
    expect(claudeInstall).toContain("Local validation / private-RC behavior:");
    expect(claudeInstall).toContain("https://github.com/markusleben/ha-nova");
    expect(claudeInstall).toContain("claude plugin update ha-nova@ha-nova");
    expect(claudeInstall).toContain("claude plugin install ha-nova@ha-nova");
    expect(claudeInstall).toContain("Claude Desktop in the **Code** tab uses this same Claude integration path.");
    expect(claudeInstall).toContain("Local repo checkout (macOS / Linux only)");
    expect(claudeInstall).not.toContain("use the repo root instead");
  });

  it("keeps other client install docs product-focused and platform-aware", () => {
    expect(codexInstall).toContain("Windows");
    expect(geminiInstall).toContain("Windows");
    expect(opencodeInstall).toContain("Windows");
    expect(opencodeInstall).toContain("WSL");
    expect(codexInstall).toContain("experimental until explicit Windows smoke completes");
    expect(geminiInstall).toContain("smoke-validated for this release");
    expect(opencodeInstall).toContain("experimental until explicit Windows smoke completes");
    expect(opencodeInstall).toContain("Install the OpenCode client separately");
    expect(codexInstall).toContain("install the Codex client separately");
    expect(geminiInstall).toContain("install the Gemini client separately");
    expect(codexInstall).toContain("No automatic startup update banner yet on Codex.");
    expect(geminiInstall).toContain("No automatic startup update banner yet on Gemini.");
    expect(opencodeInstall).toContain("No automatic startup update banner yet on OpenCode.");
    expect(codexInstall).toContain("ha-nova check-update");
    expect(geminiInstall).toContain("ha-nova check-update");
    expect(opencodeInstall).toContain("ha-nova check-update");
    expect(opencodeInstall).not.toContain("App installation, authentication, and skill setup");
  });
});
