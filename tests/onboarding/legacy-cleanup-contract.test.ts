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
  });
});
