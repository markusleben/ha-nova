import { existsSync, lstatSync, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  addWindowsMocks,
  createMockBinaries,
  createMockHome,
  mockEnvForPlatform,
  REPO_ROOT,
} from "./_helpers.js";

describe("platform dispatch", () => {
  it("loads the macOS platform module by default", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();
    const env = mockEnvForPlatform("macos", home, binDir);

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/macos-lib.sh" && printf '%s' "$HA_NOVA_PLATFORM_ID"`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );

    expect(result.status).toBe(0);
    expect(result.stdout).toBe("macos");
  });

  it("loads the Windows platform module when overridden for tests", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();
    addWindowsMocks(binDir, home);
    const env = mockEnvForPlatform("windows", home, binDir);

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/macos-lib.sh" && require_platform && printf '%s' "$HA_NOVA_PLATFORM_ID"`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );

    expect(result.status).toBe(0);
    expect(result.stdout).toBe("windows");
  });

  it("round-trips Windows secure storage through the platform module", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();
    addWindowsMocks(binDir, home);
    const env = mockEnvForPlatform("windows", home, binDir);

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/macos-lib.sh"
         store_platform_secret "ha-nova.relay-auth-token" "my-secret-token-123"
         read_platform_secret "ha-nova.relay-auth-token"`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );

    expect(result.status).toBe(0);
    expect(result.stdout.trim()).toBe("my-secret-token-123");
  });

  it("installs file-based clients with copy fallback on Windows and prefers a bundled relay.exe", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-windows-install-"));
    const binDir = createMockBinaries();
    addWindowsMocks(binDir, home);
    const bundledRelay = join(home, "bundle-relay.exe");
    writeFileSync(bundledRelay, "bundled relay exe\n", { mode: 0o755 });
    const env = mockEnvForPlatform("windows", home, binDir, {
      HA_NOVA_BUNDLED_RELAY: bundledRelay,
    });

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "all"],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 20000, env },
    );

    expect(result.status).toBe(0);

    const codexPath = join(home, ".agents/skills/ha-nova");
    const opencodePath = join(home, ".config/opencode/skills/ha-nova");
    expect(lstatSync(codexPath).isSymbolicLink()).toBe(false);
    expect(lstatSync(opencodePath).isSymbolicLink()).toBe(false);
    expect(existsSync(join(codexPath, "ha-nova", "SKILL.md"))).toBe(true);
    expect(existsSync(join(opencodePath, "ha-nova", "SKILL.md"))).toBe(true);

    const relayExe = join(home, ".config/ha-nova/relay.exe");
    const relayShim = join(home, ".config/ha-nova/relay");
    expect(statSync(relayExe).isFile()).toBe(true);
    expect(readFileSync(relayExe, "utf8")).toBe("bundled relay exe\n");
    expect(readFileSync(relayShim, "utf8")).toContain("relay.exe");
  });
});
