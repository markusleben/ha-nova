[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][ValidateSet("claude", "codex", "opencode", "gemini", "all")][string]$Client,
  [Parameter(Mandatory = $true)][string]$BundleUrl,
  [Parameter(Mandatory = $true)][string]$BundleSha256Url,
  [string]$HAHost = "127.0.0.1",
  [string]$RelayToken = "test-relay-token",
  [string]$ResultPath = "$HOME\ha-nova-desktop-validation.txt"
)

$ErrorActionPreference = "Stop"
if ($PSVersionTable.PSVersion.Major -ge 7) {
  $PSNativeCommandUseErrorActionPreference = $false
}

function Get-MergedPath {
  $segments = [System.Collections.Generic.List[string]]::new()
  foreach ($scope in @("Process", "User", "Machine")) {
    $value = [Environment]::GetEnvironmentVariable("Path", $scope)
    if ([string]::IsNullOrWhiteSpace($value)) {
      continue
    }
    foreach ($segment in ($value -split ';')) {
      if ([string]::IsNullOrWhiteSpace($segment)) {
        continue
      }
      $trimmed = $segment.Trim()
      if (-not $segments.Contains($trimmed)) {
        $segments.Add($trimmed)
      }
    }
  }
  return ($segments -join ';')
}

function Invoke-Cli {
  param(
    [Parameter(Mandatory = $true)][string[]]$Arguments
  )

  $escaped = $Arguments | ForEach-Object {
    if ($_ -match '[\s"]') {
      '"' + ($_.Replace('"', '\"')) + '"'
    }
    else {
      $_
    }
  }
  $commandLine = "ha-nova " + ($escaped -join " ") + " 2>&1"
  $lines = & cmd.exe /d /s /c $commandLine
  $exitCode = $LASTEXITCODE
  return @{
    Lines = @($lines | ForEach-Object { [string]$_ })
    ExitCode = $exitCode
  }
}

& "$PSScriptRoot\windows-clean-test-state.ps1" | Out-Null

$env:Path = Get-MergedPath
$env:HA_NOVA_BUNDLE_URL = $BundleUrl
$env:HA_NOVA_BUNDLE_SHA256_URL = $BundleSha256Url
$env:HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"
$env:HA_NOVA_NO_SETUP = "1"
$env:HA_NOVA_NO_BROWSER = "1"
$env:HA_NOVA_KEYRING_SERVICE = "ha-nova.test.desktop.$Client"
$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }

$log = [System.Collections.Generic.List[string]]::new()
& "$PSScriptRoot\..\..\install.ps1" | ForEach-Object { $log.Add([string]$_) }
$versionResult = Invoke-Cli -Arguments @("version")
$versionResult.Lines | ForEach-Object { $log.Add($_) }
$version = ($versionResult.Lines -join "`n").Trim()
$log.Add("VERSION_EXIT:$($versionResult.ExitCode)")
$setupResult = Invoke-Cli -Arguments @("setup", $Client, "--host", $HAHost, "--relay-token", $RelayToken, "--non-interactive")
$setupResult.Lines | ForEach-Object { $log.Add($_) }
$setupExit = $setupResult.ExitCode
$log.Add("SETUP_EXIT:$setupExit")
$doctorExit = -1
$updateExit = -1
$InstallDir = Join-Path $LocalAppDataDir "Programs\ha-nova"
if ($setupExit -eq 0) {
  $doctorResult = Invoke-Cli -Arguments @("doctor")
  $doctorResult.Lines | ForEach-Object { $log.Add($_) }
  $doctorExit = $doctorResult.ExitCode
  $log.Add("DOCTOR_EXIT:$doctorExit")
}
if ($setupExit -eq 0 -and $doctorExit -eq 0 -and $versionResult.ExitCode -eq 0) {
  $updateResult = Invoke-Cli -Arguments @("update", "--version", $version)
  $updateResult.Lines | ForEach-Object { $log.Add($_) }
  $updateExit = $updateResult.ExitCode
  $log.Add("UPDATE_EXIT:$updateExit")
}

$checks = @(
  @{ Name = "codex"; Path = Join-Path $HOME ".agents\skills\ha-nova\ha-nova\SKILL.md" },
  @{ Name = "opencode"; Path = Join-Path $HOME ".config\opencode\skills\ha-nova\ha-nova\SKILL.md" },
  @{ Name = "gemini-root"; Path = Join-Path $HOME ".gemini\skills\ha-nova\SKILL.md" },
  @{ Name = "gemini-sub"; Path = Join-Path $HOME ".gemini\skills\ha-nova-review\SKILL.md" },
  @{ Name = "claude-installed-plugins"; Path = Join-Path $HOME ".claude\plugins\installed_plugins.json" }
)
foreach ($check in $checks) {
  $log.Add("CHECK:$($check.Name):$(Test-Path -LiteralPath $check.Path)")
}

