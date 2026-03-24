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
    expect(content).toContain("ha-nova-installer-bundle-windows-amd64.zip");
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

  it("stays PowerShell-native without Git Bash assumptions", () => {
    expect(content).toContain("Get-Command winget");
    expect(content).toContain("Microsoft\\WinGet\\Links\\ha-nova.exe");
    expect(content).toContain("Microsoft\\WinGet\\Packages");
    expect(content).toContain("winget-managed HA NOVA install was detected");
    expect(content).toContain("winget upgrade --id");
    expect(content).toContain("winget uninstall --id");
    expect(content).toContain('return "unknown"');
    expect(content).toContain('& $wingetCommand.Source list ha-nova');
    expect(content).not.toContain("Git.Git");
    expect(content).not.toContain("git-bash.exe");
    expect(content).not.toContain("bash.exe");
  });

  it("blocks over an unfinished background uninstall instead of layering a new install", () => {
    expect(content).toContain("uninstall-status.json");
    expect(content).toContain("A background HA NOVA uninstall is still running on Windows.");
    expect(content).toContain("A previous background HA NOVA uninstall did not finish cleanly.");
    expect(content).toContain("ha-nova uninstall --yes");
    expect(content).toContain("ha-nova uninstall --yes --purge");
  });

  it("detects legacy installs and prints the dedicated cleanup one-liner", () => {
    expect(content).toContain("legacy-uninstall.ps1");
    expect(content).toContain("raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1");
    expect(content).toContain("onboarding.env");
    expect(content).toContain("check-update.cmd");
    expect(content).not.toContain('(Join-Path $LegacyConfigDir "relay")');
    expect(content).not.toContain('(Join-Path $LegacyConfigDir "relay.exe")');
    expect(content).not.toContain('(Join-Path $LegacyConfigDir "version-check")');
  });

  it("uses native Windows app locations and starts setup through the Go runtime", () => {
    expect(content).toContain("LOCALAPPDATA");
    expect(content).toContain("APPDATA");
    expect(content).toContain("Programs\\ha-nova");
    expect(content).toContain("$PublicCommandDir = $InstallDir");
    expect(content).toContain("Ensure-InstallDirOnPath");
    expect(content).toContain("HA_NOVA_NO_SETUP");
    expect(content).toContain("Start-Setup");
    expect(content).toContain("& $BinaryPath setup");
    expect(content).not.toContain("ha-nova.cmd");
  });

  it("keeps bootstrap logic out of product state persistence", () => {
    expect(content).not.toContain("Write-State");
    expect(content).not.toContain("state.json");
    expect(content).not.toContain("install_source");
    expect(content).not.toContain("path_managed");
  });
});
