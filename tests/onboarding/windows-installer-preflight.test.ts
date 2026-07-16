import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

function hasPowerShell(): boolean {
  return spawnSync("pwsh", ["-NoProfile", "-Command", "exit 0"]).status === 0;
}

function runPreflightProbe(body: string): { status: number | null; output: string } {
  const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-windows-preflight-"));
  const probe = join(tempDir, "probe.ps1");
  const installScript = join(process.cwd(), "install.ps1").replaceAll("'", "''");
  writeFileSync(
    probe,
    `$ErrorActionPreference = "Stop"
$env:HA_NOVA_INSTALLER_TEST_EXPORT = "1"
$env:HA_NOVA_PLAIN_UI = "1"
. '${installScript}'
${body}
`,
    "utf8",
  );
  const result = spawnSync(
    "pwsh",
    ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", probe],
    { encoding: "utf8" },
  );
  return {
    status: result.status,
    output: `${result.stdout ?? ""}${result.stderr ?? ""}`,
  };
}

function createNativeExitProbe(exitCode: number): string {
  const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-native-exit-"));
  if (process.platform === "win32") {
    const probe = join(tempDir, "probe.cmd");
    writeFileSync(probe, `@exit /b ${exitCode}\r\n`, "utf8");
    return probe;
  }
  const probe = join(tempDir, "probe.sh");
  writeFileSync(probe, `#!/bin/sh\nexit ${exitCode}\n`, "utf8");
  chmodSync(probe, 0o755);
  return probe;
}

const supportedRuntime = `
function Get-InstallerWindowsVersion { return [version]"10.0" }
function Get-InstallerPowerShellVersion { return [version]"5.1" }
$env:PROCESSOR_ARCHITECTURE = "AMD64"
`;

