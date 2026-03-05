/**
 * S-1: Fresh Install (Happy Path)
 * Tests the full 4-phase setup wizard from clean state.
 */
import { readFileSync, statSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "./_helpers";

const isMac = process.platform === "darwin";

describe.skipIf(!isMac)("S-1: fresh install happy path", () => {
  it("completes full setup with mock HA + relay", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();

    // Interactive input: host, accept HA URL, relay URL, confirmations
    const input = [
      "192.168.1.5",   // HA host
      "",              // accept default relay URL
      "",              // press enter (various prompts)
      "",
      "",
      "",
      "y",             // continue anyway if needed
      "",
    ].join("\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "claude"],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).toBe(0);

    // Config file created
    const config = readFileSync(join(home, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("HA_HOST=");
    expect(config).toContain("192.168.1.5");
    expect(config).toContain("HA_URL=");
    expect(config).toContain("RELAY_BASE_URL=");

    // Config file has secure permissions
    const configStats = statSync(join(home, ".config/ha-nova/onboarding.env"));
    // eslint-disable-next-line no-bitwise
    expect((configStats.mode & 0o777)).toBe(0o600);

    // Output contains success markers
    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("Setup complete!");
  });

  it("installs skills for codex as symlink during fresh setup", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();

    const input = ["192.168.1.5", "", "", "", "", "", "y", ""].join("\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "codex"],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).toBe(0);

    // Codex symlink created
    const linkTarget = readFileSync(join(home, ".agents/skills/ha-nova/ha-nova/SKILL.md"), "utf8");
    expect(linkTarget).toContain("name: ha-nova");
  });

  it("supports non-interactive setup with --host and --token flags", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();

    const result = spawnSync(
      "bash",
      [
        "scripts/onboarding/bin/ha-nova",
        "setup", "codex",
        "--host=192.168.1.5",
        "--token=test-relay-token-abc123",
      ],
      {
        cwd: REPO_ROOT,
        input: "",
        encoding: "utf8",
        timeout: 30000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).toBe(0);

    // Config file created with correct values
    const config = readFileSync(join(home, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("192.168.1.5");
    expect(config).toContain("RELAY_BASE_URL=");

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("Setup complete!");
    expect(output).toContain("Using host from --host flag");
  });

  it("supports partial flag: --host only, token interactively", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();

    // With --host but no --token, token phase runs interactively
    // Token: auto-generated → [Enter] open config → [Enter] saved → LLAT guide → [Enter] × 2
    const input = [
      "", // open app config
      "", // saved token
      "", // open HA profile
      "", // LLAT done
    ].join("\n");

    const result = spawnSync(
      "bash",
      [
        "scripts/onboarding/bin/ha-nova",
        "setup", "codex",
        "--host=192.168.1.5",
      ],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 30000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).toBe(0);

    const config = readFileSync(join(home, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("192.168.1.5");

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("Setup complete!");
    expect(output).toContain("Using host from --host flag");
  });
});
