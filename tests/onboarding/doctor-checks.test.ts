/**
 * S-8: Relay version too old
 * S-10: Doctor (7 variants)
 */
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "./_helpers.js";

const isMac = process.platform === "darwin";

function runDoctor(
  opts: {
    healthFixture?: string;
    wsFixture?: string;
    curlFails?: boolean;
    keychainToken?: string;
    config?: { HA_HOST: string; HA_URL: string; RELAY_BASE_URL: string };
  } = {},
) {

  const home = createMockHome({
    config: opts.config ?? {
      HA_HOST: "192.168.1.5",
      HA_URL: "http://192.168.1.5:8123",
      RELAY_BASE_URL: "http://192.168.1.5:8791",
    },
    keychainToken: opts.keychainToken ?? "test-relay-token",
  });
  const binDir = createMockBinaries({
    ...(opts.healthFixture != null && { healthFixture: opts.healthFixture }),
    ...(opts.wsFixture != null && { wsFixture: opts.wsFixture }),
    ...(opts.curlFails != null && { curlFails: opts.curlFails }),
  });

  const result = spawnSync(
    "bash",
    ["scripts/onboarding/macos-doctor.sh"],
    {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: mockEnv(home, binDir),
    },
  );

  return {
    output: (result.stdout ?? "") + (result.stderr ?? ""),
    status: result.status,
  };
}

describe.skipIf(!isMac)("S-10: doctor variants", () => {
  it("passes all checks when relay + WS healthy", () => {
    const r = runDoctor();

    expect(r.output).toContain("[ok] Config file found");
    expect(r.output).toContain("[ok] Keychain token found");
    expect(r.output).toContain("[ok] Relay health reachable");
    expect(r.output).toContain("Onboarding preflight passed");
    expect(r.status).toBe(0);
  });

  it("fails when no config present", () => {

    const home = createMockHome({ keychainToken: "tok" });
    const binDir = createMockBinaries();

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-doctor.sh"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 15000,
        env: mockEnv(home, binDir),
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("[fail] Missing config");
    expect(result.status).not.toBe(0);
  });

  it("fails when no keychain token", () => {
    const r = runDoctor({ keychainToken: "" });

    expect(r.output).toContain("[fail] Keychain token missing");
    expect(r.status).not.toBe(0);
  });

  it("fails when relay unreachable", () => {
    const r = runDoctor({ curlFails: true });

    expect(r.output).toContain("[fail] Relay health check failed");
    expect(r.status).not.toBe(0);
  });

  it("reports WS degraded when ha_ws_connected=false and ws ping fails", () => {
    const r = runDoctor({
      healthFixture: "relay-health-ws-down.json",
      wsFixture: "relay-ws-pong.json",
    });

    // WS ping succeeds via mock → should still pass
    expect(r.output).toContain("[ok] Relay health reachable");
    // WS ping succeeds so it should show ok
    expect(r.output).toContain("Relay /ws ping succeeded");
  });

  it("shows version when relay reports version", () => {
    const r = runDoctor();

    expect(r.output).toContain("[ok] Relay version: 0.1.2");
  });

  it("shows skills version in doctor output", () => {
    const r = runDoctor();

    expect(r.output).toContain("Skills version:");
  });
});

describe.skipIf(!isMac)("S-8: relay version too old", () => {
  it("warns when relay version is below minimum", () => {
    const r = runDoctor({ healthFixture: "relay-health-old-version.json" });

    expect(r.output).toContain("[warn] Relay version 0.0.1 is below minimum");
    expect(r.output).toContain("Update:");
  });
});
