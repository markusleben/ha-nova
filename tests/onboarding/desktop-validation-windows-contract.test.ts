import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("Windows desktop validation helpers contract", () => {
  const packageJson = readFileSync("package.json", "utf8");
  const windowsCleanup = readFileSync(
    "scripts/dev/windows-clean-test-state.ps1",
    "utf8",
  );
  const windowsInstall = readFileSync(
    "scripts/dev/windows-private-rc-install.ps1",
    "utf8",
  );
  const windowsDesktop = readFileSync(
    "scripts/dev/windows-desktop-setup.ps1",
    "utf8",
  );
  const windowsPublic = readFileSync(
    "scripts/dev/windows-public-onboarding.ps1",
    "utf8",
  );

  it("keeps the Windows cleanup helper scoped to HA NOVA-owned paths", () => {
    expect(windowsCleanup).toContain("Programs\\ha-nova");
    expect(windowsCleanup).toContain("AppData\\Roaming");
    expect(windowsCleanup).toContain('Join-Path $LocalAppDataDir "ha-nova"');
    expect(windowsCleanup).toContain("Microsoft\\WinGet\\Links\\ha-nova.exe");
    expect(windowsCleanup).toContain("markusleben.ha-nova");
    expect(windowsCleanup).toContain(".agents\\skills\\ha-nova");
    expect(windowsCleanup).toContain(".config\\opencode\\skills\\ha-nova");
    expect(windowsCleanup).toContain(".gemini\\config\\skills\\ha-nova");
    expect(windowsCleanup).toContain(
      ".gemini\\config\\skills\\ha-nova-calendar",
    );
    expect(windowsCleanup).toContain(".gemini\\config\\skills\\ha-nova-health");
    expect(windowsCleanup).toContain(".gemini\\config\\skills\\ha-nova-guide");
    expect(windowsCleanup).toContain(".gemini\\skills\\ha-nova");
    expect(windowsCleanup).toContain(".gemini\\skills\\ha-nova-calendar");
    expect(windowsCleanup).toContain(".gemini\\skills\\ha-nova-health");
    expect(windowsCleanup).toContain(".gemini\\skills\\ha-nova-guide");
    expect(windowsCleanup).toContain(".hermes\\skills\\ha-nova");
    expect(windowsCleanup).toContain(
      ".claude\\plugins\\installed_plugins.json",
    );
    expect(windowsCleanup).toContain(
      'Join-Path $ConfigDir "claude-marketplace"',
    );
    expect(windowsCleanup).toContain("Remove-ClaudePluginRecord");
    expect(windowsCleanup).toContain("Remove-ClaudeMarketplaceRecord");
    expect(windowsCleanup).toContain(
      ".claude\\plugins\\known_marketplaces.json",
    );
    expect(windowsCleanup).toContain(
      "Remove-Item -LiteralPath $marketplacesJson -Force -ErrorAction SilentlyContinue",
    );
    expect(windowsCleanup).toContain("Remove-HANovaTestCredentials");
    expect(windowsCleanup).toContain("Remove-HANovaUserEnvironment");
    expect(windowsCleanup).toContain("Remove-HermesWslSkills");
    expect(windowsCleanup).toContain("wsl.exe");
    expect(windowsCleanup).toContain("HKCU:\\Environment\\$name");
    expect(windowsCleanup).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(windowsCleanup).toContain("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING");
    expect(windowsCleanup).toContain("HA_NOVA_TEST_KEYRING_FILE");
    expect(windowsCleanup).toContain("cmdkey.exe");
    expect(windowsCleanup).toContain("ha-nova.relay-auth-token");
    expect(windowsCleanup).not.toContain('Join-Path $HOME ".agents"');
    expect(windowsCleanup).not.toContain('Join-Path $HOME ".config\\opencode"');
    expect(windowsCleanup).not.toContain('Join-Path $HOME ".gemini"');
  });

  it("lets Windows public onboarding detect Antigravity Desktop without agy", () => {
    expect(windowsPublic).toContain("function Test-AntigravityAvailable");
    expect(windowsPublic).toContain(
      "function Test-AntigravityDesktopAvailable",
    );
    expect(windowsPublic).toContain('Test-CommandAvailable "agy"');
    expect(windowsPublic).toContain("Programs\\antigravity\\Antigravity.exe");
    expect(windowsPublic).toContain("Programs\\Antigravity\\Antigravity.exe");
    expect(windowsPublic).toContain("if (Test-AntigravityAvailable)");
    expect(windowsPublic).toContain("[switch]$RequireAntigravityDesktopOnly");
    expect(windowsPublic).toContain("antigravity_desktop_available");
    expect(windowsPublic).toContain("agy_available");
    expect(windowsPublic).toContain('"antigravity-desktop-guided-setup"');
    expect(packageJson).toContain("HA_NOVA_REQUIRE_ANTIGRAVITY_DESKTOP_ONLY");
  });

  it("treats Windows installer and desktop runner failures as hard failures", () => {
    expect(windowsInstall).toContain("VERSION_EXIT:");
    expect(windowsInstall).toContain("UNINSTALL_EXIT:");
    expect(windowsInstall).toContain("UNINSTALL_STATUS_EXISTS:");
    expect(windowsInstall).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(windowsInstall).toContain('HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"');
    expect(windowsInstall).toContain("Programs\\ha-nova");
    expect(windowsInstall).toContain("cmd.exe /d /s /c");
    expect(windowsInstall).toContain("Wait-ForCondition");
    expect(windowsInstall).toContain('throw "ha-nova version failed"');
    expect(windowsInstall).toContain('throw "ha-nova uninstall failed"');
    expect(windowsInstall).toContain(
      'throw "uninstall status marker still present"',
    );
    expect(windowsDesktop).toContain("VERSION_EXIT:");
    expect(windowsDesktop).toContain("SETUP_EXIT:");
    expect(windowsDesktop).toContain("DOCTOR_EXIT:");
    expect(windowsDesktop).toContain("UPDATE_EXIT:");
    expect(windowsDesktop).toContain("POST_UPDATE_VERSION_EXIT:");
    expect(windowsDesktop).toContain("POST_UPDATE_VERSION:");
    expect(windowsDesktop).toContain("UNINSTALL_EXIT:");
    expect(windowsDesktop).toContain("PURGE_UNINSTALL_EXIT:");
    expect(windowsDesktop).toContain("STANDARD_CONFIG_EXISTS:");
    expect(windowsDesktop).toContain("STANDARD_STATE_EXISTS:");
    expect(windowsDesktop).toContain("STANDARD_CACHE_EXISTS:");
    expect(windowsDesktop).toContain("STANDARD_UNINSTALL_STATUS_EXISTS:");
    expect(windowsDesktop).toContain("STANDARD_TOKEN_EXISTS:");
    expect(windowsDesktop).toContain("STANDARD_TOKEN_MATCHES:");
    expect(windowsDesktop).toContain("PURGE_CONFIG_EXISTS:");
    expect(windowsDesktop).toContain("PURGE_STATE_EXISTS:");
    expect(windowsDesktop).toContain("PURGE_CACHE_EXISTS:");
    expect(windowsDesktop).toContain("PURGE_UNINSTALL_STATUS_EXISTS:");
    expect(windowsDesktop).toContain("PURGE_TOKEN_EXISTS:");
    expect(windowsDesktop).toContain(
      "TOKEN_VALIDATION_MODE:test-keyring-override",
    );
    expect(windowsDesktop).toContain("HA_NOVA_KEYRING_SERVICE");
    expect(windowsDesktop).toContain("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING");
    expect(windowsDesktop).toContain("HA_NOVA_TEST_KEYRING_FILE");
    expect(windowsDesktop).toContain(
      'Join-Path $AppDataDir "ha-nova\\.test-relay-auth-token"',
    );
    expect(windowsDesktop).not.toContain(
      'Join-Path $HOME ".config\\ha-nova\\.test-relay-auth-token"',
    );
    expect(windowsDesktop).toContain('HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = "1"');
    expect(windowsDesktop).toContain("Programs\\ha-nova");
    expect(windowsDesktop).toContain("Get-MergedPath");
    expect(windowsDesktop).toContain("Wait-ForCondition");
    expect(windowsDesktop).toContain("$env:Path = Get-MergedPath");
    expect(windowsDesktop).toContain('[string]$HAHost = "127.0.0.1"');
    expect(windowsDesktop).not.toContain('[string]$Host = "127.0.0.1"');
    expect(windowsDesktop).toContain("& ha-nova @Arguments 2>&1");
    expect(windowsDesktop).not.toContain("cmd.exe /d /s /c");
    expect(windowsDesktop).toContain("$AppDataDir = if ($env:APPDATA)");
    expect(windowsDesktop).toContain("claude installed_plugins.json missing");
    expect(windowsDesktop).toContain("claude known_marketplaces.json missing");
    expect(windowsDesktop).toContain("ha-nova@ha-nova");
    expect(windowsDesktop).toContain(
      "claude marketplace source is not the local install root",
    );
    expect(windowsDesktop).toContain('"all" {');
    expect(windowsDesktop).toContain(
      "all-client validation missing Claude plugin record",
    );
    expect(windowsDesktop).toContain(
      "all-client validation missing local Claude marketplace source",
    );
    expect(windowsDesktop).toContain('throw "setup failed"');
    expect(windowsDesktop).toContain('throw "doctor failed"');
    expect(windowsDesktop).toContain('throw "update failed"');
    expect(windowsDesktop).toContain('throw "uninstall failed"');
    expect(windowsDesktop).toContain('throw "purge uninstall failed"');
    expect(windowsDesktop).toContain('throw "version failed"');
    expect(windowsDesktop).toContain(
      'throw "standard uninstall removed config unexpectedly"',
    );
    expect(windowsDesktop).toContain(
      'throw "standard uninstall left recovery marker unexpectedly"',
    );
    expect(windowsDesktop).toContain(
      'throw "standard uninstall removed relay token unexpectedly"',
    );
    expect(windowsDesktop).toContain(
      'throw "standard uninstall kept a corrupted relay token unexpectedly"',
    );
    expect(windowsDesktop).toContain(
      'throw "purge uninstall left config unexpectedly"',
    );
    expect(windowsDesktop).toContain(
      'throw "purge uninstall left recovery marker unexpectedly"',
    );
    expect(windowsDesktop).toContain(
      'throw "purge uninstall left relay token unexpectedly"',
    );
    expect(packageJson).toContain('"test:desktop:windows:antigravity"');
    expect(packageJson).toContain("-Client antigravity");
  });

  it("ships a dedicated public Windows onboarding validator with structured evidence", () => {
    expect(windowsPublic).toContain('[ValidateSet("stable", "rc")]');
    expect(windowsPublic).toContain(
      '[ValidateSet("clean", "reinstall", "stale-uninstall-marker")]',
    );
    expect(windowsPublic).toContain("Get-ReadyClients");
    expect(windowsPublic).toContain("host_form");
    expect(windowsPublic).toContain("standard_user");
    expect(windowsPublic).toContain("ready_clients");
    expect(windowsPublic).toContain("expected_public_result");
    expect(windowsPublic).toContain("local_install_completed");
    expect(windowsPublic).toContain("client_prerequisite_guidance_displayed");
    expect(windowsPublic).toContain(
      "No supported AI client is ready on this machine yet.",
    );
    expect(windowsPublic).toContain(
      "Install one supported client first, then rerun: ha-nova setup",
    );
    expect(windowsPublic).toContain(
      "Invoke-Expression (Get-Content -LiteralPath $InstallScript -Raw)",
    );
    expect(windowsPublic).toContain("Start-Transcript");
    expect(windowsPublic).toContain("InstallerOutput");
    expect(windowsPublic).not.toContain("Tee-Object");
    expect(windowsPublic).not.toContain("cmd.exe /d /s /c");
    expect(windowsPublic).not.toContain("*>&1");
    expect(windowsPublic).toContain("PUBLIC_WINDOWS_ONBOARDING_EVIDENCE:");
    expect(windowsPublic).toContain("PUBLIC_WINDOWS_ONBOARDING_VERDICT:");
    expect(windowsPublic).toContain("setup_auto_started");
    expect(windowsPublic).toContain("second_terminal_command_needed");
    expect(windowsPublic).toContain("manual_fallback_displayed");
    expect(windowsPublic).toContain("final_verdict");
    expect(windowsPublic).toContain("Remove-Item Env:HA_NOVA_NO_SETUP");
  });
});
