import { existsSync, lstatSync, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  addWindowsMocks,
  createMockBinaries,
  mockEnv,
  REPO_ROOT,
} from "./_helpers.js";

const WINDOWS_INSTALL_TIMEOUT_MS = 180000;

describe("Windows dev installer contract", () => {
  it("installs file-based clients with copy fallback on Windows and prefers a bundled relay.exe", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-windows-install-"));
    const binDir = createMockBinaries();
    addWindowsMocks(binDir, home);
    const bundledRelay = join(home, "bundle-relay.exe");
    writeFileSync(bundledRelay, "bundled relay exe\n", { mode: 0o755 });
    const env = mockEnv(home, binDir, {
      HA_NOVA_PLATFORM_OVERRIDE: "windows",
      HA_NOVA_FORCE_COPY_INSTALL: "1",
      HA_NOVA_BUNDLED_RELAY: bundledRelay,
    });

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "all"],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: WINDOWS_INSTALL_TIMEOUT_MS, env },
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
  }, WINDOWS_INSTALL_TIMEOUT_MS);
});
