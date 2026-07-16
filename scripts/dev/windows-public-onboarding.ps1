[CmdletBinding()]
param(
  [ValidateSet("stable", "rc")][string]$InstallSource = "stable",
  [ValidateSet("clean", "reinstall", "stale-uninstall-marker")][string]$StartState = "clean",
  [string]$BundleUrl,
  [string]$BundleSha256Url,
  [switch]$RequireAntigravityDesktopOnly,
  [string]$ResultPath = "$HOME\ha-nova-public-onboarding.json"
)

$ErrorActionPreference = "Stop"
if ($PSVersionTable.PSVersion.Major -ge 7) {
  $PSNativeCommandUseErrorActionPreference = $false
}

$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$InstallScript = Join-Path $RepoRoot "install.ps1"
$WindowsCleanup = Join-Path $PSScriptRoot "windows-clean-test-state.ps1"
$LocalAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
$InstallDir = Join-Path $LocalAppDataDir "Programs\ha-nova"
$UninstallStatusPath = Join-Path $LocalAppDataDir "ha-nova\uninstall-status.json"
$TranscriptPath = Join-Path ([System.IO.Path]::GetTempPath()) ("ha-nova-public-onboarding-" + [guid]::NewGuid().ToString("N") + ".log")
$GitBashCandidates = @(
  "$env:ProgramFiles\Git\bin\bash.exe",
  "${env:ProgramFiles(x86)}\Git\bin\bash.exe",
  "$env:LOCALAPPDATA\Programs\Git\bin\bash.exe"
)

function Get-HostForm {
  if ($env:WT_SESSION) {
    return "windows-terminal"
  }
  return "powershell-console"
}

function Test-StandardUser {
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = New-Object Security.Principal.WindowsPrincipal($identity)
  return -not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Test-CommandAvailable {
  param([Parameter(Mandatory = $true)][string]$Name)

  try {
    $null = Get-Command $Name -ErrorAction Stop
    return $true
  }
  catch {
    return $false
  }
}

function Test-ClaudeGitBashAvailable {
  if ($env:CLAUDE_CODE_GIT_BASH_PATH -and (Test-Path -LiteralPath $env:CLAUDE_CODE_GIT_BASH_PATH)) {
    return $true
  }
  foreach ($candidate in $GitBashCandidates) {
    if ($candidate -and (Test-Path -LiteralPath $candidate)) {
      return $true
    }
  }
  return $false
}

function Test-AntigravityAvailable {
  if (Test-CommandAvailable "agy") {
    return $true
  }

  return Test-AntigravityDesktopAvailable
}

function Test-AntigravityDesktopAvailable {
  $localAppDataDir = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $HOME "AppData\Local" }
  $desktopCandidates = @(
    (Join-Path $localAppDataDir "Programs\antigravity\Antigravity.exe"),
    (Join-Path $localAppDataDir "Programs\Antigravity\Antigravity.exe")
  )
  foreach ($candidate in $desktopCandidates) {
    if ($candidate -and (Test-Path -LiteralPath $candidate)) {
      return $true
    }
  }
  return $false
}

function Get-ReadyClients {
  $clients = @()
  if ((Test-CommandAvailable "claude") -and (Test-ClaudeGitBashAvailable)) {
    $clients += "claude"
  }
  if (Test-AntigravityAvailable) {
    $clients += "antigravity"
  }
  if (Test-CommandAvailable "codex") {
    $clients += "codex"
  }
  if (Test-CommandAvailable "opencode") {
    $clients += "opencode"
  }
  return $clients
}

function Set-InstallEnv {
  param(
    [Parameter(Mandatory = $true)][string]$Source,
    [bool]$DisableSetup = $false
  )

  Remove-Item Env:HA_NOVA_BUNDLE_URL -ErrorAction SilentlyContinue
  Remove-Item Env:HA_NOVA_BUNDLE_SHA256_URL -ErrorAction SilentlyContinue
  Remove-Item Env:HA_NOVA_NO_SETUP -ErrorAction SilentlyContinue
  Remove-Item Env:HA_NOVA_NO_BROWSER -ErrorAction SilentlyContinue

  if ($Source -eq "rc") {
    if (-not $BundleUrl -or -not $BundleSha256Url) {
      throw "BundleUrl and BundleSha256Url are required for InstallSource=rc"
    }
    $env:HA_NOVA_BUNDLE_URL = $BundleUrl
    $env:HA_NOVA_BUNDLE_SHA256_URL = $BundleSha256Url
  }

  if ($DisableSetup) {
    $env:HA_NOVA_NO_SETUP = "1"
    $env:HA_NOVA_NO_BROWSER = "1"
  }
}

