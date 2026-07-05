import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("desktop validation helper behavior", () => {
  const macosSuite = readFileSync("scripts/dev/macos-private-rc-suite.sh", "utf8");
  const windowsPublic = readFileSync("scripts/dev/windows-public-onboarding.ps1", "utf8");

  it("keeps the macOS suite ordered around one local bundle server", () => {
    expect(macosSuite).toContain('SERVER_LOG="$(mktemp');
    expect(macosSuite).toContain('trap cleanup EXIT');
    expect(macosSuite).toContain('ensure_port_free "${BUNDLE_SERVER_PORT}"');
    expect(macosSuite).toContain('python3 -m http.server "${BUNDLE_SERVER_PORT}" --directory dist/install-bundles');
    expect(macosSuite).toContain('wait_for_server');
    expect(macosSuite).toContain('export BUNDLE_SERVER_BASE_URL="http://127.0.0.1:${BUNDLE_SERVER_PORT}"');
    expect(macosSuite).toMatch(
      /bash scripts\/dev\/macos-private-rc-smoke\.sh[\s\S]+bash scripts\/dev\/macos-private-rc-setup-all\.sh[\s\S]+bash scripts\/dev\/macos-private-rc-client\.sh codex[\s\S]+bash scripts\/dev\/macos-private-rc-client\.sh opencode[\s\S]+bash scripts\/dev\/macos-private-rc-client\.sh antigravity[\s\S]+bash scripts\/dev\/macos-private-rc-client\.sh claude/,
    );
  });

  it("accepts public Windows onboarding only for the two documented success paths", () => {
    expect(windowsPublic).toMatch(/function Test-AntigravityAvailable[\s\S]+Test-CommandAvailable "agy"[\s\S]+return \$true[\s\S]+return Test-AntigravityDesktopAvailable/);
    expect(windowsPublic).toMatch(/function Test-AntigravityDesktopAvailable[\s\S]+Programs\\antigravity\\Antigravity\.exe[\s\S]+return \$true/);
    expect(windowsPublic).toMatch(/function Get-ReadyClients[\s\S]+if \(Test-AntigravityAvailable\) \{[\s\S]+\$clients \+= "antigravity"/);
    expect(windowsPublic).toContain("$expectedPublicResult = if ($RequireAntigravityDesktopOnly) {");
    expect(windowsPublic).toContain('"antigravity-desktop-guided-setup"');
    expect(windowsPublic).toContain('elseif ($readyClients.Count -gt 0) { "guided-setup" } else { "missing-client-guidance" }');
    expect(windowsPublic).toContain('$readyClients.Count -gt 0 -and $result.ExitCode -eq 0 -and $setupAutoStarted -and -not $manualFallbackDisplayed');
    expect(windowsPublic).toContain('$readyClients.Count -eq 0 -and $result.ExitCode -eq 0 -and $localInstallCompleted -and $missingClientGuidanceDisplayed');
    expect(windowsPublic).toContain('throw "public Windows onboarding validation failed"');
    expect(windowsPublic).not.toContain('second_terminal_command_needed -eq $true');
  });

  it("keeps Antigravity Desktop-only proof from passing through agy or missing-client fallback", () => {
    expect(windowsPublic).toContain("$desktopOnlyProofPassed = (");
    expect(windowsPublic).toContain("$RequireAntigravityDesktopOnly -and");
    expect(windowsPublic).toContain("$antigravityDesktopAvailable -and");
    expect(windowsPublic).toContain("(-not $agyAvailable) -and");
    expect(windowsPublic).toContain('($readyClients -contains "antigravity") -and');
    expect(windowsPublic).toContain('((-not $RequireAntigravityDesktopOnly) -and $readyClients.Count -gt 0');
    expect(windowsPublic).toContain('((-not $RequireAntigravityDesktopOnly) -and $readyClients.Count -eq 0');
  });
});
