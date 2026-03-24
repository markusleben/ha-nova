# HA NOVA Windows bootstrap installer for PowerShell.
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoOwner = "markusleben"
$RepoName = "ha-nova"
$WingetPackageId = "markusleben.ha-nova"
$LatestReleaseUrl = "https://api.github.com/repos/markusleben/ha-nova/releases/latest"
$ReleaseBaseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download"
$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
$AppDataDir = if ($env:APPDATA) { $env:APPDATA } else { Join-Path $HOME "AppData\Roaming" }
$InstallDir = Join-Path $LocalAppDataDir "Programs\ha-nova"
$PublicCommandDir = $InstallDir
$WingetPortableLink = Join-Path $LocalAppDataDir "Microsoft\WinGet\Links\ha-nova.exe"
$WingetPackagesDir = Join-Path $LocalAppDataDir "Microsoft\WinGet\Packages"
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

function Test-WingetBundleRoot {
  param(
    [string]$Candidate
  )

  if (-not $Candidate) {
    return $false
  }

  $root = $Candidate.TrimEnd('\')
  return (Test-Path -LiteralPath (Join-Path $root "ha-nova.exe")) -and (Test-Path -LiteralPath (Join-Path $root "bundle.json"))
}

function Test-WingetPackageRootInstall {
  $packageRoots = @(Get-ChildItem -LiteralPath $WingetPackagesDir -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -like "$WingetPackageId*" })
  foreach ($packageRoot in $packageRoots) {
    if (Test-WingetBundleRoot -Candidate (Join-Path $packageRoot.FullName "ha-nova")) {
      return $true
    }
    if (Test-WingetBundleRoot -Candidate $packageRoot.FullName) {
      return $true
    }
  }

  return $false
}

function Get-WingetInstallInventoryState {
  $wingetCommand = Get-Command winget -ErrorAction SilentlyContinue
  if (-not $wingetCommand) {
    return "absent"
  }

  try {
    $output = & $wingetCommand.Source list --id $WingetPackageId --exact --source winget 2>$null | Out-String
    if ($output -match [regex]::Escape($WingetPackageId)) {
      return "installed"
    }

    $fallbackOutput = & $wingetCommand.Source list ha-nova 2>$null | Out-String
    if ($fallbackOutput -match [regex]::Escape($WingetPackageId)) {
      return "installed"
    }

    return "absent"
  }
  catch {
    return "unknown"
  }
}

function Test-WingetInstall {
  if (Test-Path -LiteralPath $WingetPortableLink) {
    return $true
  }

  $inventoryState = Get-WingetInstallInventoryState
  if ($inventoryState -eq "installed") {
    return $true
  }
  if ($inventoryState -eq "unknown") {
    return $true
  }

  return Test-WingetPackageRootInstall
}

