import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("install.ps1 contract", () => {
  const content = readFileSync("install.ps1", "utf8");

  it("exists as the native Windows bootstrap entrypoint", () => {
    const stats = statSync("install.ps1");
    expect((stats.mode & constants.S_IRUSR) !== 0).toBe(true);
    expect(content).toContain("PowerShell");
    expect(content).toContain("Set-StrictMode -Version Latest");
  });

  it("supports plain UI mode and linear fallback output", () => {
    expect(content).toContain("HA_NOVA_PLAIN_UI");
    expect(content).toContain("NO_COLOR");
    expect(content).toContain("Test-PlainUi");
    expect(content).toContain("ForegroundColor Yellow");
    expect(content).toContain('Write-Output "  [!] $Message"');
    expect(content).toContain("[Console]::Error.WriteLine");
    expect(content).toContain("No interactive terminal detected; setup was not started automatically.");
  });

  it("uses GitHub Releases latest unless HA_NOVA_VERSION is pinned", () => {
    expect(content).toContain("https://api.github.com/repos/markusleben/ha-nova/releases/latest");
    expect(content).toContain("HA_NOVA_VERSION");
    expect(content).toContain("tag_name");
    expect(content).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/version.json");
  });

  it("supports maintainer-only bundle URL overrides for private RC tests", () => {
    expect(content).toContain("HA_NOVA_BUNDLE_URL");
    expect(content).toContain("HA_NOVA_BUNDLE_SHA256_URL");
    expect(content).toContain("Get-BundleUrl");
    expect(content).toContain("Get-BundleChecksumUrl");
    expect(content).toContain("Downloaded bundle version");
  });

  it("downloads the Windows bundle and validates bundle.json natively", () => {
    expect(content).toContain("ha-nova-windows-amd64.zip");
    expect(content).toContain("Windows amd64 bundle only");
    expect(content).toContain("x64 emulation");
    expect(content).toContain("Expand-Archive");
    expect(content).toContain("bundle.json");
    expect(content).toContain(".sha256");
    expect(content).toContain("Get-FileHash");
    expect(content).toContain("ha-nova.exe");
    expect(content).not.toContain("git clone");
    expect(content).not.toContain("npm install");
  });

  it("stays PowerShell-native without Git Bash or winget assumptions", () => {
    expect(content).not.toContain("winget");
    expect(content).not.toContain("Git.Git");
    expect(content).not.toContain("git-bash.exe");
    expect(content).not.toContain("bash.exe");
  });

  it("detects legacy installs and prints the dedicated cleanup one-liner", () => {
    expect(content).toContain("legacy-uninstall.ps1");
    expect(content).toContain("raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1");
    expect(content).toContain("onboarding.env");
    expect(content).toContain("version-check");
  });

  it("adds the install root to PATH and starts setup through the Go runtime", () => {
    expect(content).toContain("$PublicCommandDir = $InstallDir");
    expect(content).toContain("Ensure-InstallDirOnPath");
    expect(content).toContain("Write-State");
    expect(content).toContain("path_managed");
    expect(content).toContain("HA_NOVA_NO_SETUP");
    expect(content).toContain("Start-Setup");
    expect(content).toContain("& $BinaryPath setup");
    expect(content).not.toContain("ha-nova.cmd");
  });

  it("preserves managed PATH ownership on reinstall", () => {
    expect(content).toContain("$existing.path_managed -eq $true");
    expect(content).toContain('$existing.path_target -eq "user-path"');
    expect(content).toContain("$PathManaged = $true");
  });
});
