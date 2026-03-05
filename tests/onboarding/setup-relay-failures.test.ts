/**
 * S-7: Relay unreachable during setup
 */
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "./_helpers";

const isMac = process.platform === "darwin";

describe.skipIf(!isMac)("S-7: relay unreachable during setup", () => {
  it("retries 3 times then saves config anyway", { timeout: 30000 }, () => {

    const home = createMockHome();
    const binDir = createMockBinaries({ curlFails: true });

    // Input: host, relay URL, retries, accept save-anyway
    const input = [
      "192.168.1.5",   // HA host
      "n",             // don't retry host entry
      "y",             // continue with unverified
      "",              // relay URL default
      "",              // retry 1
      "",              // retry 2
      "",              // retry 3 (gives up, saves anyway)
      "",
    ].join("\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "claude"],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 30000,
        env: mockEnv(home, binDir),
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");

    // Should exhaust retries
    expect(output).toContain("Can't reach relay");
    expect(output).toContain("Saving config anyway");

    // Config still saved
    expect(result.status).toBe(0);
    const config = readFileSync(join(home, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("HA_HOST=192.168.1.5");
  });

  it("shows relay probe failure diagnostics", { timeout: 30000 }, () => {

    const home = createMockHome();
    const binDir = createMockBinaries({ curlFails: true });

    const input = [
      "192.168.1.5", "n", "y", "", "", "", "", "",
    ].join("\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "claude"],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 30000,
        env: mockEnv(home, binDir),
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    // Should contain diagnostic explanation
    expect(output).toContain("Relay not reachable");
  });
});
