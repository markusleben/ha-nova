import { existsSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { REPO_ROOT } from "./_helpers.js";

describe("legacy shell shims", () => {
  it("scripts/onboarding/bin/ha-nova delegates setup and update commands to the Go runtime", () => {
    const content = readFileSync(resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "utf8");

    expect(content).toContain("find_runtime_binary");
    expect(content).toContain("exec_runtime setup");
    expect(content).toContain('exec "${runtime_bin}" update "$@"');
    expect(content).toContain('exec "${runtime_bin}" check-update "$@"');
    expect(content).not.toContain("macos-setup.sh");
    expect(content).not.toContain("pull --ff-only");
  });

  it("no longer keeps standalone tracked update/version-check shim files in the repo", () => {
    expect(existsSync(resolve(REPO_ROOT, "scripts/update.sh"))).toBe(false);
    expect(existsSync(resolve(REPO_ROOT, "scripts/version-check.sh"))).toBe(false);
  });

  it("runtime shim looks in native Windows install paths before falling back to repo dev runtime", () => {
    const runtime = readFileSync(resolve(REPO_ROOT, "scripts/lib/runtime.sh"), "utf8");

    expect(runtime).toContain("Programs/ha-nova/ha-nova.exe");
    expect(runtime).toContain("Microsoft/WinGet/Links/ha-nova.exe");
    expect(runtime).toContain("windows_localappdata_dir()");
    expect(runtime.indexOf("Programs/ha-nova/ha-nova.exe")).toBeLessThan(
      runtime.indexOf(".local/share/ha-nova/ha-nova.exe")
    );
  });

  it("prefers the native Windows runtime over a stale legacy .local runtime", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-shim-windows-home-"));
    const localLegacyDir = join(home, ".local", "share", "ha-nova");
    const localAppDataDir = join(home, "AppData", "Local", "Programs", "ha-nova");
    const marker = join(home, "windows-runtime-args.txt");

    spawnSync("mkdir", ["-p", localLegacyDir, localAppDataDir], { encoding: "utf8" });
    writeFileSync(join(localLegacyDir, "ha-nova.exe"), "legacy-runtime", { mode: 0o755 });
    writeFileSync(
      join(localAppDataDir, "ha-nova.exe"),
      `#!/usr/bin/env bash
printf '%s\n' "$@" > "${marker}"
`,
      { mode: 0o755 },
    );

    const result = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update", "--version", "1.2.3"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: {
        ...process.env,
        HOME: home,
        LOCALAPPDATA: join(home, "AppData", "Local"),
        OSTYPE: "msys",
      },
    });

    expect(result.status).toBe(0);
    const forwarded = readFileSync(marker, "utf8");
    expect(forwarded).toContain("update");
    expect(forwarded).toContain("--version");
    expect(forwarded).toContain("1.2.3");
  });

  it("does not keep relay shim compatibility in the main runtime anymore", () => {
    expect(existsSync(resolve(REPO_ROOT, "cli/compat_shims.go"))).toBe(false);
  });

  it("scripts/onboarding/bin/ha-nova forwards update arguments to the installed runtime", () => {
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

    const result = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update", "--version", "1.2.3"], {
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

  it("repo-dev wrappers ignore a stray .exe runtime on Unix and fall back to a repo-built dev runtime", { timeout: 120000 }, () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-shim-exe-home-"));
    const binDir = join(home, "bin");
    const localBinDir = join(home, ".local", "bin");
    const goMarker = join(home, "go-args.txt");
    const execMarker = join(home, "exec-args.txt");
    const devRootMarker = join(home, "dev-root.txt");
    const fakeRuntime = join(home, "fake-runtime.sh");
    const versionCheckWrapper = join(home, ".config", "ha-nova", "version-check");

    spawnSync("mkdir", ["-p", binDir, localBinDir], { encoding: "utf8" });
    mkdirSync(join(home, ".config", "ha-nova"), { recursive: true });
    writeFileSync(join(localBinDir, "ha-nova.exe"), "windows-binary", { mode: 0o644 });
    writeFileSync(
      fakeRuntime,
      `#!/usr/bin/env bash
printf '%s\n' "$HA_NOVA_DEV_ROOT" > "${devRootMarker}"
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
    writeFileSync(
      versionCheckWrapper,
      `#!/usr/bin/env bash
set -euo pipefail
exec "${resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova")}" check-update --quiet "$@"
`,
      { mode: 0o755 },
    );

    const baseEnv = {
      ...process.env,
      HOME: home,
      PATH: `${binDir}:${process.env.PATH ?? ""}`,
      OSTYPE: "linux-gnu",
    };

    const updateResult = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update", "--version", "1.2.3"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: baseEnv,
    });
    expect(updateResult.status).toBe(0);
    expect(readFileSync(goMarker, "utf8")).toContain("build");
    expect(readFileSync(devRootMarker, "utf8").trim()).toBe(REPO_ROOT);
    const updateArgs = readFileSync(execMarker, "utf8");
    expect(updateArgs).toContain("update");
    expect(updateArgs).toContain("--version");
    expect(updateArgs).toContain("1.2.3");

    const versionResult = spawnSync("bash", [versionCheckWrapper], {
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

  it("repo-built dev runtime rebuilds when the go build context changes", { timeout: 120000 }, () => {
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

    const firstResult = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...baseEnv, FAKE_GOFLAGS: "" },
    });
    expect(firstResult.status).toBe(0);
    expect(readFileSync(buildCountFile, "utf8").trim()).toBe("1");

    const secondResult = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...baseEnv, FAKE_GOFLAGS: "" },
    });
    expect(secondResult.status).toBe(0);
    expect(readFileSync(buildCountFile, "utf8").trim()).toBe("1");

    const thirdResult = spawnSync("bash", [resolve(REPO_ROOT, "scripts/onboarding/bin/ha-nova"), "update"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      env: { ...baseEnv, FAKE_GOFLAGS: "-tags=devshim" },
    });
    expect(thirdResult.status).toBe(0);
    expect(readFileSync(buildCountFile, "utf8").trim()).toBe("2");
  });
});
