import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

// Round 1 of the trust-boundary two-round path (precedent #578/#579): the
// verifier carries a blob-pinned one-time exception for exactly seven
// workflow rewrites, one of them (ci.yml) Cloud-release-sensitive. This suite
// models that reality with a sensitive and a non-sensitive path and leaves
// with round 2 together with the exception itself.
const scriptPath = "scripts/release/verify-cloud-workflow-uses-only.mjs";
const sensitivePath = ".github/workflows/ci.yml";
const plainPath = ".github/workflows/codeql.yml";
const maintenancePath = ".github/workflows/maintenance.yml";
const actionBefore = "1111111111111111111111111111111111111111";
const actionAfter = "2222222222222222222222222222222222222222";
const sensitiveBefore = "name: CI\non:\n  push: {}\nsteps:\n  - uses: actions/checkout@v7\n";
const sensitiveAfter = `name: CI\non:\n  push: {}\nsteps:\n  - uses: actions/checkout@${actionBefore} # v1.2.3\n`;
const plainBefore = "name: CodeQL\nlanguages: javascript-typescript\n";
const plainAfter = "name: CodeQL\nlanguages: go, javascript-typescript\n";
const maintenanceBefore = `steps:\n  - uses: example/action@${actionBefore} # v1.2.3\n`;
const maintenanceAfter = `steps:\n  - uses: example/action@${actionAfter} # v1.2.4\n`;
const productionPaths = [
  ".github/workflows/ci.yml",
  ".github/workflows/codeql.yml",
  ".github/workflows/dependency-review.yml",
  ".github/workflows/pairing-e2e.yml",
  ".github/workflows/pr-review-watchdog.yml",
  ".github/workflows/relay-image.yml",
  ".github/workflows/release-pipeline-audit.yml",
];

function blob(content: string): string {
  return execFileSync("git", ["hash-object", "--stdin"], {
    input: content,
    encoding: "utf8",
  }).trim();
}

function pinnedScript(): string {
  const source = readFileSync(scriptPath, "utf8");
  const fixtureMap = [
    "const oneTimeTrustBoundaryRewrite = new Map([",
    `  [${JSON.stringify(sensitivePath)}, { before: "${blob(sensitiveBefore)}", after: "${blob(sensitiveAfter)}" }],`,
    `  [${JSON.stringify(plainPath)}, { before: "${blob(plainBefore)}", after: "${blob(plainAfter)}" }],`,
    "]);",
  ].join("\n");
  const replaced = source.replace(
    /const oneTimeTrustBoundaryRewrite = new Map\(\[[\s\S]*?\n\]\);/,
    fixtureMap,
  );
  expect(replaced).not.toBe(source);
  return replaced;
}

function fixture(): { root: string; script: string; base: string; env: NodeJS.ProcessEnv } {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-trust-boundary-handoff-"));
  const script = join(root, "scripts", "release", "verify-cloud-workflow-uses-only.mjs");
  mkdirSync(join(root, "scripts", "release"), { recursive: true });
  mkdirSync(join(root, ".github", "policy"), { recursive: true });
  mkdirSync(join(root, ".github", "workflows"), { recursive: true });
  writeFileSync(script, pinnedScript(), "utf8");
  writeFileSync(join(root, sensitivePath), sensitiveBefore, "utf8");
  writeFileSync(join(root, plainPath), plainBefore, "utf8");
  writeFileSync(join(root, maintenancePath), maintenanceBefore, "utf8");
  writeFileSync(
    join(root, ".github", "policy", "repo-policy.json"),
    JSON.stringify({ cloud_source_gate: { sensitive_workflows: [sensitivePath] } }),
    "utf8",
  );
  execFileSync("git", ["init", "-q", "-b", "main"], { cwd: root });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: root });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: root });
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: root });
  const base = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  const fakeBin = mkdtempSync(join(tmpdir(), "ha-nova-trust-boundary-handoff-gh-"));
  const gh = join(fakeBin, "gh");
  writeFileSync(
    gh,
    `#!/usr/bin/env bash
case "$*" in
  *git/ref/tags/v1.2.3*) printf '%s\\n' '{"object":{"type":"commit","sha":"${actionBefore}"}}' ;;
  *git/ref/tags/v1.2.4*) printf '%s\\n' '{"object":{"type":"commit","sha":"${actionAfter}"}}' ;;
  *) exit 1 ;;
esac
`,
    "utf8",
  );
  chmodSync(gh, 0o755);
  return { root, script, base, env: { ...process.env, PATH: `${fakeBin}:${process.env.PATH ?? ""}` } };
}

