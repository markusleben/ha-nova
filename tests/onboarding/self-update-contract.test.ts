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

    expect(content).toContain("find_runtime_binary");
    expect(content).toContain('exec "${runtime_bin}" update "$@"');
    expect(content).toContain('scripts/lib/runtime.sh');
    expect(content).toContain('exec_repo_dev_runtime update "$@"');
    expect(content).not.toContain("pull --ff-only");
    expect(content).not.toContain("claude plugin update");
  });

  it("version-check.sh delegates to ha-nova check-update --quiet", () => {
    const content = readFileSync(versionCheckScriptPath, "utf8");

    expect(content).toContain("find_runtime_binary");
    expect(content).toContain('exec "${runtime_bin}" check-update --quiet "$@"');
    expect(content).toContain('scripts/lib/runtime.sh');
    expect(content).toContain('exec_repo_dev_runtime check-update --quiet "$@"');
    expect(content).not.toContain("latest-version.json");
  });

  it("scripts/onboarding/bin/ha-nova delegates setup and update commands to the Go runtime", () => {
    const content = readFileSync(resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "utf8");

    expect(content).toContain("find_runtime_binary");
    expect(content).toContain("exec_runtime setup");
    expect(content).toContain('exec "${runtime_bin}" update "$@"');
    expect(content).toContain('exec "${runtime_bin}" check-update "$@"');
    expect(content).not.toContain("macos-setup.sh");
    expect(content).not.toContain("pull --ff-only");
  });

  it("uninstall.sh delegates to the Go runtime too", () => {
    const content = readFileSync(resolve(REPO_ROOT, "scripts/onboarding/uninstall.sh"), "utf8");

    expect(content).toContain("find_runtime_binary");
    expect(content).toContain('exec "${runtime_bin}" uninstall "$@"');
    expect(content).toContain('scripts/lib/runtime.sh');
    expect(content).toContain('exec_repo_dev_runtime uninstall "$@"');
    expect(content).not.toContain("ha-nova.exe");
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

  it("legacy shell shims ignore a stray .exe runtime on Unix and fall back to a repo-built dev runtime", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-shim-exe-home-"));
    const binDir = join(home, "bin");
    const localBinDir = join(home, ".local", "bin");
    const goMarker = join(home, "go-args.txt");
    const execMarker = join(home, "exec-args.txt");
    const fakeRuntime = join(home, "fake-runtime.sh");

    spawnSync("mkdir", ["-p", binDir, localBinDir], { encoding: "utf8" });
    writeFileSync(join(localBinDir, "ha-nova.exe"), "windows-binary", { mode: 0o644 });
    writeFileSync(
      fakeRuntime,
      `#!/usr/bin/env bash
printf '%s\n' "$@" > "${execMarker}"
`,
      { mode: 0o755 },
    );
    writeFileSync(
      join(binDir, "go"),
      `#!/usr/bin/env bash
if [[ "$1" == "env" ]]; then
  case "$2" in
    GOVERSION) printf 'go1.26.1\\n' ;;
    GOOS) printf 'darwin\\n' ;;
    GOARCH) printf 'arm64\\n' ;;
    CGO_ENABLED) printf '1\\n' ;;
    GOFLAGS) printf '\\n' ;;
    GOEXPERIMENT) printf '\\n' ;;
  esac
  exit 0
fi
if [[ "$1" == "build" ]]; then
  shift
      out=""
      while [[ $# -gt 0 ]]; do
        if [[ "$1" == "-o" ]]; then
          out="$2"
          shift 2
      continue
        fi
        shift
      done
      mkdir -p "$(dirname "$out")"
      cp "${fakeRuntime}" "$out"
      chmod +x "$out"
      printf 'build\n' > "${goMarker}"
      exit 0
fi
printf '%s\n' "$@" > "${goMarker}"
`,
      { mode: 0o755 },
    );

    const baseEnv = {
      ...process.env,
      HOME: home,
      PATH: `${binDir}:${process.env.PATH ?? ""}`,
      OSTYPE: "linux-gnu",
    };

    const updateResult = spawnSync("bash", [updateScriptPath, "--version", "1.2.3"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: baseEnv,
    });
    expect(updateResult.status).toBe(0);
    expect(readFileSync(goMarker, "utf8")).toContain("build");
    const updateArgs = readFileSync(execMarker, "utf8");
    expect(updateArgs).toContain("update");
    expect(updateArgs).toContain("--version");
    expect(updateArgs).toContain("1.2.3");

    const versionResult = spawnSync("bash", [versionCheckScriptPath], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: baseEnv,
    });
    expect(versionResult.status).toBe(0);
    const versionArgs = readFileSync(execMarker, "utf8");
    expect(versionArgs).toContain("check-update");
    expect(versionArgs).toContain("--quiet");

    const wrapperResult = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: baseEnv,
    });
    expect(wrapperResult.status).toBe(0);
    expect(readFileSync(execMarker, "utf8")).toContain("update");
  });

  it("repo-built dev runtime rebuilds when the go build context changes", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-shim-rebuild-home-"));
    const binDir = join(home, "bin");
    const localBinDir = join(home, ".local", "bin");
    const goMarker = join(home, "go-args.txt");
    const execMarker = join(home, "exec-args.txt");
    const fakeRuntime = join(home, "fake-runtime.sh");
    const buildCountFile = join(home, "build-count.txt");

    spawnSync("mkdir", ["-p", binDir, localBinDir], { encoding: "utf8" });
    writeFileSync(join(localBinDir, "ha-nova.exe"), "windows-binary", { mode: 0o644 });
    writeFileSync(
      fakeRuntime,
      `#!/usr/bin/env bash
printf '%s\n' "$@" > "${execMarker}"
`,
      { mode: 0o755 },
    );
    writeFileSync(
      join(binDir, "go"),
      `#!/usr/bin/env bash
if [[ "$1" == "env" ]]; then
  case "$2" in
    GOVERSION) printf 'go1.26.1\\n' ;;
    GOOS) printf 'darwin\\n' ;;
    GOARCH) printf 'arm64\\n' ;;
    CGO_ENABLED) printf '1\\n' ;;
    GOFLAGS) printf '%s\\n' "\${FAKE_GOFLAGS:-}" ;;
    GOEXPERIMENT) printf '\\n' ;;
  esac
  exit 0
fi
if [[ "$1" == "build" ]]; then
  count=0
  if [[ -f "${buildCountFile}" ]]; then
    count="$(cat "${buildCountFile}")"
  fi
  count="$((count + 1))"
  printf '%s\\n' "$count" > "${buildCountFile}"
  shift
  out=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "-o" ]]; then
      out="$2"
      shift 2
      continue
    fi
    shift
  done
  mkdir -p "$(dirname "$out")"
  cp "${fakeRuntime}" "$out"
  chmod +x "$out"
  printf 'build\\n' > "${goMarker}"
  exit 0
fi
printf '%s\\n' "$@" > "${goMarker}"
`,
      { mode: 0o755 },
    );

    const baseEnv = {
      ...process.env,
      HOME: home,
      PATH: `${binDir}:${process.env.PATH ?? ""}`,
      OSTYPE: "linux-gnu",
    };

    const firstResult = spawnSync("bash", [updateScriptPath], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...baseEnv, FAKE_GOFLAGS: "" },
    });
    expect(firstResult.status).toBe(0);
    expect(readFileSync(buildCountFile, "utf8").trim()).toBe("1");

    const secondResult = spawnSync("bash", [updateScriptPath], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...baseEnv, FAKE_GOFLAGS: "" },
    });
    expect(secondResult.status).toBe(0);
    expect(readFileSync(buildCountFile, "utf8").trim()).toBe("1");

    const thirdResult = spawnSync("bash", [updateScriptPath], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...baseEnv, FAKE_GOFLAGS: "-tags=devshim" },
    });
    expect(thirdResult.status).toBe(0);
    expect(readFileSync(buildCountFile, "utf8").trim()).toBe("2");
  });
});
