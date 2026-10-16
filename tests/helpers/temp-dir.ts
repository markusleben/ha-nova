/**
 * Temp dirs that remove themselves: every dir made here is deleted after the
 * importing test file finishes. Cleanup is per file (afterAll), not per test,
 * so dirs created in beforeAll or at module level stay valid for every test.
 */
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterAll } from "vitest";

const created: string[] = [];

afterAll(() => {
  for (const dir of created.splice(0)) {
    try {
      rmSync(dir, { recursive: true, force: true });
    } catch {
      // Tests may leave read-only dirs (permission cases, Go module files).
      execFileSync("chmod", ["-R", "u+w", dir]);
      rmSync(dir, { recursive: true, force: true });
    }
  }
});

export function makeTempDir(prefix: string): string {
  const dir = mkdtempSync(join(tmpdir(), prefix));
  created.push(dir);
  return dir;
}