function commit(root: string, message: string): string {
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", message], { cwd: root });
  return execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
}

type Mode = "workflow-tree-only" | "full-tree" | "single-sensitive-workflow";

function verify(
  mutation: (root: string) => void,
  mode: Mode = "single-sensitive-workflow",
  prepareBase?: (root: string) => void,
): ReturnType<typeof spawnSync> {
  const f = fixture();
  let base = f.base;
  if (prepareBase) {
    prepareBase(f.root);
    base = commit(f.root, "prepared base");
  }
  mutation(f.root);
  const target = commit(f.root, "target");
  return spawnSync(process.execPath, [f.script, f.root, base, target, mode], {
    cwd: f.root,
    encoding: "utf8",
    env: f.env,
  });
}

function rewrite(root: string, path: string, body: string): void {
  writeFileSync(join(root, path), body, "utf8");
}

function applyRewrite(root: string): void {
  rewrite(root, sensitivePath, sensitiveAfter);
  rewrite(root, plainPath, plainAfter);
}

describe("one-time trust-boundary rewrite handoff", () => {
  it("accepts exactly the pinned rewrite on the single-sensitive-workflow lane", () => {
    const result = verify(applyRewrite);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(result.stdout).toContain("one-time approved trust-boundary rewrite");
  });

  it.each(["workflow-tree-only", "full-tree"] as const)(
    "keeps the %s lane closed to the rewrite (uses-only carry and stale evidence)",
    (mode) => {
      const result = verify(applyRewrite, mode);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      expect(result.stderr).toContain("is Cloud-release-sensitive");
    },
  );

  it("rejects a substituted path even when the change count matches", () => {
    const result = verify((root) => {
      rewrite(root, sensitivePath, sensitiveAfter);
      rewrite(root, maintenancePath, maintenanceAfter);
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("exactly one existing sensitive workflow");
  });

  it("rejects a partial rewrite that leaves a pinned path unchanged", () => {
    const result = verify((root) => rewrite(root, plainPath, plainAfter));
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("exactly one existing sensitive workflow");
  });

  it("rejects a pinned path whose after-image differs by one byte", () => {
    const result = verify((root) => {
      rewrite(root, sensitivePath, `${sensitiveAfter}\n`);
      rewrite(root, plainPath, plainAfter);
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("exactly one existing sensitive workflow");
  });

  it("rejects the pinned set plus a valid uses bump in a third workflow", () => {
    // Without set equality the extra file would ride verifyUsesOnly while the
    // pinned pair rides the exception.
    const result = verify((root) => {
      applyRewrite(root);
      rewrite(root, maintenancePath, maintenanceAfter);
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("exactly one existing sensitive workflow");
  });

  it("stays inert after the rewrite: the reverse direction is denied", () => {
    const result = verify(
      (root) => {
        rewrite(root, sensitivePath, sensitiveBefore);
        rewrite(root, plainPath, plainBefore);
      },
      "single-sensitive-workflow",
      applyRewrite,
    );
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("exactly one existing sensitive workflow");
  });

  it("pins the production before-blobs to the current workflow tree", () => {
    // Tripwire: while the exception is alive, any other change to one of the
    // seven workflows moves the base blob and this assertion goes red on main.
    const source = readFileSync(scriptPath, "utf8");
    const map = /const oneTimeTrustBoundaryRewrite = new Map\(\[([\s\S]*?)\n\]\);/.exec(source);
    expect(map).not.toBeNull();
    const entries = [...(map?.[1] ?? "").matchAll(
      /\["([^"]+)", \{ before: "([0-9a-f]{40})", after: "([0-9a-f]{40})" \}\]/g,
    )];
    expect(entries.map((entry) => entry[1])).toEqual(productionPaths);
    for (const [, path, before, after] of entries) {
      const current = execFileSync("git", ["rev-parse", `HEAD:${path}`], { encoding: "utf8" }).trim();
      expect(before, `${path} base blob must match HEAD`).toBe(current);
      expect(after).not.toBe(before);
    }
  });
});
