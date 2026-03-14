# HA NOVA Windows bootstrap installer for PowerShell.
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoOwner = "markusleben"
$RepoName = "ha-nova"
$LatestReleaseUrl = "https://api.github.com/repos/markusleben/ha-nova/releases/latest"
$ReleaseBaseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download"
$InstallDir = Join-Path $HOME ".local\share\ha-nova"
$BinDir = Join-Path $HOME ".local\bin"
$LauncherPath = Join-Path $BinDir "ha-nova.cmd"
$LegacyUninstallUrl = "https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1"
$ConfigDir = Join-Path $HOME ".config\ha-nova"
$StatePath = Join-Path $ConfigDir "state.json"

function Write-Info([string]$Message) {
  Write-Host "  [ok] $Message"
}

function Fail([string]$Message) {
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

  Fail "HA NOVA currently supports Windows amd64 only."
}

function Get-InstallVersion {
  if ($env:HA_NOVA_VERSION) {
    return $env:HA_NOVA_VERSION.TrimStart("v")
  }

  $release = Invoke-RestMethod -Uri $LatestReleaseUrl -Headers @{
    Accept = "application/vnd.github+json"
    "User-Agent" = "ha-nova-installer"
  }
  if (-not $release.tag_name) {
    Fail "Could not determine the latest HA NOVA release."
  }

  return ([string]$release.tag_name).TrimStart("v")
}

function Test-CurrentInstall {
  return Test-Path -LiteralPath (Join-Path $InstallDir "bundle.json")
}

function Test-LegacyInstall {
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
    if (Test-Path -LiteralPath $path) {
      return $true
    }
  }

  $legacyScriptsDir = Join-Path $InstallDir "scripts\onboarding"
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

function Install-Bundle {
  param(
    [Parameter(Mandatory = $true)][string]$Version
  )

  $null = Get-PlatformArch
  $bundleName = "ha-nova-windows-amd64.zip"
  $bundleUrl = "$ReleaseBaseUrl/v$Version/$bundleName"
  $tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("ha-nova-install-" + [guid]::NewGuid().ToString("N"))
  $archivePath = Join-Path $tempRoot $bundleName
  $checksumPath = "$archivePath.sha256"
  $extractDir = Join-Path $tempRoot "extract"

  New-Item -ItemType Directory -Force -Path $tempRoot | Out-Null
  New-Item -ItemType Directory -Force -Path $extractDir | Out-Null

  try {
    Invoke-WebRequest -Uri $bundleUrl -OutFile $archivePath
    Invoke-WebRequest -Uri "$bundleUrl.sha256" -OutFile $checksumPath
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

function Install-Binary {
  New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
  $runtimeBinary = Join-Path $InstallDir "ha-nova.exe"
  $launcher = @"
@echo off
"$runtimeBinary" %*
"@
  Set-Content -LiteralPath $LauncherPath -Value $launcher -Encoding ASCII
}

function Ensure-BinDirOnPath {
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $parts = @()
  if ($userPath) {
    $parts = $userPath -split ";" | Where-Object { $_ }
  }

  if ($parts -contains $BinDir) {
    Write-Info "$BinDir already configured in PATH"
    return $false
  }

  $newPath = @($BinDir) + $parts
  [Environment]::SetEnvironmentVariable("Path", ($newPath -join ";"), "User")
  $env:Path = "$BinDir;$env:Path"
  Write-Info "Added $BinDir to PATH"
  return $true
}

function Write-State {
  param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][bool]$PathManaged
  )

  New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
  $installedClients = @()
  $clientInstallModes = @{}

  if (Test-Path -LiteralPath $StatePath) {
    try {
      $existing = Get-Content -LiteralPath $StatePath -Raw | ConvertFrom-Json
      if ($existing.installed_clients) {
        $installedClients = @($existing.installed_clients)
      }
      if ($existing.client_install_modes) {
        $clientInstallModes = @{}
        foreach ($property in $existing.client_install_modes.PSObject.Properties) {
          $clientInstallModes[$property.Name] = $property.Value
        }
      }
    }
    catch {
    }
  }

  $state = [ordered]@{
    schema_version = 1
    version = $Version
    install_source = "bundle"
    installed_clients = $installedClients
    client_install_modes = $clientInstallModes
    path_managed = $PathManaged
    path_target = "user-path"
  }
  $state | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $StatePath -Encoding UTF8
}

function Start-Setup {
  param(
    [Parameter(Mandatory = $true)][string]$BinaryPath
  )

  if ($env:HA_NOVA_NO_SETUP -eq "1") {
    Write-Info "Installed HA NOVA without setup."
    Write-Host "  Next step: ha-nova setup"
    Write-Host "  Need help later? Run: ha-nova doctor"
    return
  }

  if (-not (Test-InteractiveSession)) {
    Write-Host "  [!!] No interactive terminal detected; setup was not started automatically."
    Write-Host "  Next step: ha-nova setup"
    Write-Host "  Need help later? Run: ha-nova doctor"
    return
  }

  & $BinaryPath setup
}

Write-Host ""
Write-Host "  ========================================="
Write-Host "  HA NOVA Windows Installer"
Write-Host "  ========================================="
Write-Host ""

$version = Get-InstallVersion
if (-not (Test-CurrentInstall) -and (Test-LegacyInstall)) {
  Stop-ForLegacyInstall
}
Install-Bundle -Version $version
Install-Binary
$pathManaged = Ensure-BinDirOnPath
Write-State -Version $version -PathManaged ([bool]$pathManaged)
Write-Info "Installed HA NOVA v$version"
Start-Setup -BinaryPath (Join-Path $InstallDir "ha-nova.exe")
