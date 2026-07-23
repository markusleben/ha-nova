import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

import { afterEach, describe, expect, it } from "vitest";

const temporaryDirectories = new Set<string>();

afterEach(() => {
  for (const path of temporaryDirectories) {
    rmSync(path, { recursive: true, force: true });
  }
  temporaryDirectories.clear();
});

const environmentConfig = readFileSync(
  ".codex/environments/environment.toml",
  "utf8",
);
const setupMatch = environmentConfig.match(
  /\[setup\]\s+script = '''\n([\s\S]*?)\n'''/,
);
if (!setupMatch) {
  throw new Error("Codex setup script is missing from environment.toml");
}
const setupScript = setupMatch[1];

function setupFixture(): {
  root: string;
  sourceTree: string;
  worktree: string;
  fakeBin: string;
  callLog: string;
  failMarker: string;
} {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-worktree-bootstrap-"));
  temporaryDirectories.add(root);
  const sourceTree = join(root, "source");
  const worktree = join(root, "worktree");
  const fakeBin = join(root, "fake-bin");
  const callLog = join(root, "npm-calls.log");
  const failMarker = join(root, "nova-install-failed-once");
  mkdirSync(sourceTree);
  mkdirSync(join(worktree, "nova"), { recursive: true });
  mkdirSync(fakeBin);
  for (const manifest of [
    "package.json",
    "package-lock.json",
    "nova/package.json",
    "nova/package-lock.json",
  ]) {
    writeFileSync(join(worktree, manifest), "{}\n", "utf8");
  }
  writeFileSync(
    join(fakeBin, "npm"),
    `#!/bin/sh
printf '%s|%s\\n' "$PWD" "$*" >> "$CALL_LOG"
if [ "\${FAIL_NOVA_ONCE:-0}" = 1 ] &&
   [ "$*" = "--prefix nova ci" ] &&
   [ ! -e "$FAIL_MARKER" ]; then
  : > "$FAIL_MARKER"
  exit 42
fi
`,
    "utf8",
  );
  chmodSync(join(fakeBin, "npm"), 0o755);
  return { root, sourceTree, worktree, fakeBin, callLog, failMarker };
}

function runSetup(
  fixture: ReturnType<typeof setupFixture>,
  worktree = fixture.worktree,
  sourceTree = fixture.sourceTree,
  failNovaOnce = false,
): ReturnType<typeof spawnSync> {
  return spawnSync("bash", {
    input: setupScript,
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fixture.fakeBin}:${process.env.PATH ?? ""}`,
      CODEX_SOURCE_TREE_PATH: sourceTree,
      CODEX_WORKTREE_PATH: worktree,
      CALL_LOG: fixture.callLog,
      FAIL_MARKER: fixture.failMarker,
      FAIL_NOVA_ONCE: failNovaOnce ? "1" : "0",
    },
  });
}

describe("Codex worktree bootstrap", () => {
  it("uses Codex-managed ignored-file copying instead of custom copy logic", () => {
    expect(setupScript).toBeTypeOf("string");
    expect(setupScript).not.toContain("cp ");
    expect(
      readFileSync(".worktreeinclude", "utf8").trim().split("\n"),
    ).toEqual([".env", ".env.local"]);
  });

  it("rejects an invalid worktree before invoking npm", () => {
    const fixture = setupFixture();
    const missingSource = runSetup(
      fixture,
      fixture.worktree,
      join(fixture.root, "missing-source"),
    );
    const missing = runSetup(fixture, join(fixture.root, "missing"));
    const wrongDirectory = join(fixture.root, "wrong");
    mkdirSync(wrongDirectory);
    const wrong = runSetup(fixture, wrongDirectory);

    expect(missingSource.status).not.toBe(0);
    expect(missingSource.stderr).toContain(
      "Codex source tree is not a real directory",
    );
    expect(missing.status).not.toBe(0);
    expect(missing.stderr).toContain(
      "Codex worktree is not a real directory",
    );
    expect(wrong.status).not.toBe(0);
    expect(wrong.stderr).toContain("Codex worktree is missing package.json");
    expect(existsSync(fixture.callLog)).toBe(false);
  });

  it("rejects identical or symlinked roots before invoking npm", () => {
    const fixture = setupFixture();
    const same = runSetup(
      fixture,
      fixture.sourceTree,
      fixture.sourceTree,
    );
    const linkedWorktree = join(fixture.root, "linked-worktree");
    symlinkSync(fixture.worktree, linkedWorktree, "dir");
    const linked = runSetup(fixture, linkedWorktree);
    const linkedSource = join(fixture.root, "linked-source");
    symlinkSync(fixture.sourceTree, linkedSource, "dir");
    const sourceLinked = runSetup(
      fixture,
      fixture.worktree,
      linkedSource,
    );

    expect(same.status).not.toBe(0);
    expect(same.stderr).toContain(
      "Codex source tree and worktree must be different",
    );
    expect(linked.status).not.toBe(0);
    expect(linked.stderr).toContain(
      "Codex worktree is not a real directory",
    );
    expect(sourceLinked.status).not.toBe(0);
    expect(sourceLinked.stderr).toContain(
      "Codex source tree is not a real directory",
    );
    expect(existsSync(fixture.callLog)).toBe(false);
  });

  it("runs both lockfile installs in order", () => {
    const fixture = setupFixture();
    const result = runSetup(fixture);
    const worktreeRoot = realpathSync(fixture.worktree);

    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(readFileSync(fixture.callLog, "utf8").trim().split("\n")).toEqual([
      `${worktreeRoot}|ci`,
      `${worktreeRoot}|--prefix nova ci`,
    ]);
  });

  it("fails on a partial install and succeeds cleanly on rerun", () => {
    const fixture = setupFixture();
    const first = runSetup(
      fixture,
      fixture.worktree,
      fixture.sourceTree,
      true,
    );
    const second = runSetup(
      fixture,
      fixture.worktree,
      fixture.sourceTree,
      true,
    );
    const worktreeRoot = realpathSync(fixture.worktree);

    expect(first.status).toBe(42);
    expect(second.status, `${second.stdout}\n${second.stderr}`).toBe(0);
    expect(readFileSync(fixture.callLog, "utf8").trim().split("\n")).toEqual([
      `${worktreeRoot}|ci`,
      `${worktreeRoot}|--prefix nova ci`,
      `${worktreeRoot}|ci`,
      `${worktreeRoot}|--prefix nova ci`,
    ]);
  });
});
