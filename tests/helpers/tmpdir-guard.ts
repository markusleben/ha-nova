/**
 * Vitest globalSetup: each run gets its own TMPDIR, inherited by every worker
 * and every child it spawns (bash, go, node). Teardown removes it and fails the
 * run if a test left anything there. A run killed before teardown leaves only
 * its root, which the next run sweeps once its pid is gone.
 *
 * Each run also gets its own empty HOME, so no test reaches the developer's
 * real one (a test fake once overwrote a real Claude Code plugin registry).
 * Tests that write under HOME pass their own temp one (mockEnv in
 * tests/onboarding/_helpers.ts, makeTempDir in tests/helpers/temp-dir.ts);
 * teardown fails the run if anything lands in this shared one. Go and npm
 * keep their real caches.
 */
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

const PREFIX = "ha-nova-vitest-";

function removeDir(dir: string): void {
  try {
    rmSync(dir, { recursive: true, force: true });
  } catch {
    // Go module caches and permission tests leave read-only entries.
    execFileSync("chmod", ["-R", "u+w", dir]);
    rmSync(dir, { recursive: true, force: true });
  }
}

function isolateHome(home: string): void {
  const go = spawnSync("go", ["env", "GOMODCACHE", "GOCACHE", "GOPATH", "GOENV"], { encoding: "utf8" });
  const goValues = go.status === 0 ? go.stdout.trim().split("\n") : [];
  ["GOMODCACHE", "GOCACHE", "GOPATH", "GOENV"].forEach((key, i) => {
    if (!process.env[key] && goValues[i]) process.env[key] = goValues[i];
  });
  process.env.npm_config_cache ??= join(homedir(), ".npm");
  process.env.HOME = home;
  if (process.platform === "win32") {
    process.env.USERPROFILE = home;
    process.env.APPDATA = join(home, "AppData", "Roaming");
    process.env.LOCALAPPDATA = join(home, "AppData", "Local");
  }
  for (const key of ["XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME"]) {
    delete process.env[key];
  }
}

function pidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code === "EPERM";
  }
}

export default function setup(): () => void {
  const base = tmpdir();
  for (const name of readdirSync(base)) {
    const pid = new RegExp(`^${PREFIX}(\\d+)-`).exec(name)?.[1];
    if (pid === undefined || pidAlive(Number(pid))) continue;
    try {
      removeDir(join(base, name));
    } catch {
      // Another run swept it first; a stale root must not block this run.
    }
  }

  const root = mkdtempSync(join(base, `${PREFIX}${process.pid}-`));
  const home = mkdtempSync(join(base, `${PREFIX}${process.pid}-home-`));
  isolateHome(home);
  // TEMP/TMP: os.tmpdir() reads those on Windows.
  process.env.TMPDIR = root;
  process.env.TEMP = root;
  process.env.TMP = root;

  return () => {
    const homeLeft = readdirSync(home, { recursive: true }).map(String);
    removeDir(home);
    const left = readdirSync(root);
    removeDir(root);
    if (homeLeft.length > 0) {
      throw new Error(
        `Tests wrote ${homeLeft.length} entries under the shared HOME (removed now; give each test its own temp HOME, ` +
          `e.g. mockEnv in tests/onboarding/_helpers.ts): ${homeLeft.slice(0, 20).join(", ")}`,
      );
    }
    if (left.length > 0) {
      throw new Error(
        `Tests left ${left.length} entries in TMPDIR (removed now; clean up in afterEach/afterAll, ` +
          `e.g. tests/helpers/temp-dir.ts): ${left.slice(0, 20).join(", ")}`,
      );
    }
  };
}
