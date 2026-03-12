/**
 * S-7: Relay unreachable during setup
 */
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "./_helpers.js";

const isMac = process.platform === "darwin";

describe.skipIf(!isMac)("S-7: relay unreachable during setup", () => {
  it("retries 3 times then saves config anyway", { timeout: 30000 }, () => {

    const home = createMockHome();
    const binDir = createMockBinaries({ curlFails: true });

    // Phase 1b: host prompt → validation fails → continue anyway
    // Phase 2: app install (Enter × 2)
    // Phase 3: token gen → open config → saved (Enter × 2)
    // Phase 3b: LLAT → open profile → open config → app running (Enter × 3)
    // Phase 4: relay retries (URL prompt × 3, gives up)
    const input = [
      "192.168.1.5",   // HA host prompt
      "n",             // "Try a different address?" → no
      "y",             // "Continue anyway?" → yes
      "",              // app install: open browser
      "",              // app install: installation complete
      "",              // relay token: open settings
      "",              // relay token: saved
      "",              // LLAT: open HA profile
      "",              // LLAT: open relay settings
      "",              // LLAT: app running
      "",              // relay retry 1: [Enter] retry
      "",              // relay retry 2: [Enter] retry
      "",              // relay retry 3: gives up, saves anyway
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

    // Should exhaust retries with troubleshooting checklist
    expect(output).toContain("Can't reach the relay");
    expect(output).toContain("Quick checklist");
    expect(output).toContain("Saving your settings anyway");

    // Config still saved
    expect(result.status).toBe(0);
    const config = readFileSync(join(home, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("HA_HOST=192.168.1.5");
  });

  it("shows relay probe failure diagnostics", { timeout: 30000 }, () => {

    const home = createMockHome();
    const binDir = createMockBinaries({ curlFails: true });

    const input = [
      "192.168.1.5", "n", "y",  // host
      "", "",                     // app install
      "", "",                     // relay token
      "", "", "",                 // LLAT
      "", "", "",                 // relay retries
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
    // Should contain diagnostic checklist
    expect(output).toContain("Can't reach the relay");
    expect(output).toContain("Quick checklist");
    expect(output).toContain("NOVA Relay app started");
    expect(output).toContain('Is the "Relay Auth Token" field ("relay_auth_token") saved');
  });
});