describe("Windows installer preflight behavior", () => {
  it("prints the exact pairing-aware continuation in a non-interactive session", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`
function Test-InteractiveSession { return $false }
Start-Setup -BinaryPath "never-run.exe"
`);
    expect(result.status).toBe(0);
    expect(result.output).toContain(
      "No interactive terminal detected; setup was not started automatically.",
    );
    expect(result.output).toContain("Next step: ha-nova setup");
    expect(result.output).toContain(
      "Setup will ask for the six-digit pairing code shown in NOVA Home Base.",
    );
  });

  it("prints missing-client guidance without launching setup", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`
function Test-InteractiveSession { return $true }
$script:calls = @()
function Invoke-FakeBinary {
  param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
  $script:calls += ($Arguments -join " ")
  if ($Arguments[0] -eq "internal-setup-readiness") {
    $global:LASTEXITCODE = 2
  }
}
Start-Setup -BinaryPath "Invoke-FakeBinary"
Write-Output ("CALLS:" + ($script:calls -join ","))
Write-Output ("LASTEXITCODE:" + $LASTEXITCODE)
`);
    expect(result.status).toBe(0);
    expect(result.output).toContain(
      "No supported AI client is ready on this machine yet.",
    );
    expect(result.output).toContain(
      "Install one supported client first, then rerun: ha-nova setup",
    );
    expect(result.output).toContain("CALLS:internal-setup-readiness");
    expect(result.output).not.toContain("CALLS:internal-setup-readiness,setup");
    expect(result.output).toContain("LASTEXITCODE:0");
  });

  it("handles the no-client native exit when PowerShell promotes native errors", () => {
    if (!hasPowerShell()) return;
    const fakeBinary = createNativeExitProbe(2).replaceAll("'", "''");
    const result = runPreflightProbe(`
function Test-InteractiveSession { return $true }
$PSNativeCommandUseErrorActionPreference = $true
Start-Setup -BinaryPath '${fakeBinary}'
Write-Output ("NATIVE_ERROR_PREFERENCE:" + $PSNativeCommandUseErrorActionPreference)
Write-Output ("LASTEXITCODE:" + $LASTEXITCODE)
`);
    expect(result.status).toBe(0);
    expect(result.output).toContain(
      "No supported AI client is ready on this machine yet.",
    );
    expect(result.output).toContain(
      "Install one supported client first, then rerun: ha-nova setup",
    );
    expect(result.output).toContain("NATIVE_ERROR_PREFERENCE:True");
    expect(result.output).toContain("LASTEXITCODE:0");
  });

  it("launches setup after a successful client-readiness check", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`
function Test-InteractiveSession { return $true }
$script:calls = @()
function Invoke-FakeBinary {
  param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
  $script:calls += ($Arguments -join " ")
  $global:LASTEXITCODE = 0
}
Start-Setup -BinaryPath "Invoke-FakeBinary"
Write-Output ("CALLS:" + ($script:calls -join ","))
`);
    expect(result.status).toBe(0);
    expect(result.output).toContain("CALLS:internal-setup-readiness,setup");
    expect(result.output).not.toContain(
      "No supported AI client is ready on this machine yet.",
    );
  });

  it("passes supported prerequisites in write-then-TLS order", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`${supportedRuntime}
$script:checks = @()
function Assert-InstallRootWritable { $script:checks += "write" }
function Assert-GitHubTlsAccess { $script:checks += "tls" }
Invoke-WindowsInstallerPreflight
Write-Output ($script:checks -join ",")
`);
    expect(result.status).toBe(0);
    expect(result.output).toContain("Windows prerequisites passed");
    expect(result.output).toContain("write,tls");
  });

  it("fails unsupported Windows before later preflight work", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`
function Get-InstallerWindowsVersion { return [version]"6.3" }
function Get-InstallerPowerShellVersion { throw "PowerShell check must not run" }
Invoke-WindowsInstallerPreflight
`);
    expect(result.status).not.toBe(0);
    expect(result.output).toContain(
      "Windows 10 or Windows Server 2016 or later is required",
    );
    expect(result.output).not.toContain("PowerShell check must not run");
  });

  it("fails PowerShell below 5.1 before architecture or network work", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`
function Get-InstallerWindowsVersion { return [version]"10.0" }
function Get-InstallerPowerShellVersion { return [version]"5.0" }
function Get-PlatformArch { throw "Architecture check must not run" }
Invoke-WindowsInstallerPreflight
`);
    expect(result.status).not.toBe(0);
    expect(result.output).toContain("PowerShell 5.1 or later is required");
    expect(result.output).not.toContain("Architecture check must not run");
  });

  it("fails a native ARM64 process with the x64-emulation recovery", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`${supportedRuntime}
$env:PROCESSOR_ARCHITECTURE = "ARM64"
Invoke-WindowsInstallerPreflight
`);
    expect(result.status).not.toBe(0);
    expect(result.output).toContain("Windows amd64 bundle only");
    expect(result.output).toContain("use x64 emulation");
  });

  it("fails a 32-bit process with a 64-bit PowerShell recovery", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`${supportedRuntime}
$env:PROCESSOR_ARCHITECTURE = "x86"
Invoke-WindowsInstallerPreflight
`);
    expect(result.status).not.toBe(0);
    expect(result.output).toContain("requires a 64-bit PowerShell process");
    expect(result.output).toContain("Open 64-bit PowerShell or Windows Terminal");
  });

  it("fails an unwritable per-user install root before the TLS probe", () => {
    if (!hasPowerShell()) return;
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-unwritable-root-"));
    const blocker = join(tempDir, "not-a-directory");
    writeFileSync(blocker, "blocked", "utf8");
    const escapedBlocker = blocker.replaceAll("'", "''");
    const result = runPreflightProbe(`${supportedRuntime}
$script:InstallDir = Join-Path '${escapedBlocker}' "ha-nova"
function Assert-GitHubTlsAccess { throw "TLS probe must not run" }
Invoke-WindowsInstallerPreflight
`);
    expect(result.status).not.toBe(0);
    expect(result.output).toContain("Cannot write to the per-user HA NOVA install location");
    expect(result.output).toContain("Fix LOCALAPPDATA permissions");
    expect(result.output).not.toContain("TLS probe must not run");
  });

  it("surfaces actionable GitHub TLS recovery before downloads", () => {
    if (!hasPowerShell()) return;
    const result = runPreflightProbe(`${supportedRuntime}
function Assert-InstallRootWritable { }
function Assert-GitHubTlsAccess {
  Fail "Could not establish a TLS connection to GitHub. Check the Windows date/time, proxy or firewall, and pending Windows updates, then rerun the installer."
}
Invoke-WindowsInstallerPreflight
`);
    expect(result.status).not.toBe(0);
    expect(result.output).toContain("Could not establish a TLS connection to GitHub");
    expect(result.output).toContain("proxy or firewall");
    expect(result.output).toContain("pending Windows updates");
  });
});
