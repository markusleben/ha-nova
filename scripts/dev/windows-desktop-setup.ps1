[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][ValidateSet("claude", "codex", "opencode", "antigravity", "all")][string]$Client,
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

function Wait-ForCondition {
  param(
    [Parameter(Mandatory = $true)][scriptblock]$Condition,
    [Parameter(Mandatory = $true)][string]$Description,
    [int]$Attempts = 40,
    [int]$DelayMilliseconds = 500
  )

  for ($i = 0; $i -lt $Attempts; $i++) {
    if (& $Condition) {
      return
    }
    Start-Sleep -Milliseconds $DelayMilliseconds
  }

  throw "$Description did not complete in time"
}

& "$PSScriptRoot\windows-clean-test-state.ps1" | Out-Null

$env:Path = Get-MergedPath
$env:HA_NOVA_BUNDLE_URL = $BundleUrl
$env:HA_NOVA_BUNDLE_SHA256_URL = $BundleSha256Url
$env:HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"
$env:HA_NOVA_NO_SETUP = "1"
$env:HA_NOVA_NO_BROWSER = "1"
$env:HA_NOVA_ALLOW_INSECURE_TEST_KEYRING = "1"
$env:HA_NOVA_TEST_KEYRING_FILE = Join-Path $HOME ".config\ha-nova\.test-relay-auth-token"
$env:HA_NOVA_KEYRING_SERVICE = "ha-nova.test.desktop.$Client"
$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
$ConfigDir = Join-Path $env:APPDATA "ha-nova"
$ConfigFile = Join-Path $ConfigDir "config.json"
$StateFile = Join-Path $ConfigDir "state.json"
$CacheDir = Join-Path $LocalAppDataDir "ha-nova\cache"
$UninstallStatusPath = Join-Path $LocalAppDataDir "ha-nova\uninstall-status.json"
$TestKeyringFile = $env:HA_NOVA_TEST_KEYRING_FILE

$log = [System.Collections.Generic.List[string]]::new()
$validationError = $null
$log.Add("TOKEN_VALIDATION_MODE:test-keyring-override")
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
  if ($updateExit -eq 0) {
    $postUpdateVersionResult = Invoke-Cli -Arguments @("version")
    $postUpdateVersionResult.Lines | ForEach-Object { $log.Add($_) }
    $postUpdateVersion = ($postUpdateVersionResult.Lines -join "`n").Trim()
    $log.Add("POST_UPDATE_VERSION_EXIT:$($postUpdateVersionResult.ExitCode)")
    $log.Add("POST_UPDATE_VERSION:$postUpdateVersion")
    if ($postUpdateVersionResult.ExitCode -ne 0) {
      $validationError = "post-update version failed"
    }
    elseif ($postUpdateVersion -ne $version) {
      $validationError = "same-version update changed runtime version"
    }
  }
}

