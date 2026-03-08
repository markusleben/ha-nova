/**
 * Contract tests for scripts/update.sh — the self-update script.
 * Verifies client detection, modular architecture, and safety properties.
 */
import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { REPO_ROOT } from "./_helpers.js";

const updateScript = readFileSync(
  resolve(REPO_ROOT, "scripts/update.sh"),
  "utf8",
);

describe("self-update script contract", () => {
  it("script exists and is executable", () => {
    const path = resolve(REPO_ROOT, "scripts/update.sh");
    expect(existsSync(path)).toBe(true);
  });

  describe("client detection", () => {
    it("detects all 4 supported clients", () => {
      // Each client has a distinct filesystem marker
      expect(updateScript).toContain("ha-nova@ha-nova");        // Claude Code
      expect(updateScript).toContain(".agents/skills/ha-nova");  // Codex
      expect(updateScript).toContain("opencode/skills/ha-nova"); // OpenCode
      expect(updateScript).toContain("ha-nova-read/SKILL.md");   // Gemini
    });

    it("populates DETECTED_CLIENTS array", () => {
      expect(updateScript).toContain("DETECTED_CLIENTS+=(");
      expect(updateScript).toMatch(/DETECTED_CLIENTS\+=\("claude"\)/);
      expect(updateScript).toMatch(/DETECTED_CLIENTS\+=\("codex"\)/);
      expect(updateScript).toMatch(/DETECTED_CLIENTS\+=\("opencode"\)/);
      expect(updateScript).toMatch(/DETECTED_CLIENTS\+=\("gemini"\)/);
    });
  });

  describe("three update archetypes", () => {
    it("uses native claude plugin update for Claude Code", () => {
      expect(updateScript).toContain("claude plugin update ha-nova@ha-nova");
    });

    it("uses symlink verification for Codex and OpenCode", () => {
      expect(updateScript).toContain("update_symlink_client");
      expect(updateScript).toMatch(/update_symlink_client.*Codex/);
      expect(updateScript).toMatch(/update_symlink_client.*OpenCode/);
    });

    it("uses flat-copy re-creation for Gemini", () => {
      expect(updateScript).toContain("update_gemini");
      // Gemini needs path resolution for docs/reference/
      expect(updateScript).toContain("docs/reference/");
    });
  });

  describe("safety", () => {
    it("uses git pull --ff-only, not reset --hard", () => {
      expect(updateScript).toContain("pull --ff-only");
      expect(updateScript).not.toContain("reset --hard");
    });

    it("skips git pull when only Claude Code is installed", () => {
      // Logic: needs_pull stays false if all clients are "claude"
      expect(updateScript).toContain('!= "claude"');
      expect(updateScript).toContain("needs_pull");
    });

    it("clears update cache after update", () => {
      expect(updateScript).toContain("latest-version.json");
    });
  });

  describe("shared tools", () => {
    it("updates relay CLI, update script, and version-check", () => {
      expect(updateScript).toContain("relay.sh");
      expect(updateScript).toContain("update.sh");
      expect(updateScript).toContain("version-check.sh");
      expect(updateScript).toContain("version.json");
    });
  });

  describe("extensibility", () => {
    it("has per-client case statement for easy addition", () => {
      // main() dispatches via case statement — new client = new case branch
      expect(updateScript).toMatch(/case.*\$client.*in/);
      expect(updateScript).toContain("claude)");
      expect(updateScript).toContain("codex)");
      expect(updateScript).toContain("opencode)");
      expect(updateScript).toContain("gemini)");
    });
  });

  describe("install script deploys update", () => {
    it("install-local-skills.sh copies update.sh to config dir", () => {
      const installScript = readFileSync(
        resolve(REPO_ROOT, "scripts/onboarding/install-local-skills.sh"),
        "utf8",
      );
      expect(installScript).toContain("update.sh");
      expect(installScript).toContain("ha-nova/update");
    });

    it("uninstall.sh cleans up update script", () => {
      const uninstallScript = readFileSync(
        resolve(REPO_ROOT, "scripts/onboarding/uninstall.sh"),
        "utf8",
      );
      expect(uninstallScript).toContain("update");
    });
  });
});
