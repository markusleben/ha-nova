/**
 * S-6: SessionStart hook (4 variants)
 * Tests the hooks/session-start script output in various configurations.
 */
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, createMockHome, mockEnv, REPO_ROOT } from "../onboarding/_helpers.js";

describe("S-6: session-start hook", () => {
  it("outputs valid JSON with skill content", () => {
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
      env: { ...process.env },
    });

    expect(result.status).toBe(0);

    const output = result.stdout.trim();
    const json = JSON.parse(output);

    expect(json).toHaveProperty("additional_context");
    expect(json).toHaveProperty("hookSpecificOutput");
    expect(json.additional_context).toContain("HA NOVA Skills");
    expect(json.additional_context).toContain("name: ha-nova");
  });

  it("includes sub-skill discovery list", () => {
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
    });

    const json = JSON.parse(result.stdout.trim());
    expect(json.additional_context).toContain("ha-nova:read");
    expect(json.additional_context).toContain("ha-nova:write");
    expect(json.additional_context).toContain("ha-nova:review");
    expect(json.additional_context).toContain("ha-nova:entity-discovery");
    expect(json.additional_context).toContain("ha-nova:service-call");
    expect(json.additional_context).toContain("ha-nova:onboarding");
  });

  it("includes version from version.json", () => {
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
    });

    const json = JSON.parse(result.stdout.trim());
    expect(json.additional_context).toContain(`HA NOVA Skills v${versionJson.skill_version}`);
  });

  it("warns when relay version is outdated (via relay CLI)", () => {
    // This test verifies the hook checks relay version.
    // The hook sources the relay CLI which calls /health.
    // We verify the hook *script* contains the version comparison logic.
    const hookContent = readFileSync("hooks/session-start", "utf8");

    expect(hookContent).toContain("semver_lt");
    expect(hookContent).toContain("min_relay_version");
    expect(hookContent).toContain("WARNING:");
    expect(hookContent).toContain("Relay version");
  });

  it("contains remote update check with stale-while-revalidate", () => {
    const hookContent = readFileSync("hooks/session-start", "utf8");

    expect(hookContent).toContain("latest-version.json");
    expect(hookContent).toContain("UPDATE AVAILABLE");
    expect(hookContent).toContain("ha-nova update");
  });

  it("does not leak secrets in JSON output", () => {
    const result = spawnSync("bash", ["hooks/session-start"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 15000,
    });

    const output = result.stdout;
    expect(output).not.toContain("RELAY_AUTH_TOKEN");
    expect(output).not.toContain("HA_LLAT");
    expect(output).not.toContain("Bearer");
  });
});