$checks = @(
  @{ Name = "codex"; Path = Join-Path $HOME ".agents\skills\ha-nova\ha-nova\SKILL.md" },
  @{ Name = "opencode"; Path = Join-Path $HOME ".config\opencode\skills\ha-nova\ha-nova\SKILL.md" },
  @{ Name = "antigravity-root"; Path = Join-Path $HOME ".gemini\antigravity\skills\ha-nova\SKILL.md" },
  @{ Name = "antigravity-sub"; Path = Join-Path $HOME ".gemini\antigravity\skills\ha-nova-review\SKILL.md" },
  @{ Name = "claude-installed-plugins"; Path = Join-Path $HOME ".claude\plugins\installed_plugins.json" }
)
foreach ($check in $checks) {
  $log.Add("CHECK:$($check.Name):$(Test-Path -LiteralPath $check.Path)")
}

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
  "antigravity" {
    if (
      (-not (Test-Path -LiteralPath (Join-Path $HOME ".gemini\antigravity\skills\ha-nova\SKILL.md"))) -or
      (-not (Test-Path -LiteralPath (Join-Path $HOME ".gemini\antigravity\skills\ha-nova-review\SKILL.md")))
    ) {
      $validationError = "antigravity skill tree missing"
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
      (Join-Path $HOME ".gemini\antigravity\skills\ha-nova\SKILL.md"),
      (Join-Path $HOME ".gemini\antigravity\skills\ha-nova-review\SKILL.md"),
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
if ($uninstallResult.ExitCode -eq 0) {
  Wait-ForCondition -Description "standard uninstall" -Condition {
    (-not (Test-Path -LiteralPath $InstallDir)) -and (-not (Test-Path -LiteralPath $UninstallStatusPath))
  }
}
$standardInstallStillExists = Test-Path -LiteralPath $InstallDir
$standardConfigExists = Test-Path -LiteralPath $ConfigFile
$standardStateExists = Test-Path -LiteralPath $StateFile
$standardCacheExists = Test-Path -LiteralPath $CacheDir
$standardStatusExists = Test-Path -LiteralPath $UninstallStatusPath
$standardTokenExists = Test-Path -LiteralPath $TestKeyringFile
$standardTokenMatches = $false
if ($standardTokenExists) {
  $standardTokenMatches = ((Get-Content -LiteralPath $TestKeyringFile -Raw).Trim() -eq $RelayToken)
}
$log.Add("UNINSTALL_EXISTS:$standardInstallStillExists")
$log.Add("STANDARD_CONFIG_EXISTS:$standardConfigExists")
$log.Add("STANDARD_STATE_EXISTS:$standardStateExists")
$log.Add("STANDARD_CACHE_EXISTS:$standardCacheExists")
$log.Add("STANDARD_UNINSTALL_STATUS_EXISTS:$standardStatusExists")
$log.Add("STANDARD_TOKEN_EXISTS:$standardTokenExists")
$log.Add("STANDARD_TOKEN_MATCHES:$standardTokenMatches")
$marketplacesJson = Join-Path $HOME ".claude\plugins\known_marketplaces.json"
if ((Test-Path -LiteralPath $marketplacesJson) -and (Get-Content -LiteralPath $marketplacesJson -Raw) -match [regex]::Escape($InstallDir)) {
  throw "claude marketplace source still points at removed install root"
}

& "$PSScriptRoot\..\..\install.ps1" | ForEach-Object { $log.Add([string]$_) }
$purgeUninstallResult = Invoke-Cli -Arguments @("uninstall", "--yes", "--purge")
$purgeUninstallResult.Lines | ForEach-Object { $log.Add($_) }
$log.Add("PURGE_UNINSTALL_EXIT:$($purgeUninstallResult.ExitCode)")
if ($purgeUninstallResult.ExitCode -eq 0) {
  Wait-ForCondition -Description "purge uninstall" -Condition {
    (-not (Test-Path -LiteralPath $InstallDir)) -and (-not (Test-Path -LiteralPath $UninstallStatusPath))
  }
}
$purgeInstallStillExists = Test-Path -LiteralPath $InstallDir
$purgeConfigExists = Test-Path -LiteralPath $ConfigFile
$purgeStateExists = Test-Path -LiteralPath $StateFile
$purgeCacheExists = Test-Path -LiteralPath $CacheDir
$purgeStatusExists = Test-Path -LiteralPath $UninstallStatusPath
$purgeTokenExists = Test-Path -LiteralPath $TestKeyringFile
$log.Add("PURGE_UNINSTALL_EXISTS:$purgeInstallStillExists")
$log.Add("PURGE_CONFIG_EXISTS:$purgeConfigExists")
$log.Add("PURGE_STATE_EXISTS:$purgeStateExists")
$log.Add("PURGE_CACHE_EXISTS:$purgeCacheExists")
$log.Add("PURGE_UNINSTALL_STATUS_EXISTS:$purgeStatusExists")
$log.Add("PURGE_TOKEN_EXISTS:$purgeTokenExists")

$log | Set-Content -LiteralPath $ResultPath -Encoding UTF8

if ($standardInstallStillExists) {
  throw "install dir still present after standard uninstall"
}
if (-not $standardConfigExists) {
  throw "standard uninstall removed config unexpectedly"
}
if ($standardStateExists) {
  throw "standard uninstall left state unexpectedly"
}
if ($standardCacheExists) {
  throw "standard uninstall left cache unexpectedly"
}
if ($standardStatusExists) {
  throw "standard uninstall left recovery marker unexpectedly"
}
if (-not $standardTokenExists) {
  throw "standard uninstall removed relay token unexpectedly"
}
if (-not $standardTokenMatches) {
  throw "standard uninstall kept a corrupted relay token unexpectedly"
}
if ($purgeInstallStillExists) {
  throw "install dir still present after purge uninstall"
}
if ($purgeConfigExists) {
  throw "purge uninstall left config unexpectedly"
}
if ($purgeStateExists) {
  throw "purge uninstall left state unexpectedly"
}
if ($purgeCacheExists) {
  throw "purge uninstall left cache unexpectedly"
}
if ($purgeStatusExists) {
  throw "purge uninstall left recovery marker unexpectedly"
}
if ($purgeTokenExists) {
  throw "purge uninstall left relay token unexpectedly"
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
if ($purgeUninstallResult.ExitCode -ne 0) {
  throw "purge uninstall failed"
}
