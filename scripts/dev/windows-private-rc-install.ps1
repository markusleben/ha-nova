[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][string]$BundleUrl,
  [Parameter(Mandatory = $true)][string]$BundleSha256Url,
  [string]$ResultPath = "$HOME\ha-nova-private-rc-install.txt"
)

$ErrorActionPreference = "Stop"
if ($PSVersionTable.PSVersion.Major -ge 7) {
  $PSNativeCommandUseErrorActionPreference = $false
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

$env:HA_NOVA_BUNDLE_URL = $BundleUrl
$env:HA_NOVA_BUNDLE_SHA256_URL = $BundleSha256Url
$env:HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"
$env:HA_NOVA_NO_SETUP = "1"
$env:HA_NOVA_NO_BROWSER = "1"
$env:HA_NOVA_KEYRING_SERVICE = "ha-nova.test.private-rc.install"

$log = [System.Collections.Generic.List[string]]::new()
& "$PSScriptRoot\..\..\install.ps1" | ForEach-Object { $log.Add([string]$_) }
$versionResult = Invoke-Cli -Arguments @("version")
$versionResult.Lines | ForEach-Object { $log.Add($_) }
$log.Add("VERSION_EXIT:$($versionResult.ExitCode)")
$uninstallResult = Invoke-Cli -Arguments @("uninstall", "--yes")
$uninstallResult.Lines | ForEach-Object { $log.Add($_) }
$log.Add("UNINSTALL_EXIT:$($uninstallResult.ExitCode)")

$installDir = Join-Path $HOME ".local\share\ha-nova"
for ($i = 0; $i -lt 20; $i++) {
  if (-not (Test-Path -LiteralPath $installDir)) { break }
  Start-Sleep -Milliseconds 500
}
$log.Add("UNINSTALL_EXISTS:$(Test-Path -LiteralPath $installDir)")
$log | Set-Content -LiteralPath $ResultPath -Encoding UTF8

if ($versionResult.ExitCode -ne 0) {
  throw "ha-nova version failed"
}
if ($uninstallResult.ExitCode -ne 0) {
  throw "ha-nova uninstall failed"
}
if (Test-Path -LiteralPath $installDir) {
  throw "install dir still present"
}
