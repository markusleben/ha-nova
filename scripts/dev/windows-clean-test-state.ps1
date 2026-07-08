[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
$AppDataDir = if ($env:APPDATA) { $env:APPDATA } else { Join-Path $HOME "AppData\Roaming" }
$LocalDataDir = Join-Path $LocalAppDataDir "ha-nova"
$InstallDir = Join-Path $LocalAppDataDir "Programs\ha-nova"
$LegacyInstallDir = Join-Path $HOME ".local\share\ha-nova"
$ConfigDir = Join-Path $AppDataDir "ha-nova"
$LegacyConfigDir = Join-Path $HOME ".config\ha-nova"
$CacheDir = Join-Path $LocalAppDataDir "ha-nova\cache"
$WingetLink = Join-Path $LocalAppDataDir "Microsoft\WinGet\Links\ha-nova.exe"
$WingetPackagesRoot = Join-Path $LocalAppDataDir "Microsoft\WinGet\Packages"

function Remove-ClaudePluginRecord {
  $pluginsJson = Join-Path $HOME ".claude\plugins\installed_plugins.json"
  if (-not (Test-Path -LiteralPath $pluginsJson)) {
    return
  }

  $raw = Get-Content -LiteralPath $pluginsJson -Raw
  if ([string]::IsNullOrWhiteSpace($raw)) {
    Remove-Item -LiteralPath $pluginsJson -Force -ErrorAction SilentlyContinue
    return
  }

  try {
    $parsed = $raw | ConvertFrom-Json -ErrorAction Stop
  }
  catch {
    Remove-Item -LiteralPath $pluginsJson -Force -ErrorAction SilentlyContinue
    return
  }

  if ($null -eq $parsed.plugins) {
    return
  }

  if ($parsed.plugins -is [System.Collections.IDictionary] -or $parsed.plugins.PSObject.Properties.Name -contains "ha-nova@ha-nova") {
    if ($parsed.plugins.PSObject.Properties.Name -contains "ha-nova@ha-nova") {
      $parsed.plugins.PSObject.Properties.Remove("ha-nova@ha-nova")
    }
    elseif ($parsed.plugins -is [System.Collections.IDictionary]) {
      $parsed.plugins.Remove("ha-nova@ha-nova") | Out-Null
    }

    if ($parsed.plugins.PSObject.Properties.Count -eq 0) {
      Remove-Item -LiteralPath $pluginsJson -Force -ErrorAction SilentlyContinue
      return
    }

    $parsed | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $pluginsJson -Encoding UTF8
    return
  }

  $filtered = @()
  foreach ($plugin in @($parsed.plugins)) {
    $name = ""
    if ($plugin -is [string]) {
      $name = $plugin
    }
    elseif ($plugin.PSObject.Properties.Name -contains "name") {
      $name = [string]$plugin.name
    }
    elseif ($plugin.PSObject.Properties.Name -contains "id") {
      $name = [string]$plugin.id
    }
    elseif ($plugin.PSObject.Properties.Name -contains "plugin") {
      $name = [string]$plugin.plugin
    }

    if ($name -eq "ha-nova@ha-nova") {
      continue
    }
    $filtered += $plugin
  }

  if ($filtered.Count -eq 0) {
    Remove-Item -LiteralPath $pluginsJson -Force -ErrorAction SilentlyContinue
    return
  }

  $parsed.plugins = $filtered
  $parsed | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $pluginsJson -Encoding UTF8
}

function Remove-ClaudeMarketplaceRecord {
  $marketplacesJson = Join-Path $HOME ".claude\plugins\known_marketplaces.json"
  if (-not (Test-Path -LiteralPath $marketplacesJson)) {
    return
  }

  $raw = Get-Content -LiteralPath $marketplacesJson -Raw
  if ([string]::IsNullOrWhiteSpace($raw)) {
    Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue
    return
  }

  try {
    $parsed = $raw | ConvertFrom-Json -ErrorAction Stop
  }
  catch {
    Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue
    return
  }

  if ($parsed -is [System.Collections.IDictionary] -or $parsed.PSObject.Properties.Name -contains "ha-nova") {
    if ($parsed.PSObject.Properties.Name -contains "ha-nova") {
      $parsed.PSObject.Properties.Remove("ha-nova")
    }
    elseif ($parsed -is [System.Collections.IDictionary]) {
      $parsed.Remove("ha-nova") | Out-Null
    }

    if ($parsed.PSObject.Properties.Count -eq 0) {
      Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue
      return
    }

    $parsed | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $marketplacesJson -Encoding UTF8
    return
  }

  $filtered = @()
  foreach ($marketplace in @($parsed)) {
    $name = ""
    if ($marketplace -is [string]) {
      $name = $marketplace
    }
    elseif ($marketplace.PSObject.Properties.Name -contains "name") {
      $name = [string]$marketplace.name
    }
    elseif ($marketplace.PSObject.Properties.Name -contains "id") {
      $name = [string]$marketplace.id
    }
    elseif ($marketplace.PSObject.Properties.Name -contains "slug") {
      $name = [string]$marketplace.slug
    }

    if ($name -eq "ha-nova") {
      continue
    }
    $filtered += $marketplace
  }

  if ($filtered.Count -eq 0) {
    Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue
    return
  }

  if ($parsed -is [System.Array] -or $parsed.GetType().Name -eq "Object[]") {
    $filtered | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $marketplacesJson -Encoding UTF8
    return
  }

  Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue
}

function Remove-HANovaTestCredentials {
  $cmdkey = Get-Command cmdkey.exe -ErrorAction SilentlyContinue
  if ($null -eq $cmdkey) {
    return
  }

  $output = & $cmdkey.Source /list 2>$null
  if ($LASTEXITCODE -ne 0 -or $null -eq $output) {
    return
  }

  foreach ($line in $output) {
    if ($line -match 'Target:\s*(.+)$') {
      $target = $Matches[1].Trim()
      if (
        $target -like '*ha-nova.relay-auth-token*' -or
        $target -like '*ha-nova.test.*' -or
        $target -like '*ha-nova.test.manual.*'
      ) {
        & $cmdkey.Source /delete:$target 2>$null | Out-Null
      }
    }
  }
}

function Remove-HANovaUserEnvironment {
  foreach ($name in @(
    "HA_NOVA_BUNDLE_URL",
    "HA_NOVA_BUNDLE_SHA256_URL",
    "HA_NOVA_VERSION",
    "HA_NOVA_CLAUDE_MARKETPLACE_LOCAL",
    "HA_NOVA_NO_SETUP",
    "HA_NOVA_NO_BROWSER",
    "HA_NOVA_KEYRING_SERVICE",
    "HA_NOVA_ALLOW_INSECURE_TEST_KEYRING",
    "HA_NOVA_TEST_KEYRING_FILE"
  )) {
    Remove-Item -Path "HKCU:\Environment\$name" -Force -ErrorAction SilentlyContinue
  }
}

function Remove-InstallRootWithRuntime {
  param(
    [Parameter(Mandatory = $true)][string]$Root
  )

  $runtimeExe = Join-Path $Root "ha-nova.exe"
  if (-not (Test-Path -LiteralPath $runtimeExe)) {
    return
  }

  try {
    & $runtimeExe uninstall --yes | Out-Null
    for ($i = 0; $i -lt 20; $i++) {
      if (-not (Test-Path -LiteralPath $Root)) {
        break
      }
      Start-Sleep -Milliseconds 500
    }
  }
  catch {
    Write-Host "WARN: background uninstall did not complete cleanly for $Root; continuing with dev cleanup"
  }
}

function Remove-WingetTestInstall {
  $winget = Get-Command winget -ErrorAction SilentlyContinue
  if ($null -eq $winget) {
    return
  }

  foreach ($args in @(
    @("uninstall", "--id", "markusleben.ha-nova", "--exact"),
    @("uninstall", "HA NOVA")
  )) {
    try {
      & $winget.Source @args 2>$null | Out-Null
    }
    catch {
    }
  }
}

function Remove-HermesWslSkills {
  $wsl = Get-Command wsl.exe -ErrorAction SilentlyContinue
  if ($null -eq $wsl) {
    return
  }

  try {
    & $wsl.Source sh -lc 'rm -rf ~/.hermes/skills/ha-nova' 2>$null | Out-Null
  }
  catch {
  }
}

Remove-WingetTestInstall
Remove-HermesWslSkills
Remove-InstallRootWithRuntime -Root $InstallDir
if ($LegacyInstallDir -ne $InstallDir) {
  Remove-InstallRootWithRuntime -Root $LegacyInstallDir
}

$paths = @(
  $InstallDir,
  $LegacyInstallDir,
  $ConfigDir,
  $LegacyConfigDir,
  $LocalDataDir,
  $CacheDir,
  $WingetLink,
  (Join-Path $HOME ".agents\skills\ha-nova"),
  (Join-Path $HOME ".config\opencode\skills\ha-nova"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-scene"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-calendar"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-dashboard"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-organize"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-health"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-history"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-write"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-read"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-helper"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-entity-discovery"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-onboarding"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-service-call"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-review"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-fallback"),
  (Join-Path $HOME ".gemini\config\skills\ha-nova-guide"),
  (Join-Path $HOME ".gemini\skills\ha-nova"),
  (Join-Path $HOME ".hermes\skills\ha-nova"),
  (Join-Path $HOME ".gemini\skills\ha-nova-scene"),
  (Join-Path $HOME ".gemini\skills\ha-nova-calendar"),
  (Join-Path $HOME ".gemini\skills\ha-nova-dashboard"),
  (Join-Path $HOME ".gemini\skills\ha-nova-organize"),
  (Join-Path $HOME ".gemini\skills\ha-nova-health"),
  (Join-Path $HOME ".gemini\skills\ha-nova-history"),
  (Join-Path $HOME ".gemini\skills\ha-nova-write"),
  (Join-Path $HOME ".gemini\skills\ha-nova-read"),
  (Join-Path $HOME ".gemini\skills\ha-nova-helper"),
  (Join-Path $HOME ".gemini\skills\ha-nova-entity-discovery"),
  (Join-Path $HOME ".gemini\skills\ha-nova-onboarding"),
  (Join-Path $HOME ".gemini\skills\ha-nova-service-call"),
  (Join-Path $HOME ".gemini\skills\ha-nova-review"),
  (Join-Path $HOME ".gemini\skills\ha-nova-fallback"),
  (Join-Path $HOME ".gemini\skills\ha-nova-guide"),
  (Join-Path $HOME ".claude\skills\ha-nova"),
  (Join-Path $ConfigDir "claude-marketplace")
)

foreach ($path in $paths) {
  if (Test-Path -LiteralPath $path) {
    Remove-Item -LiteralPath $path -Recurse -Force
  }
}

Get-ChildItem -LiteralPath $WingetPackagesRoot -Filter "markusleben.ha-nova*" -ErrorAction SilentlyContinue | ForEach-Object {
  Remove-Item -LiteralPath $_.FullName -Recurse -Force -ErrorAction SilentlyContinue
}

Remove-ClaudePluginRecord
Remove-ClaudeMarketplaceRecord
Remove-HANovaTestCredentials
Remove-HANovaUserEnvironment

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$parts = @()
if ($userPath) {
  $parts = $userPath -split ";" | Where-Object {
    $_ -and $_ -ne $InstallDir -and $_ -ne $LegacyInstallDir
  }
}
[Environment]::SetEnvironmentVariable("Path", ($parts -join ";"), "User")

Write-Host "CLEAN"
