/**
 * S-2: Upgrade (everything present) — "Already set up"
 * S-3: Resume after partial abort
 */
import { mkdirSync, symlinkSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "./_helpers";

const isMac = process.platform === "darwin";

describe.skipIf(!isMac)("S-2: smart resume — already set up", () => {
  it("exits early when relay + WS + skills all OK", () => {

    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "test-relay-token-abc123",
    });
    const binDir = createMockBinaries();

    // Pre-install codex skill symlink so detect_setup_state finds skills
    const skillsDir = join(home, ".agents/skills");
    mkdirSync(skillsDir, { recursive: true });
    symlinkSync(join(REPO_ROOT, "skills"), join(skillsDir, "ha-nova"));

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "codex"],
      {
        cwd: REPO_ROOT,
        input: "",
        encoding: "utf8",
        timeout: 15000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).toBe(0);

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("Everything is already set up!");
    expect(output).toContain("ha-nova doctor");
    // Should NOT contain "Setup complete!" (no phases ran)
    expect(output).not.toContain("Setup complete!");
  });

  it("shows current status when resuming fully-set-up state", () => {

    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "test-relay-token-abc123",
    });
    const binDir = createMockBinaries();

    // Pre-install codex skill symlink
    const skillsDir = join(home, ".agents/skills");
    mkdirSync(skillsDir, { recursive: true });
    symlinkSync(join(REPO_ROOT, "skills"), join(skillsDir, "ha-nova"));

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "codex"],
      {
        cwd: REPO_ROOT,
        input: "",
        encoding: "utf8",
        timeout: 15000,
        env: mockEnv(home, binDir),
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("Relay reachable");
    expect(output).toContain("Authentication valid");
    expect(output).toContain("WebSocket connected");
    expect(output).toContain("Skills installed");
  });
});

describe.skipIf(!isMac)("S-3: resume after partial abort", () => {
  it("skips app install when config already exists", () => {

    // Config exists but no token → should skip app install phase
    // Token phase: auto-generates token → prompts for app config link → LLAT guide
    // Verify phase: prompts for HA host → relay URL → retries
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
    });
    const binDir = createMockBinaries();

    // Input sequence:
    // Phase 3 (token): generated token → [Enter] open app config → [Enter] saved token
    // Phase 3 (LLAT): [Enter] open HA profile → [Enter] done
    // Phase 4 (verify): host prompt (accept default) → relay URL (accept default)
    const input = [
      "",             // open app config
      "",             // saved relay token
      "",             // open HA profile
      "",             // LLAT done
      "",             // HA host (accept 192.168.1.5 default)
      "",             // relay URL (accept default)
    ].join("\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "codex"],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 30000,
        env: mockEnv(home, binDir),
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");

    // Should mention skipping completed steps
    expect(output).toContain("Skipping completed steps:");
    expect(output).toContain("app install");

    // Should complete
    expect(result.status).toBe(0);
  });

  it("skips to verify when config + token exist but relay unreachable", () => {

    // Config + token exist, relay unreachable → skip_app_install=1
    // But skip_relay_token=0 (relay not OK → might need token fix)
    // Token phase: existing token found → keep → skip app config
    // LLAT guide runs (WS not OK)
    // Verify: HA host probe fails → continue unverified → relay retries exhaust
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "existing-token-xyz",
    });
    const binDir = createMockBinaries({ curlFails: true });

    const input = [
      // Token phase: "Keep existing token?" [Y] → default
      "",
      // "Show full token?" [N] → default
      "",
      // LLAT guide: "open HA profile" → press enter
      "",
      // LLAT done
      "",
      // Verify: HA host prompt (accept default)
      "",
      // "Retry host entry?" [Y] → no
      "n",
      // "Continue with unverified host?" [N] → yes
      "y",
      // Relay URL prompt (accept default)
      "",
      // Retry 1: press enter
      "",
      // Retry 2: press enter
      "",
      // Retry 3: gives up, saves anyway
    ].join("\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "codex"],
      {
        cwd: REPO_ROOT,
        input,
        encoding: "utf8",
        timeout: 30000,
        env: mockEnv(home, binDir),
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");

    // Should skip app install
    expect(output).toContain("Skipping completed steps:");
    expect(output).toContain("app install");

    // Token kept
    expect(output).toContain("Using existing relay auth token from Keychain");

    // Should save config despite failures
    expect(output).toContain("Saving config anyway");
  });
});
