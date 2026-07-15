import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { constants, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";

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
    expect(content).toContain(
      "No interactive terminal detected; setup was not started automatically.",
    );
  });

  it("uses GitHub Releases latest unless HA_NOVA_VERSION is pinned", () => {
    expect(content).toContain(
      "https://api.github.com/repos/markusleben/ha-nova/releases/latest",
    );
    expect(content).toContain("HA_NOVA_VERSION");
    expect(content).toContain("tag_name");
    expect(content).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/version.json",
    );
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

  it("keeps progress suppression local to bundle downloads and restores the previous preference", () => {
    expect(content).toContain("function Invoke-DownloadFile");
    expect(content).toContain("$previousProgressPreference = $global:ProgressPreference");
    expect(content).toContain('$global:ProgressPreference = "SilentlyContinue"');
    expect(content).toContain("finally");
    expect(content).toContain(
      "$global:ProgressPreference = $previousProgressPreference",
    );
    expect(content).not.toContain(
      "$ProgressPreference = 'SilentlyContinue'",
    );
  });

  it("uses resilient native downloads for GitHub release assets", () => {
    expect(content).toContain("function Initialize-WebSecurity");
    expect(content).toContain("function Invoke-GitHubJson");
    expect(content).toContain("[Net.SecurityProtocolType]::Tls12");
    expect(content).toContain("function Get-DownloadHeaders");
    expect(content).toContain("function Invoke-DownloadFileWithMethod");
    expect(content).toContain("function Invoke-DownloadFileVerified");
    expect(content).toContain('"User-Agent" = "ha-nova-installer"');
    expect(content).toContain("UseBasicParsing");
    expect(content).toContain("MaximumRedirection = 10");
    expect(content).toContain("[System.Net.Http.HttpClient]");
    expect(content).toContain("DefaultProxyCredentials");
    expect(content).toContain("Start-BitsTransfer");
    expect(content).toContain("Could not download HA NOVA release asset");
    expect(content).toContain("Could not download and verify HA NOVA release asset");
    expect(content).toContain("Could not read the latest HA NOVA release from GitHub");
    expect(content).toContain("github.com release downloads");
    expect(content).toContain("$release = Invoke-GitHubJson -Uri $LatestReleaseUrl");
    expect(content).toContain("Initialize-WebSecurity\n\nif (-not (Test-CurrentInstall)");
    expect(content).toContain("Invoke-WindowsInstallerPreflight\n$expectedVersion");
    expect(content).toContain("HA_NOVA_INSTALLER_TEST_EXPORT");
  });

  it("checks Windows prerequisites before resolving or downloading a release", () => {
    expect(content).toContain("function Invoke-WindowsInstallerPreflight");
    expect(content).toContain("Windows 10 or Windows Server 2016 or later is required");
    expect(content).toContain("PowerShell 5.1 or later is required");
    expect(content).toContain("Get-PlatformArch");
    expect(content).toContain("function Assert-InstallRootWritable");
    expect(content).toContain("function Assert-GitHubTlsAccess");
    expect(content).toContain('$GitHubPreflightUrl = "https://github.com"');
    expect(content).toContain("HttpCompletionOption]::ResponseHeadersRead");

    const preflight = content.indexOf("Invoke-WindowsInstallerPreflight\n$expectedVersion");
    const versionResolution = content.indexOf("$version = Get-DownloadInstallVersion", preflight);
    const bundleInstall = content.indexOf("$bundleResult = Install-Bundle", preflight);
    expect(preflight).toBeGreaterThan(-1);
    expect(preflight).toBeLessThan(versionResolution);
    expect(versionResolution).toBeLessThan(bundleInstall);
  });

  it("retries a hash-mismatched primary download with the next transport", async () => {
    try {
      execFileSync("pwsh", ["-NoProfile", "-Command", "exit 0"], { stdio: "ignore" });
    } catch {
      return;
    }

    const payload = Buffer.from("verified fallback payload", "utf8");
    const expectedHash = createHash("sha256").update(payload).digest("hex");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-installer-test-"));
    const outFile = join(tempDir, "bundle.zip");
    const probe = join(tempDir, "probe.ps1");
    const installScript = join(process.cwd(), "install.ps1").replaceAll("'", "''");
    const escapedOutFile = outFile.replaceAll("'", "''");
    writeFileSync(
      probe,
      `
$ErrorActionPreference = "Stop"
$env:HA_NOVA_INSTALLER_TEST_EXPORT = "1"
. '${installScript}'
function Get-DownloadMethods {
  return @("Invoke-WebRequest", "HttpClient", "BITS")
}
function Invoke-DownloadFileWithMethod {
  param(
    [string]$Method,
    [string]$Uri,
    [string]$OutFile
  )
  $script:methods += $Method
  if ($Method -eq "Invoke-WebRequest") {
    Set-Content -LiteralPath $OutFile -Value "not the expected archive" -NoNewline
    return
  }
  if ($Method -eq "HttpClient") {
    Set-Content -LiteralPath $OutFile -Value "verified fallback payload" -NoNewline
    return
  }
  throw "BITS should not run after verified HttpClient success"
}
$script:methods = @()
Invoke-DownloadFileVerified -Uri "https://example.invalid/bundle.zip" -OutFile '${escapedOutFile}' -ExpectedSha256 "${expectedHash}"
$actual = [System.IO.File]::ReadAllText('${escapedOutFile}')
if (-not $actual.Contains("verified fallback payload")) {
  throw "fallback did not replace the mismatched primary download"
}
if (($script:methods -join ",") -ne "Invoke-WebRequest,HttpClient") {
  throw "unexpected download method sequence: $($script:methods -join ',')"
}
`,
      "utf8",
    );

    execFileSync(
      "pwsh",
      ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", probe],
      { stdio: "pipe" },
    );
  });

  it("stays PowerShell-native without winget or Git Bash assumptions", () => {
    expect(content).not.toContain("Get-Command winget");
    expect(content).not.toContain("Microsoft\\WinGet\\Links\\ha-nova.exe");
    expect(content).not.toContain("Microsoft\\WinGet\\Packages");
    expect(content).not.toContain("winget-managed HA NOVA install was detected");
    expect(content).not.toContain("winget upgrade --id");
    expect(content).not.toContain("winget uninstall --id");
    expect(content).not.toContain("Git.Git");
    expect(content).not.toContain("git-bash.exe");
    expect(content).not.toContain("bash.exe");
  });

  it("reads optional uninstall marker fields through the defensive accessor", () => {
    expect(content).toContain("function Get-UninstallStatusField");
    expect(content).toContain('$Status.PSObject.Properties[$Name]');
    expect(content).not.toContain("$Status.remaining_paths");
    expect(content).not.toContain("$status.error_summary");
    expect(content).not.toContain("$status.helper_pid");
  });

  it("blocks over an unfinished background uninstall instead of layering a new install", () => {
    expect(content).toContain("uninstall-status.json");
    expect(content).toContain(
      "A background HA NOVA uninstall is still running on Windows.",
    );
    expect(content).toContain(
      "A previous background HA NOVA uninstall did not finish cleanly.",
    );
    expect(content).toContain("ha-nova uninstall --yes");
    expect(content).toContain("ha-nova uninstall --yes --purge");
  });

  it("forces array shape on the remaining-paths accessor so .Count is StrictMode-safe", () => {
    // Regression: a fresh re-install over a leftover uninstall marker crashed
    // right after the banner with PropertyNotFoundStrict on `.Count`. PowerShell
    // unrolls a function's `return @(...)`: zero remaining paths collapse to
    // $null and a single path to a bare String, neither of which exposes .Count
    // under Set-StrictMode -Version Latest. The call site must re-wrap in @(...).
    expect(content).toContain(
      "$remainingPaths = @(Get-ExistingUninstallRemainingPaths -Status $status)",
    );
    expect(content).not.toMatch(
      /\$remainingPaths = Get-ExistingUninstallRemainingPaths/,
    );
  });

  it("makes post-install directory cleanup survive an antivirus file lock without aborting the install", () => {
    // Regression (live RC proof): a fresh install completed (runtime live in
    // InstallDir) but the installer still crashed with a Win32 "Access is denied"
    // on `Remove-Item $tempRoot`, because Defender briefly holds a handle on the
    // freshly extracted unsigned ha-nova.exe and `Remove-Item -Recurse` then throws
    // a TERMINATING Win32Exception that -ErrorAction SilentlyContinue does NOT
    // suppress under Set-StrictMode + $ErrorActionPreference="Stop" - AFTER install
    // yet BEFORE PATH setup / setup launch. The best-effort deletes must therefore
    // go through a try/catch helper, not a bare -ErrorAction.
    expect(content).toContain('$ErrorActionPreference = "Stop"');
    // The helper wraps Remove-Item in try/catch so the terminating exception is
    // swallowed - the -ErrorAction-only form was insufficient and crashed the live install.
    expect(content).toMatch(
      /function Remove-PathQuiet[\s\S]*?try\s*\{[\s\S]*?Remove-Item -LiteralPath \$Path -Recurse -Force -ErrorAction SilentlyContinue[\s\S]*?\}\s*\r?\n\s*catch\s*\{/,
    );
    // The disposable post-swap deletes route through the helper, not a bare Remove-Item.
    expect(content).toContain("Remove-PathQuiet -Path $tempRoot");
    expect(content).toContain("Remove-PathQuiet -Path $backupRoot");
    // No bare Remove-Item on these roots survives - the -ErrorAction-only form
    // crashed the live install, so it must be gone.
    expect(content).not.toMatch(/Remove-Item -LiteralPath \$tempRoot/);
    expect(content).not.toMatch(/Remove-Item -LiteralPath \$backupRoot/);
  });

  it("hardens install-time edge cases (legacy migration, running exe, empty checksum, PATH dedup, recovery null)", () => {
    // Legacy->current migration must not abort the installer on a locked legacy binary.
    expect(content).toContain("Could not migrate the previous Windows install");
    // The swap that stages the old install aside must catch a running/locked ha-nova.exe.
    expect(content).toContain("a ha-nova process may still be running");
    // An empty .sha256 download must Fail cleanly, not throw a null-method stacktrace.
    expect(content).toContain("$checksumRaw = Get-Content -LiteralPath $checksumPath -Raw");
    expect(content).toContain("if (-not $checksumRaw) {");
    // PATH membership compares NORMALIZED paths so a cased/trailing-slash variant
    // is not treated as missing (which would prepend a duplicate every update).
    expect(content).toContain(
      "$normalizedTarget = Normalize-RecoveryPath -Path $PublicCommandDir",
    );
    expect(content).not.toContain("if ($parts -contains $PublicCommandDir) {");
    // Recovery state must be null-checked explicitly, not via assignment truthiness
    // (same array-unrolling family as the fixed .Count crash).
    expect(content).toContain("$uninstallRecovery = Get-UninstallRecoveryState\n");
    expect(content).toContain("if ($null -ne $uninstallRecovery) {");
    expect(content).not.toContain("if ($uninstallRecovery = Get-UninstallRecoveryState) {");
  });

  it("detects legacy installs and prints the dedicated cleanup one-liner", () => {
    expect(content).toContain("legacy-uninstall.ps1");
    expect(content).toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1",
    );
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
    expect(content).toContain("Next step: ha-nova setup");
    expect(content).toContain(
      "Setup will ask for the six-digit pairing code shown in NOVA Home Base.",
    );
    expect(content).not.toContain("ha-nova.cmd");
  });

  it("keeps bootstrap logic out of product state persistence", () => {
    expect(content).not.toContain("Write-State");
    expect(content).not.toContain("state.json");
    expect(content).not.toContain("install_source");
    expect(content).not.toContain("path_managed");
  });
});
