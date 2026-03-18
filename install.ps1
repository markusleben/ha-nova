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
$PublicCommandDir = $InstallDir
$LegacyUninstallUrl = "https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1"
$ConfigDir = Join-Path $HOME ".config\ha-nova"
$StatePath = Join-Path $ConfigDir "state.json"
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

  return "$ReleaseBaseUrl/v$Version/ha-nova-windows-amd64.zip"
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
    [string]$Version
  )

  $null = Get-PlatformArch
  $bundleName = "ha-nova-windows-amd64.zip"
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
    $parts = $userPath -split ";" | Where-Object { $_ }
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
      if ($existing.path_managed -eq $true -and $existing.path_target -eq "user-path") {
        $PathManaged = $true
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
$bundleResult = Install-Bundle -Version $version
$pathManaged = Ensure-InstallDirOnPath
if ($expectedVersion -and $bundleResult.Version -ne $expectedVersion) {
  Fail "Downloaded bundle version v$($bundleResult.Version) does not match requested version v$expectedVersion."
}
Write-State -Version $bundleResult.Version -PathManaged ([bool]$pathManaged)
Write-Info "Installed HA NOVA v$($bundleResult.Version)"
Start-Setup -BinaryPath (Join-Path $InstallDir "ha-nova.exe")
