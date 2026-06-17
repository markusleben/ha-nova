import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("legacy cleanup contract", () => {
  const unixScript = "scripts/legacy-uninstall.sh";
  const windowsScript = "scripts/legacy-uninstall.ps1";

  it("ships a standalone Unix cleanup entrypoint for pre-Go installs", () => {
    const stats = statSync(unixScript);
    const content = readFileSync(unixScript, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain("onboarding.env");
    expect(content).toContain("version-check");
    expect(content).toContain("ha-nova uninstall");
  });

  it("ships a standalone Windows cleanup entrypoint for pre-Go installs", () => {
    const stats = statSync(windowsScript);
    const content = readFileSync(windowsScript, "utf8");

    expect((stats.mode & constants.S_IRUSR) !== 0).toBe(true);
    expect(content).toContain("PowerShell");
    expect(content).toContain("onboarding.env");
    expect(content).toContain("version-check");
    expect(content).toContain("ha-nova uninstall");
    expect(content).toContain("Programs\\ha-nova");
    expect(content).toContain("Microsoft\\WinGet\\Links\\ha-nova.exe");
    expect(content).toContain("A current Go install was detected");
  });

  it("keeps Windows legacy cleanup best-effort so a locked file cannot trap users in a residue loop", () => {
    const content = readFileSync(windowsScript, "utf8");
    // Under Set-StrictMode + ErrorActionPreference=Stop, an unguarded Remove-Item
    // on a locked legacy file (running relay.exe, antivirus handle) aborts the
    // whole cleanup, leaving residue the installer still detects -> the user is
    // stuck in a re-run loop. Every Remove-Item must tolerate a failed delete.
    // Remove-IfExists is the single best-effort deleter: it wraps Remove-Item in
    // try/catch so a TERMINATING Win32Exception ("Access is denied") - which
    // -ErrorAction SilentlyContinue does NOT suppress under Stop mode - cannot abort
    // the cleanup.
    expect(content).toMatch(
      /function Remove-IfExists[\s\S]*?try\s*\{[\s\S]*?Remove-Item -LiteralPath \$Path -Recurse -Force -ErrorAction SilentlyContinue[\s\S]*?\}\s*\r?\n\s*catch\s*\{/,
    );
    // Every recursive delete routes through that helper, not a bare Remove-Item.
    expect(content).toContain("Remove-IfExists $InstallDir");
    expect(content).toContain("Remove-IfExists $_.FullName");
    expect(content).not.toMatch(/-Recurse -Force(?! -ErrorAction)/);
    // ...but a locked BLOCKER file must not slip through as a false success: the
    // script verifies the install.ps1 Test-LegacyInstall blockers are actually
    // gone and Fails with the residue list, or the user loops (cleanup reports OK
    // while the installer keeps detecting residue and aborting).
    expect(content).toContain("$blockerResidue");
    expect(content).toContain("Could not remove some legacy files");
  });
});
