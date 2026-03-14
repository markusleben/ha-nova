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

  it("uses GitHub Releases latest unless HA_NOVA_VERSION is pinned", () => {
    expect(content).toContain("https://api.github.com/repos/markusleben/ha-nova/releases/latest");
    expect(content).toContain("HA_NOVA_VERSION");
    expect(content).toContain("tag_name");
    expect(content).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/version.json");
  });

  it("downloads the Windows bundle and validates bundle.json natively", () => {
    expect(content).toContain("ha-nova-windows-amd64.zip");
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

  it("installs a single public launcher and starts setup through the Go runtime", () => {
    expect(content).toContain("ha-nova.cmd");
    expect(content).toContain("Ensure-BinDirOnPath");
    expect(content).toContain("Write-State");
    expect(content).toContain("path_managed");
    expect(content).toContain("HA_NOVA_NO_SETUP");
    expect(content).toContain("Start-Setup");
    expect(content).toContain("& $BinaryPath setup");
    expect(content).not.toContain("Copy-Item -LiteralPath (Join-Path $InstallDir \"ha-nova.exe\") -Destination $PublicExePath -Force");
  });
});
