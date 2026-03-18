import { existsSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { REPO_ROOT } from "./_helpers.js";

const updateScriptPath = resolve(REPO_ROOT, "scripts/update.sh");
const versionCheckScriptPath = resolve(REPO_ROOT, "scripts/version-check.sh");

describe("legacy shell shims", () => {
  it("update.sh delegates to the Go runtime instead of implementing product logic itself", () => {
    const content = readFileSync(updateScriptPath, "utf8");

    expect(content).toContain("find_runtime_binary()");
    expect(content).toContain('exec "${runtime_bin}" update "$@"');
    expect(content).toContain('exec go run "${REPO_ROOT}/cli" update "$@"');
    expect(content).not.toContain("pull --ff-only");
    expect(content).not.toContain("claude plugin update");
  });

  it("version-check.sh delegates to ha-nova check-update --quiet", () => {
    const content = readFileSync(versionCheckScriptPath, "utf8");

    expect(content).toContain("find_runtime_binary()");
    expect(content).toContain('exec "${runtime_bin}" check-update --quiet "$@"');
    expect(content).toContain('exec go run "${REPO_ROOT}/cli" check-update --quiet "$@"');
    expect(content).not.toContain("latest-version.json");
  });

  it("scripts/onboarding/bin/ha-nova delegates setup and update commands to the Go runtime", () => {
    const content = readFileSync(resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "utf8");

    expect(content).toContain("find_runtime_binary()");
    expect(content).toContain("exec_runtime setup");
    expect(content).toContain('exec "${runtime_bin}" update "$@"');
    expect(content).toContain('exec "${runtime_bin}" check-update "$@"');
    expect(content).not.toContain("macos-setup.sh");
    expect(content).not.toContain("pull --ff-only");
  });

  it("uninstall.sh delegates to the Go runtime too", () => {
    const content = readFileSync(resolve(REPO_ROOT, "scripts/onboarding/uninstall.sh"), "utf8");

    expect(content).toContain("find_runtime_binary()");
    expect(content).toContain('exec "${runtime_bin}" uninstall "$@"');
    expect(content).toContain('exec go run "${REPO_ROOT}/cli" uninstall "$@"');
  });

  it("does not keep relay shim compatibility in the main runtime anymore", () => {
    expect(existsSync(resolve(REPO_ROOT, "cli/compat_shims.go"))).toBe(false);
  });

  it("update.sh forwards arguments to the installed runtime", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-shim-home-"));
    const binDir = join(home, ".local", "bin");
    const publicBinary = join(binDir, "ha-nova");
    const marker = join(home, "update-args.txt");

    spawnSync("mkdir", ["-p", binDir], { encoding: "utf8" });
    writeFileSync(
      publicBinary,
      `#!/usr/bin/env bash
printf '%s\n' "$@" > "${marker}"
`,
      { mode: 0o755 },
    );

    const result = spawnSync("bash", [updateScriptPath, "--version", "1.2.3"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...process.env, HOME: home },
    });

    expect(result.status).toBe(0);
    const forwarded = readFileSync(marker, "utf8");
    expect(forwarded).toContain("update");
    expect(forwarded).toContain("--version");
    expect(forwarded).toContain("1.2.3");
  });
});
