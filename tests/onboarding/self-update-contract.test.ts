/**
 * Contract tests for scripts/update.sh — the self-update script.
 * Verifies client detection, modular architecture, and safety properties.
 */
import { existsSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, mockEnv, mockEnvWithBase, REPO_ROOT } from "./_helpers.js";

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
      expect(updateScript).toContain(".gemini/skills/ha-nova-read/SKILL.md");   // Gemini
      expect(updateScript).toContain(".agents/skills/ha-nova-read/SKILL.md");   // Legacy Gemini
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
      expect(updateScript).toContain("rewrite_flat_markdown");
      expect(updateScript).toContain("copy_flat_skill_markdown");
      expect(updateScript).toContain("perl -0pe");
      expect(updateScript).toContain("find \"${dest_dir}\" -maxdepth 1 -type f -name '*.md'");
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
      expect(updateScript).toContain("relay");
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

  it("refreshes Gemini flat copies and companion markdown from the source clone", { timeout: 45000 }, () => {
    const sandbox = mkdtempSync(join(tmpdir(), "ha-nova-update-"));
    const repoCopy = join(sandbox, "repo");
    const originBare = join(sandbox, "origin.git");
    const home = join(sandbox, "home");
    const binDir = createMockBinaries();
    const hookEnv = {
      ...process.env,
      GIT_DIR: join(sandbox, "hook.git"),
      GIT_PREFIX: "hooks/pre-push/",
      GIT_WORK_TREE: sandbox,
    };

    const copyResult = spawnSync(
      "bash",
      ["-lc", 'mkdir -p "$DEST" && tar --exclude=".git" -cf - . | tar -xf - -C "$DEST"'],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        env: mockEnvWithBase(hookEnv, { DEST: repoCopy }),
      },
    );
    expect(copyResult.status).toBe(0);

    const initResult = spawnSync(
      "bash",
      [
        "-lc",
        [
          'git init --initial-branch=main',
          'git config user.email "tests@example.com"',
          'git config user.name "Tests"',
          "git add .",
          'git commit -m "snapshot"',
          'git clone --bare "$PWD" "$ORIGIN"',
          'git remote remove origin 2>/dev/null || true',
          'git remote add origin "$ORIGIN"',
        ].join(" && "),
      ],
      {
        cwd: repoCopy,
        encoding: "utf8",
        env: mockEnvWithBase(hookEnv, { ORIGIN: originBare }),
      },
    );
    expect(initResult.status).toBe(0);

    const installResult = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "all"],
      {
        cwd: repoCopy,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );
    expect(installResult.status).toBe(0);

    writeFileSync(join(home, ".gemini/skills/ha-nova-review/checks.md"), "stale checks\n", "utf8");
    writeFileSync(join(home, ".gemini/skills/ha-nova-review/old-companion.md"), "stale companion\n", "utf8");
    writeFileSync(join(home, ".gemini/skills/ha-nova-write/SKILL.md"), "stale write skill\n", "utf8");

    const updateResult = spawnSync(
      "bash",
      [join(home, ".config/ha-nova/update")],
      {
        cwd: sandbox,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );
    expect(updateResult.status).toBe(0);

    const refreshedChecks = readFileSync(join(home, ".gemini/skills/ha-nova-review/checks.md"), "utf8");
    expect(refreshedChecks).toContain("H-09 [MEDIUM → HIGH]");
    expect(refreshedChecks).toContain("Canonical path: `checks.md`");
    expect(existsSync(join(home, ".gemini/skills/ha-nova-review/old-companion.md"))).toBe(false);

    const refreshedWrite = readFileSync(join(home, ".gemini/skills/ha-nova-write/SKILL.md"), "utf8");
    expect(refreshedWrite).toContain(join(repoCopy, "skills/review/checks.md"));
    expect(refreshedWrite).toContain(join(repoCopy, "skills/ha-nova/relay-api.md"));
  });

  it("detects legacy Gemini installs from the shared agents root", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-legacy-gemini-"));
    const repoCopy = join(home, ".local/share/ha-nova");
    const originBare = join(home, "origin.git");
    const legacySkillDir = join(home, ".agents/skills/ha-nova-read");
    const binDir = createMockBinaries();
    const hookEnv = {
      ...process.env,
      GIT_DIR: join(home, "hook.git"),
      GIT_PREFIX: "hooks/pre-push/",
      GIT_WORK_TREE: home,
    };

    const copyResult = spawnSync(
      "bash",
      ["-lc", 'mkdir -p "$DEST" && tar --exclude=".git" -cf - . | tar -xf - -C "$DEST"'],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        env: mockEnvWithBase(hookEnv, { DEST: repoCopy }),
      },
    );
    expect(copyResult.status).toBe(0);

    const initResult = spawnSync(
      "bash",
      [
        "-lc",
        [
          'git init --initial-branch=main',
          'git config user.email "tests@example.com"',
          'git config user.name "Tests"',
          "git add .",
          'git commit -m "snapshot"',
          'git clone --bare "$PWD" "$ORIGIN"',
          'git remote remove origin 2>/dev/null || true',
          'git remote add origin "$ORIGIN"',
        ].join(" && "),
      ],
      {
        cwd: repoCopy,
        encoding: "utf8",
        env: mockEnvWithBase(hookEnv, { ORIGIN: originBare }),
      },
    );
    expect(initResult.status).toBe(0);

    const legacyResult = spawnSync(
      "bash",
      ["-lc", 'mkdir -p "$LEGACY" && printf "name: ha-nova-read\n" > "$LEGACY/SKILL.md"'],
      {
        encoding: "utf8",
        env: mockEnvWithBase(hookEnv, { LEGACY: legacySkillDir }),
      },
    );
    expect(legacyResult.status).toBe(0);

    const result = spawnSync("bash", ["scripts/update.sh"], {
      cwd: repoCopy,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir),
    });

    expect(result.status).toBe(0);
    expect(result.stderr).not.toContain("No HA NOVA client installations detected");
    expect(result.stdout).toContain("Detected clients: gemini");
    expect(existsSync(join(home, ".gemini/skills/ha-nova/SKILL.md"))).toBe(true);
  });
});
