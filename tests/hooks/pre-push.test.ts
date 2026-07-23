import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync, spawnSync } from "node:child_process";

import { afterEach, describe, expect, it } from "vitest";

const temporaryDirectories = new Set<string>();

afterEach(() => {
  for (const path of temporaryDirectories) {
    rmSync(path, { recursive: true, force: true });
  }
  temporaryDirectories.clear();
});

function writeExecutable(path: string, body: string): void {
  writeFileSync(path, body, "utf8");
  chmodSync(path, 0o755);
}

function hookFixture(): {
  root: string;
  hook: string;
  fakeBin: string;
  callLog: string;
  localGitVariables: string[];
} {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-pre-push-"));
  temporaryDirectories.add(root);
  const fakeBin = join(root, "fake-bin");
  const hook = join(root, "pre-push");
  const callLog = join(root, "calls.log");
  const localGitVariables = execFileSync(
    "git",
    ["rev-parse", "--local-env-vars"],
    { encoding: "utf8" },
  ).trim().split(/\s+/);
  mkdirSync(fakeBin);
  copyFileSync(".husky/pre-push", hook);
  chmodSync(hook, 0o755);

  writeExecutable(
    join(fakeBin, "git"),
    `#!/bin/sh
[ "\${FAKE_GIT_FAIL:-0}" = 0 ] || exit 42
[ "$#" -eq 2 ] &&
  [ "$1" = "rev-parse" ] &&
  [ "$2" = "--local-env-vars" ] || exit 43
printf '%s\\n' '${localGitVariables.join("\n")}'
`,
  );
  const logger = `#!/bin/sh
for name in $LOCAL_GIT_VARS; do
  eval "is_set=\\\${$name+x}"
  if [ "$is_set" = x ]; then
    printf 'leaked %s\\n' "$name" >&2
    exit 90
  fi
done
printf '%s\\n' "$0" >> "$CALL_LOG"
`;
  writeExecutable(join(fakeBin, "npm"), logger);
  writeExecutable(join(fakeBin, "bash"), logger);

  return { root, hook, fakeBin, callLog, localGitVariables };
}

function runHook(
  fixture: ReturnType<typeof hookFixture>,
  gitFails = false,
): ReturnType<typeof spawnSync> {
  const inheritedGitEnv = Object.fromEntries(
    fixture.localGitVariables.map((name) => [name, "contaminated"]),
  );
  return spawnSync("sh", [fixture.hook], {
    cwd: fixture.root,
    encoding: "utf8",
    env: {
      ...process.env,
      ...inheritedGitEnv,
      PATH: `${fixture.fakeBin}:${process.env.PATH ?? ""}`,
      CALL_LOG: fixture.callLog,
      LOCAL_GIT_VARS: fixture.localGitVariables.join(" "),
      FAKE_GIT_FAIL: gitFails ? "1" : "0",
    },
  });
}

describe("pre-push hook isolation", () => {
  it("clears every repository-local Git variable before child commands", () => {
    const fixture = hookFixture();
    expect(fixture.localGitVariables.length).toBeGreaterThan(10);
    const result = runHook(fixture);

    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    const calls = readFileSync(fixture.callLog, "utf8").trim().split("\n");
    expect(calls).toHaveLength(4);
  });

  it("fails closed when Git cannot enumerate its local variables", () => {
    const fixture = hookFixture();
    const result = runHook(fixture, true);

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "[pre-push] FAILED: cannot isolate Git hook environment",
    );
    expect(existsSync(fixture.callLog)).toBe(false);
  });
});
