import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const workflowPath = ".github/workflows/cloud-candidate-bundle.yml";
const otherSensitivePath = ".github/workflows/ci.yml";
const maintenancePath = ".github/workflows/maintenance.yml";
const actionBefore = "1111111111111111111111111111111111111111";
const actionAfter = "2222222222222222222222222222222222222222";
const inertRunName =
  "run-name: Cloud candidate PR #${{ inputs.pull_request }} ${{ inputs.version_tag }} (${{ inputs.request_id }})";
const quotedRunName =
  'run-name: "Cloud candidate PR #${{ inputs.pull_request }} ${{ inputs.version_tag }} (${{ inputs.request_id }})"';
const workflow = [
  "name: Cloud Candidate Bundle",
  inertRunName,
  "on:",
  "  workflow_dispatch: {}",
  "",
].join("\n");
const otherWorkflow = ["name: CI", "on:", "  push: {}", ""].join("\n");

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-runname-handoff-"));
  const script = join(
    root,
    "scripts",
    "release",
    "verify-cloud-workflow-uses-only.mjs",
  );
  mkdirSync(join(root, "scripts", "release"), { recursive: true });
  mkdirSync(join(root, ".github", "policy"), { recursive: true });
  mkdirSync(join(root, ".github", "workflows"), { recursive: true });
  copyFileSync("scripts/release/verify-cloud-workflow-uses-only.mjs", script);
  writeFileSync(join(root, workflowPath), workflow, "utf8");
  writeFileSync(join(root, otherSensitivePath), otherWorkflow, "utf8");
  writeFileSync(
    join(root, maintenancePath),
    `steps:\n  - uses: example/action@${actionBefore} # v1.2.3\n`,
    "utf8",
  );
  // The production reality this suite must model: the candidate-bundle
  // workflow IS on the sensitive denylist, and the one-time exception is the
  // only thing that may carry its exact rewrite past it.
  writeFileSync(
    join(root, ".github", "policy", "repo-policy.json"),
    JSON.stringify({
      cloud_source_gate: {
        sensitive_workflows: [workflowPath, otherSensitivePath],
      },
    }),
    "utf8",
  );
  execFileSync("git", ["init", "-q", "-b", "main"], { cwd: root });
  execFileSync("git", ["config", "user.email", "test@example.com"], {
    cwd: root,
  });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: root });
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: root });
  const base = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: root,
    encoding: "utf8",
  }).trim();
  const fakeBin = mkdtempSync(join(tmpdir(), "ha-nova-runname-handoff-gh-"));
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
  return {
    root,
    script,
    base,
    env: { ...process.env, PATH: `${fakeBin}:${process.env.PATH ?? ""}` },
  };
}

function verify(mutation: (root: string) => void): ReturnType<typeof spawnSync> {
  const { root, script, base, env } = fixture();
  mutation(root);
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", "target"], { cwd: root });
  const target = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: root,
    encoding: "utf8",
  }).trim();
  return spawnSync(
    process.execPath,
    [script, root, base, target, "workflow-tree-only"],
    { cwd: root, encoding: "utf8", env },
  );
}

function rewrite(root: string, path: string, body: string): void {
  writeFileSync(join(root, path), body, "utf8");
}

describe("one-time candidate run-name quote fix handoff", () => {
  it("accepts exactly the quote fix on the sensitive candidate workflow", () => {
    const result = verify((root) => {
      rewrite(root, workflowPath, workflow.replace(inertRunName, quotedRunName));
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it.each([
    ["an extra trailing change", `${workflow.replace(inertRunName, quotedRunName)}# extra\n`],
    ["a different replacement", workflow.replace(inertRunName, "run-name: Something else")],
    ["an unrelated edit with the line kept inert", workflow.replace("workflow_dispatch: {}", "workflow_dispatch: {}\n  push: {}")],
  ])("rejects %s as Cloud-release-sensitive", (_name, changed) => {
    const result = verify((root) => rewrite(root, workflowPath, changed));
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("is Cloud-release-sensitive");
  });

  it("rejects the fix when a sensitive sibling changes too", () => {
    const result = verify((root) => {
      rewrite(root, workflowPath, workflow.replace(inertRunName, quotedRunName));
      rewrite(root, otherSensitivePath, `${otherWorkflow}# touched\n`);
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it("rejects the fix even when the sibling change is a valid uses bump", () => {
    // Without the sole-delta guard this combination would pass: the candidate
    // file rides the exception and the maintenance bump passes verifyUsesOnly.
    const result = verify((root) => {
      rewrite(root, workflowPath, workflow.replace(inertRunName, quotedRunName));
      rewrite(
        root,
        maintenancePath,
        `steps:\n  - uses: example/action@${actionAfter} # v1.2.4\n`,
      );
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("must be the sole workflow delta");
  });

  it("rejects the same rewrite smuggled into another workflow", () => {
    const result = verify((root) => {
      rewrite(root, otherSensitivePath, `${otherWorkflow}${quotedRunName}\n`);
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("is Cloud-release-sensitive");
  });
});
