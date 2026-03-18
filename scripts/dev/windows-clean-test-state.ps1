[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

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
      if ($target -like '*ha-nova.test.*' -or $target -like '*ha-nova.test.manual.*') {
        & $cmdkey.Source /delete:$target 2>$null | Out-Null
      }
    }
  }
}

$installDir = Join-Path $HOME ".local\share\ha-nova"
$runtimeExe = Join-Path $installDir "ha-nova.exe"
if (Test-Path -LiteralPath $runtimeExe) {
  try {
    & $runtimeExe uninstall --yes | Out-Null
    for ($i = 0; $i -lt 20; $i++) {
      if (-not (Test-Path -LiteralPath $installDir)) {
        break
      }
      Start-Sleep -Milliseconds 500
    }
  }
  catch {
    Write-Host "WARN: background uninstall did not complete cleanly; continuing with dev cleanup"
  }
}

$paths = @(
  $installDir,
  (Join-Path $HOME ".config\ha-nova"),
  (Join-Path $HOME ".agents\skills\ha-nova"),
  (Join-Path $HOME ".config\opencode\skills\ha-nova"),
  (Join-Path $HOME ".gemini\skills\ha-nova"),
  (Join-Path $HOME ".gemini\skills\ha-nova-write"),
  (Join-Path $HOME ".gemini\skills\ha-nova-read"),
  (Join-Path $HOME ".gemini\skills\ha-nova-helper"),
  (Join-Path $HOME ".gemini\skills\ha-nova-entity-discovery"),
  (Join-Path $HOME ".gemini\skills\ha-nova-onboarding"),
  (Join-Path $HOME ".gemini\skills\ha-nova-service-call"),
  (Join-Path $HOME ".gemini\skills\ha-nova-review"),
  (Join-Path $HOME ".gemini\skills\ha-nova-fallback"),
  (Join-Path $HOME ".claude\skills\ha-nova"),
  (Join-Path $HOME ".config\ha-nova\claude-marketplace")
)

foreach ($path in $paths) {
  if (Test-Path -LiteralPath $path) {
    Remove-Item -LiteralPath $path -Recurse -Force
  }
}

Remove-ClaudePluginRecord
Remove-HANovaTestCredentials

$installDir = Join-Path $HOME ".local\share\ha-nova"
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$parts = @()
if ($userPath) {
  $parts = $userPath -split ";" | Where-Object { $_ -and $_ -ne $installDir }
}
[Environment]::SetEnvironmentVariable("Path", ($parts -join ";"), "User")

Write-Host "CLEAN"