function Initialize-StartState {
  param(
    [Parameter(Mandatory = $true)][string]$State,
    [Parameter(Mandatory = $true)][string]$Source
  )

  & $WindowsCleanup | Out-Null

  switch ($State) {
    "clean" {
      return
    }
    "reinstall" {
      Set-InstallEnv -Source $Source -DisableSetup $true
      try {
        Invoke-Expression (Get-Content -LiteralPath $InstallScript -Raw) | Out-Null
      }
      finally {
        Set-InstallEnv -Source $Source -DisableSetup $false
      }
      return
    }
    "stale-uninstall-marker" {
      $markerDir = Split-Path -Parent $UninstallStatusPath
      New-Item -ItemType Directory -Force -Path $markerDir | Out-Null
      @'
{
  "status": "failed",
  "mode": "standard",
  "error_summary": "stale uninstall marker seeded for public onboarding validation",
  "remaining_paths": []
}
'@ | Set-Content -LiteralPath $UninstallStatusPath -Encoding UTF8
      return
    }
  }
}

function Invoke-PublicInstaller {
  $exitCode = 0
  $caught = $null
  $transcriptStarted = $false
  $installerOutput = New-Object System.Text.StringBuilder

  function Add-InstallerOutput {
    param(
      [AllowEmptyString()][string]$Text,
      [switch]$NoNewline
    )

    $installerOutput.Append($Text) | Out-Null
    if (-not $NoNewline) {
      $installerOutput.AppendLine() | Out-Null
    }
  }

  # Windows 10 PowerShell 5.1 transcripts can omit nested host output. Mirror
  # explicit installer UI writes into memory while the installer and its native
  # interactive child stay attached directly to the console.
  function Write-Host {
    [CmdletBinding()]
    param(
      [Parameter(Position = 0, ValueFromPipeline = $true, ValueFromRemainingArguments = $true)]
      [object[]]$Object,
      [string]$Separator = " ",
      [switch]$NoNewline,
      [ConsoleColor]$ForegroundColor,
      [ConsoleColor]$BackgroundColor
    )

    process {
      $parts = New-Object System.Collections.Generic.List[string]
      foreach ($item in $Object) {
        $parts.Add([string]$item)
      }
      $line = [string]::Join($Separator, $parts)
      Add-InstallerOutput -Text $line -NoNewline:$NoNewline

      $hostParameters = @{
        Object = $Object
        Separator = $Separator
        NoNewline = $NoNewline
      }
      if ($PSBoundParameters.ContainsKey("ForegroundColor")) {
        $hostParameters.ForegroundColor = $ForegroundColor
      }
      if ($PSBoundParameters.ContainsKey("BackgroundColor")) {
        $hostParameters.BackgroundColor = $BackgroundColor
      }
      Microsoft.PowerShell.Utility\Write-Host @hostParameters
    }
  }

  function Write-Output {
    [CmdletBinding()]
    param(
      [Parameter(Position = 0, ValueFromPipeline = $true, ValueFromRemainingArguments = $true)]
      [AllowNull()][object]$InputObject,
      [switch]$NoEnumerate
    )

    process {
      Add-InstallerOutput -Text ([string]$InputObject)
      Microsoft.PowerShell.Utility\Write-Output -InputObject $InputObject -NoEnumerate:$NoEnumerate
    }
  }

  try {
    Start-Transcript -Path $TranscriptPath -Force | Out-Null
    $transcriptStarted = $true
    Invoke-Expression (Get-Content -LiteralPath $InstallScript -Raw)
    if ($LASTEXITCODE) {
      $exitCode = $LASTEXITCODE
    }
  }
  catch {
    $caught = $_
    if ($LASTEXITCODE) {
      $exitCode = $LASTEXITCODE
    }
    elseif ($_.Exception.HResult) {
      $exitCode = 1
    }
  }
  finally {
    if ($transcriptStarted) {
      Stop-Transcript | Out-Null
    }
  }

  $script:PublicInstallerResult = @{
    ExitCode = $exitCode
    Error = $caught
    InstallerOutput = $installerOutput.ToString()
  }
}

