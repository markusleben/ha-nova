# HA NOVA PowerShell legacy cleanup for pre-Go installs.
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
$InstallDir = Join-Path $HOME ".local\share\ha-nova"
$CurrentInstallDir = Join-Path $LocalAppDataDir "Programs\ha-nova"
$WingetPortableLink = Join-Path $LocalAppDataDir "Microsoft\WinGet\Links\ha-nova.exe"
$ConfigDir = Join-Path $HOME ".config\ha-nova"

function Fail([string]$Message) {
  throw "[ha-nova:legacy-uninstall] ERROR: $Message"
}

function Remove-IfExists([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) {
    return
  }
  # Best-effort: a locked legacy file (running relay.exe, AV handle) must not abort
  # the whole cleanup under Stop mode and trap the user in a loop where the
  # installer still detects legacy residue. `Remove-Item -Recurse` throws a
  # TERMINATING Win32Exception ("Access is denied") that -ErrorAction SilentlyContinue
  # does NOT suppress, so wrap it in try/catch. The blocker-residue check below still
  # fails loudly if a real blocker survives, so this never hides a stuck cleanup.
  try {
    Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction SilentlyContinue
  }
  catch {
  }
}

if (
  (Test-Path -LiteralPath (Join-Path $InstallDir "bundle.json")) -or
  (Test-Path -LiteralPath (Join-Path $CurrentInstallDir "bundle.json")) -or
  (Test-Path -LiteralPath (Join-Path $CurrentInstallDir "ha-nova.exe")) -or
  (Test-Path -LiteralPath $WingetPortableLink)
) {
  Fail "A current Go install was detected. Use: ha-nova uninstall"
}

$legacyPaths = @(
  (Join-Path $ConfigDir "onboarding.env"),
  (Join-Path $ConfigDir "relay"),
  (Join-Path $ConfigDir "relay.exe"),
  (Join-Path $ConfigDir "update"),
  (Join-Path $ConfigDir "update.cmd"),
  (Join-Path $ConfigDir "version-check"),
  (Join-Path $ConfigDir "check-update.cmd")
)

foreach ($path in $legacyPaths) {
  Remove-IfExists $path
}

$wsl = Get-Command wsl.exe -ErrorAction SilentlyContinue
if ($null -ne $wsl) {
  try {
    & $wsl.Source sh -lc 'rm -rf ~/.hermes/skills/ha-nova' 2>$null | Out-Null
  }
  catch {
  }
}

$legacyScriptsDir = Join-Path $InstallDir "scripts\onboarding"
if ((Test-Path -LiteralPath $legacyScriptsDir) -and -not (Test-Path -LiteralPath (Join-Path $InstallDir "bundle.json"))) {
  Remove-IfExists $InstallDir
}

$skillRoots = @(
  (Join-Path $HOME ".agents\skills"),
  (Join-Path $HOME ".config\opencode\skills"),
  (Join-Path $HOME ".gemini\skills"),
  (Join-Path $HOME ".hermes\skills"),
  (Join-Path $HOME ".claude\skills")
)

foreach ($root in $skillRoots) {
  if (-not (Test-Path -LiteralPath $root)) {
    continue
  }
  Get-ChildItem -LiteralPath $root -Filter "ha-nova*" -ErrorAction SilentlyContinue | ForEach-Object {
    Remove-IfExists $_.FullName
  }
}

# The deletes above are best-effort so a locked non-blocker file can't abort the
# whole cleanup. But install.ps1's Test-LegacyInstall keeps aborting while any of
# these blocker files remain, so verify they are actually gone and fail loudly
# with the residue list rather than reporting a false success.
$blockerPaths = @(
  (Join-Path $ConfigDir "onboarding.env"),
  (Join-Path $ConfigDir "update"),
  (Join-Path $ConfigDir "update.cmd"),
  (Join-Path $ConfigDir "check-update.cmd"),
  (Join-Path $InstallDir "scripts\onboarding")
)
$blockerResidue = @($blockerPaths | Where-Object { Test-Path -LiteralPath $_ })
if ($blockerResidue.Count -gt 0) {
  Fail "Could not remove some legacy files (one may be locked or in use):`n  $($blockerResidue -join "`n  ")`nClose any running ha-nova or relay process, then run this cleanup again."
}

Write-Host "[ha-nova:legacy-uninstall] Legacy HA NOVA cleanup finished."
