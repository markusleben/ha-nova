import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

// .github/scripts/changed-areas.sh decides which CI jobs a pull request may skip.
// Every doubt must select every job; only an error may stop it.
const script = resolve(".github/scripts/changed-areas.sh");
const jobAreas = JSON.stringify({
  "go-test": ["go", "relay", "skills", "docs"],
  "go-build": ["go"],
  "ha-integration": ["integration"],
});
const dirs: string[] = [];

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

function repoWith(files: string[]): string {
  const dir = mkdtempSync(join(tmpdir(), "ha-nova-changed-areas-"));
  dirs.push(dir);
  const git = (...args: string[]) => execFileSync("git", args, { cwd: dir, stdio: "pipe" });
  git("init", "-q");
  git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base");
  for (const file of files) {
    mkdirSync(join(dir, dirname(file)), { recursive: true });
    writeFileSync(join(dir, file), "x");
  }
  git("add", "-A");
  git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "change");
  return dir;
}

function run(cwd: string, args: string[], areas = jobAreas) {
  const result = spawnSync("bash", [script, ...args], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, JOB_AREAS: areas, GITHUB_OUTPUT: "" },
  });
  const runLine = result.stdout.split("\n").find((line) => line.startsWith("run="));
  return { status: result.status, run: runLine ? JSON.parse(runLine.slice(4)) : undefined };
}

const every = { "go-test": true, "go-build": true, "ha-integration": true };

describe("CI path scoping", () => {
  it.each([
    ["skills only", ["skills/ha-nova/SKILL.md", "tests/skills/a.test.ts"], { "go-test": true, "go-build": false, "ha-integration": false }],
    ["docs only", ["docs/work/a.md", "README.md"], { "go-test": true, "go-build": false, "ha-integration": false }],
    ["integration only", ["nova/integration/ha_nova/x.py", "tests/ha_integration/t.py"], { "go-test": false, "go-build": false, "ha-integration": true }],
    ["Go", ["cli/main.go"], { "go-test": true, "go-build": true, "ha-integration": false }],
    ["a workflow", ["skills/a.md", ".github/workflows/ci.yml"], every],
    ["an unknown directory", ["skills/a.md", "scripts/x.sh"], every],
    ["an unknown root file", ["version.json"], every],
    ["nothing", [], every],
  ])("selects jobs for %s", (_name, files, expected) => {
    expect(run(repoWith(files), ["HEAD^", "HEAD"])).toEqual({ status: 0, run: expected });
  });

  it("runs every job with --all", () => {
    expect(run(repoWith([]), ["--all"])).toEqual({ status: 0, run: every });
  });

  it("fails on a bad revision or a bad job map instead of skipping", () => {
    const dir = repoWith(["skills/a.md"]);
    expect(run(dir, ["0000000", "HEAD"]).status).not.toBe(0);
    expect(run(dir, []).status).not.toBe(0);
    expect(run(dir, ["--all"], JSON.stringify({ x: ["gox"] })).status).not.toBe(0);
    expect(run(dir, ["--all"], JSON.stringify({ x: [] })).status).not.toBe(0);
    expect(run(dir, ["--all"], "").status).not.toBe(0);
  });
});