Initialize-StartState -State $StartState -Source $InstallSource
Set-InstallEnv -Source $InstallSource -DisableSetup $false
$readyClients = @(Get-ReadyClients)
$agyAvailable = Test-CommandAvailable "agy"
$antigravityDesktopAvailable = Test-AntigravityDesktopAvailable

$script:PublicInstallerResult = $null
Invoke-PublicInstaller
$result = $script:PublicInstallerResult
if ($null -eq $result) {
  throw "public Windows installer did not return a validation result"
}
$transcript = if (Test-Path -LiteralPath $TranscriptPath) {
  Get-Content -LiteralPath $TranscriptPath -Raw
}
else {
  ""
}
$installerLog = @($transcript, $result.InstallerOutput) -join [Environment]::NewLine

$setupAutoStarted = (
  $installerLog -match "Press Enter to continue setup" -or
  $installerLog -match "Install NOVA Relay in Home Assistant" -or
  $installerLog -match "Set up Relay Auth Token" -or
  $installerLog -match "Setup complete!" -or
  $installerLog -match "Setup cancelled"
)
$manualFallbackDisplayed = (
  $installerLog -match [regex]::Escape("Next step: ha-nova setup") -or
  $installerLog -match [regex]::Escape("Finish setup later from a local PowerShell or Windows Terminal session:")
)
$missingClientGuidanceDisplayed = (
  $installerLog -match [regex]::Escape("No supported AI client is ready on this machine yet.") -and
  $installerLog -match [regex]::Escape("Install one supported client first, then rerun: ha-nova setup")
)
$localInstallCompleted = Test-Path -LiteralPath $InstallDir
$expectedPublicResult = if ($RequireAntigravityDesktopOnly) {
  "antigravity-desktop-guided-setup"
}
elseif ($readyClients.Count -gt 0) { "guided-setup" } else { "missing-client-guidance" }
$desktopOnlyProofPassed = (
  $RequireAntigravityDesktopOnly -and
  $antigravityDesktopAvailable -and
  (-not $agyAvailable) -and
  ($readyClients -contains "antigravity") -and
  $result.ExitCode -eq 0 -and
  $setupAutoStarted -and
  (-not $manualFallbackDisplayed)
)
$evidence = [ordered]@{
  windows_version = [System.Environment]::OSVersion.Version.ToString()
  powershell_version = $PSVersionTable.PSVersion.ToString()
  host_form = Get-HostForm
  standard_user = Test-StandardUser
  install_source = $InstallSource
  start_state = $StartState
  require_antigravity_desktop_only = [bool]$RequireAntigravityDesktopOnly
  agy_available = $agyAvailable
  antigravity_desktop_available = $antigravityDesktopAvailable
  ready_clients = $readyClients
  expected_public_result = $expectedPublicResult
  installer_exit_code = $result.ExitCode
  local_install_completed = $localInstallCompleted
  setup_auto_started = $setupAutoStarted
  second_terminal_command_needed = $manualFallbackDisplayed
  manual_fallback_displayed = $manualFallbackDisplayed
  client_prerequisite_guidance_displayed = $missingClientGuidanceDisplayed
  final_verdict = if (
    $desktopOnlyProofPassed -or
    ((-not $RequireAntigravityDesktopOnly) -and $readyClients.Count -gt 0 -and $result.ExitCode -eq 0 -and $setupAutoStarted -and -not $manualFallbackDisplayed) -or
    ((-not $RequireAntigravityDesktopOnly) -and $readyClients.Count -eq 0 -and $result.ExitCode -eq 0 -and $localInstallCompleted -and $missingClientGuidanceDisplayed)
  ) { "pass" } else { "fail" }
}

$evidence | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $ResultPath -Encoding UTF8

Write-Host ("PUBLIC_WINDOWS_ONBOARDING_EVIDENCE:" + $ResultPath)
Write-Host ("PUBLIC_WINDOWS_ONBOARDING_VERDICT:" + $evidence.final_verdict)

if ($null -ne $result.Error) {
  throw $result.Error
}
if ($evidence.final_verdict -ne "pass") {
  throw "public Windows onboarding validation failed"
}