function Stop-ForWingetInstall {
  Fail @"
A winget-managed HA NOVA install was detected.
This bundle installer will not create a second Windows install channel.

Keep a single Windows install channel:
  Update existing install:
    winget upgrade --id $WingetPackageId --exact

  Remove existing install first:
    winget uninstall --id $WingetPackageId --exact

Then rerun install.ps1 only if you intentionally want the bundle fallback path.
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

function Get-ExistingUninstallRemainingPaths {
  param(
    $Status
  )

  $remaining = @()
  foreach ($candidate in @($Status.remaining_paths)) {
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
    $wingetRuntimePresent = Test-Path -LiteralPath $WingetPortableLink
    $pathResidue = Test-UserPathContainsEntry -TargetPath $InstallDir
    if (-not ($bundleRuntimePresent -or $wingetRuntimePresent) -and -not $pathResidue) {
      Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
      return $null
    }
    return [pscustomobject]@{
      Kind = "failed"
      Summary = "A previous background HA NOVA uninstall left an unreadable recovery marker."
      RecoveryCommand = (Get-UninstallRecoveryCommand -Mode "standard")
      RemainingPaths = @()
      RuntimePresent = ($bundleRuntimePresent -or $wingetRuntimePresent)
      PathResidue = $pathResidue
    }
  }

  if ($status.status -eq "success") {
    Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
    return $null
  }

  $mode = if ($status.mode -eq "purge") { "purge" } else { "standard" }
  $recoveryCommand = Get-UninstallRecoveryCommand -Mode $mode
  $bundleRuntimePresent = Test-Path -LiteralPath (Join-Path $InstallDir "ha-nova.exe")
  $wingetRuntimePresent = Test-Path -LiteralPath $WingetPortableLink
  $runtimePresent = $bundleRuntimePresent -or $wingetRuntimePresent
  $remainingPaths = Get-ExistingUninstallRemainingPaths -Status $status
  $pathResidue = Test-UserPathContainsEntry -TargetPath $InstallDir

  if ($status.status -eq "running") {
    $updatedAt = $null
    if ($status.last_updated_at) {
      try {
        $updatedAt = [DateTimeOffset]::Parse([string]$status.last_updated_at)
      }
      catch {
        $updatedAt = $null
      }
    }
    if (-not $updatedAt -and $status.started_at) {
      try {
        $updatedAt = [DateTimeOffset]::Parse([string]$status.started_at)
      }
      catch {
        $updatedAt = $null
      }
    }

    $helperAlive = $false
    if ($status.helper_pid) {
      try {
        $helperAlive = $null -ne (Get-Process -Id ([int]$status.helper_pid) -ErrorAction SilentlyContinue)
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

  if ($status.status -eq "failed") {
    if (-not $runtimePresent -and -not $pathResidue -and $remainingPaths.Count -eq 0) {
      Remove-Item -LiteralPath $UninstallStatusPath -Force -ErrorAction SilentlyContinue
      return $null
    }

    $summary = if ($status.error_summary) { [string]$status.error_summary } else { "A previous background HA NOVA uninstall did not finish cleanly." }
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
    Invoke-WebRequest -Uri $bundleUrl -OutFile $archivePath
    Invoke-WebRequest -Uri (Get-BundleChecksumUrl -Version $Version) -OutFile $checksumPath
    $expectedHash = (Get-Content -LiteralPath $checksumPath -Raw).Trim().Split()[0]
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
      Move-Item -LiteralPath $LegacyInstallDir -Destination $InstallDir -Force
      Write-Info "Migrated previous Windows install to $InstallDir"
    }

    $installParent = Split-Path -Parent $InstallDir
    $nextRoot = Join-Path $installParent (".ha-nova-next-" + [guid]::NewGuid().ToString("N"))
    $backupRoot = Join-Path $installParent (".ha-nova-old-" + [guid]::NewGuid().ToString("N"))

    New-Item -ItemType Directory -Force -Path $installParent | Out-Null
    Copy-Item -Path $bundleRoot -Destination $nextRoot -Recurse -Force

    if (Test-Path -LiteralPath $InstallDir) {
      Move-Item -LiteralPath $InstallDir -Destination $backupRoot -Force
    }

    try {
      Move-Item -LiteralPath $nextRoot -Destination $InstallDir -Force
      if (Test-Path -LiteralPath $backupRoot) {
        Remove-Item -LiteralPath $backupRoot -Recurse -Force
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
      Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
  }
}

function Ensure-InstallDirOnPath {
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $parts = @()
  if ($userPath) {
    $parts = $userPath -split ";" | Where-Object { $_ -and $_ -ne $LegacyInstallDir }
  }

  if ($parts -contains $PublicCommandDir) {
    Write-Info "$PublicCommandDir already configured in PATH"
    return $false
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
if ($uninstallRecovery = Get-UninstallRecoveryState) {
  Stop-ForUninstallRecovery -Recovery $uninstallRecovery
}
if (Test-WingetInstall) {
  Stop-ForWingetInstall
}
$bundleResult = Install-Bundle -Version $version
$pathManaged = Ensure-InstallDirOnPath
if ($expectedVersion -and $bundleResult.Version -ne $expectedVersion) {
  Fail "Downloaded bundle version v$($bundleResult.Version) does not match requested version v$expectedVersion."
}
Cleanup-LegacyBundleInstall
Write-Info "Installed HA NOVA v$($bundleResult.Version)"
Start-Setup -BinaryPath (Join-Path $InstallDir "ha-nova.exe")