$validationError = $null
switch ($Client) {
  "codex" {
    if (-not (Test-Path -LiteralPath (Join-Path $HOME ".agents\skills\ha-nova\ha-nova\SKILL.md"))) {
      $validationError = "codex skill tree missing"
    }
  }
  "opencode" {
    if (-not (Test-Path -LiteralPath (Join-Path $HOME ".config\opencode\skills\ha-nova\ha-nova\SKILL.md"))) {
      $validationError = "opencode skill tree missing"
    }
  }
  "gemini" {
    if (
      (-not (Test-Path -LiteralPath (Join-Path $HOME ".gemini\skills\ha-nova\SKILL.md"))) -or
      (-not (Test-Path -LiteralPath (Join-Path $HOME ".gemini\skills\ha-nova-review\SKILL.md")))
    ) {
      $validationError = "gemini skill tree missing"
    }
  }
  "claude" {
    $pluginsJson = Join-Path $HOME ".claude\plugins\installed_plugins.json"
    $marketplacesJson = Join-Path $HOME ".claude\plugins\known_marketplaces.json"
    if (-not (Test-Path -LiteralPath $pluginsJson)) {
      $validationError = "claude installed_plugins.json missing"
      break
    }
    if (-not (Test-Path -LiteralPath $marketplacesJson)) {
      $validationError = "claude known_marketplaces.json missing"
      break
    }
    $pluginsRaw = Get-Content -LiteralPath $pluginsJson -Raw
    $marketplacesRaw = Get-Content -LiteralPath $marketplacesJson -Raw
    $log.Add("CLAUDE_PLUGINS_JSON_PRESENT:True")
    if ($pluginsRaw -notmatch "ha-nova@ha-nova") {
      $validationError = "claude plugin missing from installed_plugins.json"
    }
    elseif ($marketplacesRaw -notmatch [regex]::Escape($InstallDir)) {
      $validationError = "claude marketplace source is not the local install root"
    }
  }
  "all" {
    $requiredPaths = @(
      (Join-Path $HOME ".agents\skills\ha-nova\ha-nova\SKILL.md"),
      (Join-Path $HOME ".config\opencode\skills\ha-nova\ha-nova\SKILL.md"),
      (Join-Path $HOME ".gemini\skills\ha-nova\SKILL.md"),
      (Join-Path $HOME ".gemini\skills\ha-nova-review\SKILL.md"),
      (Join-Path $HOME ".claude\plugins\installed_plugins.json")
    )
    foreach ($requiredPath in $requiredPaths) {
      if (-not (Test-Path -LiteralPath $requiredPath)) {
        $validationError = "all-client validation missing: $requiredPath"
        break
      }
    }
    if (-not $validationError) {
      $pluginsJson = Join-Path $HOME ".claude\plugins\installed_plugins.json"
      $marketplacesJson = Join-Path $HOME ".claude\plugins\known_marketplaces.json"
      $pluginsRaw = Get-Content -LiteralPath $pluginsJson -Raw
      $marketplacesRaw = Get-Content -LiteralPath $marketplacesJson -Raw
      if ($pluginsRaw -notmatch "ha-nova@ha-nova") {
        $validationError = "all-client validation missing Claude plugin record"
      }
      elseif ($marketplacesRaw -notmatch [regex]::Escape($InstallDir)) {
        $validationError = "all-client validation missing local Claude marketplace source"
      }
    }
  }
}

$uninstallResult = Invoke-Cli -Arguments @("uninstall", "--yes")
$uninstallResult.Lines | ForEach-Object { $log.Add($_) }
$log.Add("UNINSTALL_EXIT:$($uninstallResult.ExitCode)")
for ($i = 0; $i -lt 20; $i++) {
  if (-not (Test-Path -LiteralPath $InstallDir)) { break }
  Start-Sleep -Milliseconds 500
}
$installStillExists = Test-Path -LiteralPath $InstallDir
$log.Add("UNINSTALL_EXISTS:$installStillExists")
$marketplacesJson = Join-Path $HOME ".claude\plugins\known_marketplaces.json"
if ((Test-Path -LiteralPath $marketplacesJson) -and (Get-Content -LiteralPath $marketplacesJson -Raw) -match [regex]::Escape($InstallDir)) {
  throw "claude marketplace source still points at removed install root"
}
$log | Set-Content -LiteralPath $ResultPath -Encoding UTF8

if ($installStillExists) {
  throw "install dir still present"
}
if ($validationError) {
  throw $validationError
}
if ($versionResult.ExitCode -ne 0) {
  throw "version failed"
}
if ($setupExit -ne 0) {
  throw "setup failed"
}
if ($doctorExit -ne 0) {
  throw "doctor failed"
}
if ($updateExit -ne 0) {
  throw "update failed"
}
if ($uninstallResult.ExitCode -ne 0) {
  throw "uninstall failed"
}
