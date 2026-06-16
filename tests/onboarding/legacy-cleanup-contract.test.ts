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
    expect(content).toContain(
      "Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction SilentlyContinue",
    );
    expect(content).toContain(
      "Remove-Item -LiteralPath $InstallDir -Recurse -Force -ErrorAction SilentlyContinue",
    );
    expect(content).toContain(
      "Remove-Item -LiteralPath $_.FullName -Recurse -Force -ErrorAction SilentlyContinue",
    );
    expect(content).not.toMatch(/-Recurse -Force(?! -ErrorAction)/);
  });
});
