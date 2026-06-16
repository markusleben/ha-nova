# HA NOVA Windows bootstrap installer for PowerShell.
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoOwner = "markusleben"
$RepoName = "ha-nova"
$LatestReleaseUrl = "https://api.github.com/repos/markusleben/ha-nova/releases/latest"
$ReleaseBaseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download"
$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
$AppDataDir = if ($env:APPDATA) { $env:APPDATA } else { Join-Path $HOME "AppData\Roaming" }
$InstallDir = Join-Path $LocalAppDataDir "Programs\ha-nova"
$PublicCommandDir = $InstallDir
$LegacyUninstallUrl = "https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1"
$ConfigDir = Join-Path $AppDataDir "ha-nova"
$UninstallStatusPath = Join-Path $LocalAppDataDir "ha-nova\uninstall-status.json"
$LegacyConfigDir = Join-Path $HOME ".config\ha-nova"
$LegacyInstallDir = Join-Path $HOME ".local\share\ha-nova"
$UsePlainUi = $false

function Test-PlainUi {
  if ($env:HA_NOVA_PLAIN_UI -eq "1") {
    return $true
  }
  if ($env:NO_COLOR) {
    return $true
  }
  if ($env:TERM -eq "dumb") {
    return $true
  }
  try {
    return [Console]::IsOutputRedirected
  }
  catch {
    return $true
  }
}

function Write-Banner {
  if ($UsePlainUi) {
    Write-Output ""
    Write-Output "  HA NOVA Windows Installer"
    Write-Output "  -------------------------"
    Write-Output ""
    return
  }

  Write-Host ""
  Write-Host "  HA NOVA Windows Installer" -ForegroundColor Yellow
  Write-Host "  -------------------------" -ForegroundColor Yellow
  Write-Host ""
}

function Write-Info([string]$Message) {
  if ($UsePlainUi) {
    Write-Output "  [ok] $Message"
    return
  }
  Write-Host "  [ok] $Message" -ForegroundColor Green
}

function Write-Warn([string]$Message) {
  if ($UsePlainUi) {
    Write-Output "  [!] $Message"
    return
  }
  Write-Host "  [!] $Message" -ForegroundColor DarkYellow
}

function Write-Note([string]$Message) {
  if ($UsePlainUi) {
    Write-Output "  $Message"
    return
  }
  Write-Host "  $Message" -ForegroundColor DarkGray
}

function Fail([string]$Message) {
  if ($UsePlainUi) {
    [Console]::Error.WriteLine("  [!!] $Message")
  }
  else {
    Write-Host "  [!!] $Message" -ForegroundColor Red
  }
  throw $Message
}

function Test-InteractiveSession {
  try {
    return -not [Console]::IsInputRedirected
  }
  catch {
    return $false
  }
}

function Get-PlatformArch {
  $arch = $env:PROCESSOR_ARCHITECTURE
  if ($arch -eq "AMD64") {
    return "amd64"
  }

  if ($arch -eq "ARM64") {
    Fail "HA NOVA currently ships a Windows amd64 bundle only. On Windows ARM64, use x64 emulation."
  }

  Fail "Unsupported Windows architecture '$arch'. HA NOVA currently ships a Windows amd64 bundle only."
}

function Get-ExpectedInstallVersion {
  if ($env:HA_NOVA_VERSION) {
    return $env:HA_NOVA_VERSION.TrimStart("v")
  }

  return $null
}

function Get-LatestInstallVersion {
  $release = Invoke-RestMethod -Uri $LatestReleaseUrl -Headers @{
    Accept = "application/vnd.github+json"
    "User-Agent" = "ha-nova-installer"
  }
  if (-not $release.tag_name) {
    Fail "Could not determine the latest HA NOVA release."
  }

  return ([string]$release.tag_name).TrimStart("v")
}

function Get-DownloadInstallVersion {
  $expected = Get-ExpectedInstallVersion
  if ($expected) {
    return $expected
  }

  if ($env:HA_NOVA_BUNDLE_URL) {
    return $null
  }

  return Get-LatestInstallVersion
}

function Get-BundleUrl {
  param(
    [string]$Version
  )

  if ($env:HA_NOVA_BUNDLE_URL) {
    return $env:HA_NOVA_BUNDLE_URL
  }

  return "$ReleaseBaseUrl/v$Version/ha-nova-installer-bundle-windows-amd64.zip"
}

function Get-BundleChecksumUrl {
  param(
    [string]$Version
  )

  if ($env:HA_NOVA_BUNDLE_SHA256_URL) {
    return $env:HA_NOVA_BUNDLE_SHA256_URL
  }

  return "$(Get-BundleUrl -Version $Version).sha256"
}

