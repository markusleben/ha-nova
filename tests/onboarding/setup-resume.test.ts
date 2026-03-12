/**
 * S-2: Upgrade (everything present) — "Already set up"
 * S-3: Resume after partial abort
 */
import { mkdirSync, readdirSync, readFileSync, symlinkSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "./_helpers.js";

const isMac = process.platform === "darwin";
const GEMINI_SUB_SKILLS = [
  "ha-nova-write",
  "ha-nova-read",
  "ha-nova-helper",
  "ha-nova-entity-discovery",
  "ha-nova-onboarding",
  "ha-nova-service-call",
  "ha-nova-review",
  "ha-nova-fallback",
];

function seedGeminiSkills(home: string, options: { includeReviewChecks?: boolean } = {}) {
  const geminiSkillsDir = join(home, ".gemini/skills");

  const copyRepoMarkdown = (skillName: string) => {
    const repoSkillDir = join(REPO_ROOT, "skills", skillName);
    const destDir = join(geminiSkillsDir, skillName);
    mkdirSync(destDir, { recursive: true });

    for (const file of readdirSync(repoSkillDir)) {
      if (!file.endsWith(".md")) {
        continue;
      }
      if (!(options.includeReviewChecks ?? true) && skillName === "ha-nova-review" && file === "checks.md") {
        continue;
      }
      writeFileSync(join(destDir, file), readFileSync(join(repoSkillDir, file), "utf8"), "utf8");
    }
  };

  copyRepoMarkdown("ha-nova");
  for (const sub of GEMINI_SUB_SKILLS) {
    copyRepoMarkdown(sub);
  }
}

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

  it("treats Gemini namespaced flat skills as fully installed", () => {
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "test-relay-token-abc123",
    });
    const binDir = createMockBinaries();

    seedGeminiSkills(home);

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "gemini"],
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
    expect(output).toContain("Skills installed");
  });

  it("does not treat Gemini as fully installed when review companions are missing", () => {
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "test-relay-token-abc123",
    });
    const binDir = createMockBinaries();

    seedGeminiSkills(home, { includeReviewChecks: false });

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "gemini"],
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
    expect(output).not.toContain("Everything is already set up!");
    expect(output).toContain("Installing HA NOVA skills");
    expect(output).toContain("Skills not installed");
  });

  it("treats setup all with namespaced skills as fully installed", () => {
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "test-relay-token-abc123",
    });
    const binDir = createMockBinaries();

    mkdirSync(join(home, ".agents/skills"), { recursive: true });
    symlinkSync(join(REPO_ROOT, "skills"), join(home, ".agents/skills/ha-nova"));

    seedGeminiSkills(home);

    mkdirSync(join(home, ".config/opencode/skills"), { recursive: true });
    symlinkSync(join(REPO_ROOT, "skills"), join(home, ".config/opencode/skills/ha-nova"));

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "all"],
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
    expect(output).toContain("Skills installed");
  });

  it("does not treat setup all as complete when Gemini companions are missing", () => {
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "test-relay-token-abc123",
    });
    const binDir = createMockBinaries();

    mkdirSync(join(home, ".agents/skills"), { recursive: true });
    symlinkSync(join(REPO_ROOT, "skills"), join(home, ".agents/skills/ha-nova"));
    seedGeminiSkills(home, { includeReviewChecks: false });
    mkdirSync(join(home, ".config/opencode/skills"), { recursive: true });
    symlinkSync(join(REPO_ROOT, "skills"), join(home, ".config/opencode/skills/ha-nova"));

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/macos-onboarding.sh", "setup", "all"],
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
    expect(output).not.toContain("Everything is already set up!");
    expect(output).toContain("Installing HA NOVA skills");
    expect(output).toContain("Skills not installed");
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
    expect(output).toContain("Already done:");
    expect(output).toContain("app installation");

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
      // Phase 1b: HA host prompt (accept default — config has it)
      // Token phase: "Keep existing token?" [Y] → default
      "",
      // "Copy token to clipboard?" [N] → default
      "",
      // LLAT guide: "open HA profile" → press enter
      "",
      // "open relay settings" → press enter
      "",
      // LLAT done / app running
      "",
      // Relay retry 1: URL prompt (accept default)
      "",
      // Relay retry 2: URL prompt (accept default)
      "",
      // Relay retry 3: gives up, saves anyway
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
    expect(output).toContain("Already done:");
    expect(output).toContain("app installation");

    // Token kept
    expect(output).toContain("Using existing relay auth token from Keychain");

    // Should save config despite failures
    expect(output).toContain("Saving your settings anyway");
  });

  it("retries degraded WS inside resume flow before finishing incomplete", { timeout: 30000 }, () => {
    const home = createMockHome({
      config: {
        HA_HOST: "192.168.1.5",
        HA_URL: "http://192.168.1.5:8123",
        RELAY_BASE_URL: "http://192.168.1.5:8791",
      },
      keychainToken: "existing-token-xyz",
    });
    const binDir = createMockBinaries({
      healthFixture: "relay-health-ws-down.json",
      wsFixture: "relay-ws-upstream-error.json",
      wsStatusCode: 502,
    });

    const input = [
      "",
      "",
      "",
      "",
      "",
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

    expect(result.status).toBe(0);

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(output).toContain("Already done:");
    expect(output).toContain("connection check");
    expect(output).toContain("Home Assistant WebSocket is not connected yet");
    expect(output).toContain("Quick checklist");
    expect(output).toContain("Setup incomplete");
    expect(output).not.toContain("Setup complete!");
  });
});
