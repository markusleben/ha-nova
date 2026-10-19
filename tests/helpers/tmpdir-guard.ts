/**
 * Vitest globalSetup: each run gets its own TMPDIR, inherited by every worker
 * and every child it spawns (bash, go, node). Teardown removes it and fails the
 * run if a test left anything there. A run killed before teardown leaves only
 * its root, which the next run sweeps once its pid is gone.
 */
import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
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
  // TEMP/TMP: os.tmpdir() reads those on Windows.
  process.env.TMPDIR = root;
  process.env.TEMP = root;
  process.env.TMP = root;

  return () => {
    const left = readdirSync(root);
    removeDir(root);
    if (left.length > 0) {
      throw new Error(
        `Tests left ${left.length} entries in TMPDIR (removed now; clean up in afterEach/afterAll, ` +
          `e.g. tests/helpers/temp-dir.ts): ${left.slice(0, 20).join(", ")}`,
      );
    }
  };
}