function Test-CurrentInstall {
  return Test-Path -LiteralPath (Join-Path $InstallDir "bundle.json")
}

function Test-LegacyInstall {
  $legacyPaths = @(
    (Join-Path $LegacyConfigDir "onboarding.env"),
    (Join-Path $LegacyConfigDir "update"),
    (Join-Path $LegacyConfigDir "update.cmd"),
    (Join-Path $LegacyConfigDir "check-update.cmd")
  )

  foreach ($path in $legacyPaths) {
    if (Test-Path -LiteralPath $path) {
      return $true
    }
  }

  $legacyScriptsDir = Join-Path $LegacyInstallDir "scripts\onboarding"
  return (Test-Path -LiteralPath $legacyScriptsDir) -and -not (Test-CurrentInstall)
}

function Stop-ForLegacyInstall {
  Fail @"
A pre-Go HA NOVA install was detected.
This installer does not migrate legacy installs in place.

Run the cleanup first:
  irm $LegacyUninstallUrl | iex

Then run this installer again.
"@
}

function Get-UninstallRecoveryCommand {
  param(
    [string]$Mode
  )

  if ($Mode -eq "purge") {
    return "ha-nova uninstall --yes --purge"
  }

  return "ha-nova uninstall --yes"
}

function Normalize-RecoveryPath {
  param(
    [string]$Path
  )

  if (-not $Path) {
    return ""
  }

  try {
    return [System.IO.Path]::GetFullPath($Path).TrimEnd('\').ToLowerInvariant()
  }
  catch {
    return $Path.TrimEnd('\').ToLowerInvariant()
  }
}

function Test-UserPathContainsEntry {
  param(
    [string]$TargetPath
  )

  $normalizedTarget = Normalize-RecoveryPath -Path $TargetPath
  if (-not $normalizedTarget) {
    return $false
  }

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (-not $userPath) {
    return $false
  }

  foreach ($entry in ($userPath -split ";")) {
    if ((Normalize-RecoveryPath -Path $entry) -eq $normalizedTarget) {
      return $true
    }
  }

  return $false
}

function Get-UninstallStatusField {
  param(
    $Status,
    [string]$Name
  )

  # The background uninstall helper writes marker fields incrementally; a
  # killed helper can leave a marker without optional fields. Under
  # Set-StrictMode, direct access to a missing property throws, so all
  # optional marker reads must go through this accessor.
  if ($null -eq $Status) {
    return $null
  }
  $property = $Status.PSObject.Properties[$Name]
  if ($null -eq $property) {
    return $null
  }
  return $property.Value
}

function Get-ExistingUninstallRemainingPaths {
  param(
    $Status
  )

  $remaining = @()
  foreach ($candidate in @(Get-UninstallStatusField -Status $Status -Name "remaining_paths")) {
    $candidatePath = [string]$candidate
    if ($candidatePath -and (Test-Path -LiteralPath $candidatePath)) {
      $remaining += $candidatePath
    }
  }

  return @($remaining | Select-Object -Unique)
}

function Get-UninstallRecoveryState {
  if (-not (Test-Path -LiteralPath $UninstallStatusPath)) {
    return $null
  }

  $status = $null
  try {
    $status = Get-Content -LiteralPath $UninstallStatusPath -Raw | ConvertFrom-Json
  }
  catch {
    $bundleRuntimePresent = Test-Path -LiteralPath (Join-Path $InstallDir "ha-nova.exe")
    $pathResidue = Test-UserPathContainsEntry -TargetPath $InstallDir
    if (-not $bundleRuntimePresent -and -not $pathResidue) {
      Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
      return $null
    }
    return [pscustomobject]@{
      Kind = "failed"
      Summary = "A previous background HA NOVA uninstall left an unreadable recovery marker."
      RecoveryCommand = (Get-UninstallRecoveryCommand -Mode "standard")
      RemainingPaths = @()
      RuntimePresent = $bundleRuntimePresent
      PathResidue = $pathResidue
    }
  }

  if ((Get-UninstallStatusField -Status $status -Name "status") -eq "success") {
    Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
    return $null
  }

  $mode = if ((Get-UninstallStatusField -Status $status -Name "mode") -eq "purge") { "purge" } else { "standard" }
  $recoveryCommand = Get-UninstallRecoveryCommand -Mode $mode
  $bundleRuntimePresent = Test-Path -LiteralPath (Join-Path $InstallDir "ha-nova.exe")
  $runtimePresent = $bundleRuntimePresent
  # PowerShell unrolls a function's `return @(...)` on the way out: zero items
  # collapse to $null and a single item to a bare scalar. Under
  # Set-StrictMode -Version Latest, `.Count` on either throws
  # PropertyNotFoundStrict, which aborted a fresh re-install over a leftover
  # uninstall marker right after the banner. Re-wrap to guarantee array shape.
  $remainingPaths = @(Get-ExistingUninstallRemainingPaths -Status $status)
  $pathResidue = Test-UserPathContainsEntry -TargetPath $InstallDir

  if ((Get-UninstallStatusField -Status $status -Name "status") -eq "running") {
    $updatedAt = $null
    if (Get-UninstallStatusField -Status $status -Name "last_updated_at") {
      try {
        $updatedAt = [DateTimeOffset]::Parse([string](Get-UninstallStatusField -Status $status -Name "last_updated_at"))
      }
      catch {
        $updatedAt = $null
      }
    }
    if (-not $updatedAt -and (Get-UninstallStatusField -Status $status -Name "started_at")) {
      try {
        $updatedAt = [DateTimeOffset]::Parse([string](Get-UninstallStatusField -Status $status -Name "started_at"))
      }
      catch {
        $updatedAt = $null
      }
    }

    $helperAlive = $false
    if (Get-UninstallStatusField -Status $status -Name "helper_pid") {
      try {
        $helperAlive = $null -ne (Get-Process -Id ([int](Get-UninstallStatusField -Status $status -Name "helper_pid")) -ErrorAction SilentlyContinue)
      }
      catch {
        $helperAlive = $false
      }
    }

    if ($helperAlive -and $updatedAt -and ([DateTimeOffset]::UtcNow - $updatedAt.ToUniversalTime()).TotalMinutes -lt 10) {
      return [pscustomobject]@{
        Kind = "running"
        Summary = "A background HA NOVA uninstall is still running on Windows."
        RecoveryCommand = $recoveryCommand
        RemainingPaths = $remainingPaths
        RuntimePresent = $runtimePresent
        PathResidue = $pathResidue
      }
    }

    if (-not $runtimePresent -and -not $pathResidue -and $remainingPaths.Count -eq 0) {
      Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
      return $null
    }

    return [pscustomobject]@{
      Kind = "failed"
      Summary = "A previous background HA NOVA uninstall was interrupted before it finished."
      RecoveryCommand = $recoveryCommand
      RemainingPaths = $remainingPaths
      RuntimePresent = $runtimePresent
      PathResidue = $pathResidue
    }
  }

  if ((Get-UninstallStatusField -Status $status -Name "status") -eq "failed") {
    if (-not $runtimePresent -and -not $pathResidue -and $remainingPaths.Count -eq 0) {
      Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
      return $null
    }

    $summary = if (Get-UninstallStatusField -Status $status -Name "error_summary") { [string](Get-UninstallStatusField -Status $status -Name "error_summary") } else { "A previous background HA NOVA uninstall did not finish cleanly." }
    return [pscustomobject]@{
      Kind = "failed"
      Summary = $summary
      RecoveryCommand = $recoveryCommand
      RemainingPaths = $remainingPaths
      RuntimePresent = $runtimePresent
      PathResidue = $pathResidue
    }
  }

  return [pscustomobject]@{
    Kind = "failed"
    Summary = "A previous background HA NOVA uninstall left an unreadable recovery marker."
    RecoveryCommand = $recoveryCommand
    RemainingPaths = $remainingPaths
    RuntimePresent = $runtimePresent
    PathResidue = $pathResidue
  }
}

function Stop-ForUninstallRecovery {
  param(
    [Parameter(Mandatory = $true)]$Recovery
  )

  if ($Recovery.Kind -eq "running") {
    Fail @"
A background HA NOVA uninstall is still running on Windows.
Wait for it to finish, then run:
  ha-nova doctor

If cleanup did not complete, repair it with:
  $($Recovery.RecoveryCommand)
"@
  }

  if (-not $Recovery.RuntimePresent) {
    $instructions = @()
    if ($Recovery.PathResidue) {
      $instructions += "Remove the stale Windows user PATH entry for $InstallDir."
    }
    foreach ($path in @($Recovery.RemainingPaths)) {
      $instructions += "Remove: $path"
    }
    if ($instructions.Count -eq 0) {
      $instructions += "Clean up the stale HA NOVA uninstall marker at $UninstallStatusPath."
    }

    Fail @"
$($Recovery.Summary)

The HA NOVA runtime is no longer available, so the normal recovery command cannot run.

Manual cleanup required:
  $($instructions -join "`n  ")

Then run this installer again.
"@
  }

  Fail @"
$($Recovery.Summary)

Run the cleanup first:
  $($Recovery.RecoveryCommand)

Then run this installer again.
"@
}

function Invoke-DownloadFile {
  param(
    [Parameter(Mandatory = $true)][string]$Uri,
    [Parameter(Mandatory = $true)][string]$OutFile
  )

  $previousProgressPreference = $global:ProgressPreference
  try {
    $global:ProgressPreference = "SilentlyContinue"
    Invoke-WebRequest -Uri $Uri -OutFile $OutFile
  }
  finally {
    $global:ProgressPreference = $previousProgressPreference
  }
}

function Install-Bundle {
  param(
    [string]$Version
  )

  $null = Get-PlatformArch
  $bundleName = "ha-nova-installer-bundle-windows-amd64.zip"
  $bundleUrl = Get-BundleUrl -Version $Version
  $tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("ha-nova-install-" + [guid]::NewGuid().ToString("N"))
  $archivePath = Join-Path $tempRoot $bundleName
  $checksumPath = "$archivePath.sha256"
  $extractDir = Join-Path $tempRoot "extract"

  New-Item -ItemType Directory -Force -Path $tempRoot | Out-Null
  New-Item -ItemType Directory -Force -Path $extractDir | Out-Null

  try {
    Invoke-DownloadFile -Uri $bundleUrl -OutFile $archivePath
    Invoke-DownloadFile -Uri (Get-BundleChecksumUrl -Version $Version) -OutFile $checksumPath
    $checksumRaw = Get-Content -LiteralPath $checksumPath -Raw
    if (-not $checksumRaw) {
      Fail "Downloaded bundle checksum verification failed."
    }
    $expectedHash = $checksumRaw.Trim().Split()[0]
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if (-not $expectedHash -or $actualHash -ne $expectedHash.ToLowerInvariant()) {
      Fail "Downloaded bundle checksum verification failed."
    }
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir -Force

    $bundleRoot = Join-Path $extractDir "ha-nova"
    if (-not (Test-Path -LiteralPath $bundleRoot)) {
      Fail "Downloaded bundle did not contain an installable ha-nova directory."
    }

    $bundleMeta = Join-Path $bundleRoot "bundle.json"
    $bundleBinary = Join-Path $bundleRoot "ha-nova.exe"
    if (-not (Test-Path -LiteralPath $bundleMeta)) {
      Fail "Downloaded bundle is missing bundle.json."
    }
    if (-not (Test-Path -LiteralPath $bundleBinary)) {
      Fail "Downloaded bundle is missing ha-nova.exe."
    }
    $bundleInfo = Get-Content -LiteralPath $bundleMeta -Raw | ConvertFrom-Json
    if ($bundleInfo.os -ne "windows") {
      Fail "Downloaded bundle OS metadata does not match this machine."
    }
    if ($bundleInfo.arch -ne "amd64") {
      Fail "Downloaded bundle architecture metadata does not match this machine."
    }
    if ($bundleInfo.binary_name -ne "ha-nova.exe") {
      Fail "Downloaded bundle binary metadata does not match the expected runtime."
    }
    if (-not $bundleInfo.version) {
      Fail "Downloaded bundle is missing version metadata."
    }
    $bundleVersion = ([string]$bundleInfo.version).TrimStart("v")
    if ($Version -and $bundleVersion -ne $Version) {
      Fail "Downloaded bundle version v$bundleVersion does not match requested version v$Version."
    }

    if ((Test-Path -LiteralPath (Join-Path $LegacyInstallDir "bundle.json")) -and -not (Test-Path -LiteralPath $InstallDir)) {
      New-Item -ItemType Directory -Force -Path (Split-Path -Parent $InstallDir) | Out-Null
      try {
        Move-Item -LiteralPath $LegacyInstallDir -Destination $InstallDir -Force
      }
      catch {
        Fail "Could not migrate the previous Windows install at $LegacyInstallDir (a file may be in use). Close ha-nova and retry."
      }
      Write-Info "Migrated previous Windows install to $InstallDir"
    }

    $installParent = Split-Path -Parent $InstallDir
    $nextRoot = Join-Path $installParent (".ha-nova-next-" + [guid]::NewGuid().ToString("N"))
    $backupRoot = Join-Path $installParent (".ha-nova-old-" + [guid]::NewGuid().ToString("N"))

    New-Item -ItemType Directory -Force -Path $installParent | Out-Null
    Copy-Item -Path $bundleRoot -Destination $nextRoot -Recurse -Force

    if (Test-Path -LiteralPath $InstallDir) {
      try {
        Move-Item -LiteralPath $InstallDir -Destination $backupRoot -Force
      }
      catch {
        Remove-Item -LiteralPath $nextRoot -Recurse -Force -ErrorAction SilentlyContinue
        Fail "Could not replace the existing HA NOVA install - a ha-nova process may still be running. Close it (and any running relay), then run the installer again."
      }
    }

    try {
      Move-Item -LiteralPath $nextRoot -Destination $InstallDir -Force
      if (Test-Path -LiteralPath $backupRoot) {
        # The new runtime is already live in $InstallDir; deleting the old copy
        # is best-effort. A locked file here (same antivirus race as below) must
        # not throw into the catch, which would roll back the successful swap.
        Remove-Item -LiteralPath $backupRoot -Recurse -Force -ErrorAction SilentlyContinue
      }
      return [ordered]@{ Version = $bundleVersion }
    }
    catch {
      if (Test-Path -LiteralPath $backupRoot) {
        Move-Item -LiteralPath $backupRoot -Destination $InstallDir -Force
      }
      throw
    }
  }
  finally {
    if (Test-Path -LiteralPath $tempRoot) {
      # Best-effort cleanup: antivirus (Defender real-time) can briefly hold an
      # exclusive handle on the freshly extracted, unsigned ha-nova.exe. Under
      # $ErrorActionPreference = "Stop" a throw here would abort the installer
      # AFTER a successful install but BEFORE PATH setup and setup launch,
      # leaving HA NOVA installed yet unreachable. The temp dir lives under
      # %TEMP% and is reaped by Windows regardless.
      Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
  }
}

function Ensure-InstallDirOnPath {
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $normalizedTarget = Normalize-RecoveryPath -Path $PublicCommandDir
  $parts = @()
  if ($userPath) {
    $parts = @($userPath -split ";" | Where-Object { $_ -and $_ -ne $LegacyInstallDir })
  }

  # Compare normalized (full-path, case-insensitive, no trailing slash) so a
  # differently-cased or trailing-backslash variant is not treated as missing,
  # which would prepend a duplicate PATH entry on every re-install/update.
  foreach ($entry in $parts) {
    if ((Normalize-RecoveryPath -Path $entry) -eq $normalizedTarget) {
      Write-Info "$PublicCommandDir already configured in PATH"
      return $false
    }
  }

  $newPath = @($PublicCommandDir) + $parts
  [Environment]::SetEnvironmentVariable("Path", ($newPath -join ";"), "User")
  $env:Path = "$PublicCommandDir;$env:Path"
  Write-Info "Added $PublicCommandDir to PATH"
  return $true
}

function Cleanup-LegacyBundleInstall {
  if ($LegacyInstallDir -eq $InstallDir) {
    return
  }
  if ((Test-Path -LiteralPath $LegacyInstallDir) -and (Test-Path -LiteralPath (Join-Path $LegacyInstallDir "bundle.json"))) {
    Remove-Item -LiteralPath $LegacyInstallDir -Recurse -Force
    Write-Info "Removed legacy Windows install root $LegacyInstallDir"
  }
}

function Start-Setup {
  param(
    [Parameter(Mandatory = $true)][string]$BinaryPath
  )

  if ($env:HA_NOVA_NO_SETUP -eq "1") {
    Write-Info "Installed HA NOVA without setup."
    Write-Note "Next step: ha-nova setup"
    Write-Note "Need help later? Run: ha-nova doctor"
    return
  }

  if (-not (Test-InteractiveSession)) {
    Write-Warn "No interactive terminal detected; setup was not started automatically."
    Write-Note "Next step: ha-nova setup"
    Write-Note "Need help later? Run: ha-nova doctor"
    return
  }

  & $BinaryPath setup
}

$UsePlainUi = Test-PlainUi
Write-Banner

$expectedVersion = Get-ExpectedInstallVersion
$version = Get-DownloadInstallVersion
if (-not (Test-CurrentInstall) -and (Test-LegacyInstall)) {
  Stop-ForLegacyInstall
}
$uninstallRecovery = Get-UninstallRecoveryState
if ($null -ne $uninstallRecovery) {
  Stop-ForUninstallRecovery -Recovery $uninstallRecovery
}
$bundleResult = Install-Bundle -Version $version
Ensure-InstallDirOnPath | Out-Null
if ($expectedVersion -and $bundleResult.Version -ne $expectedVersion) {
  Fail "Downloaded bundle version v$($bundleResult.Version) does not match requested version v$expectedVersion."
}
Cleanup-LegacyBundleInstall
Write-Info "Installed HA NOVA v$($bundleResult.Version)"
Start-Setup -BinaryPath (Join-Path $InstallDir "ha-nova.exe")
