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

const workflowPath = ".github/workflows/readme-release-gate.yml";
const otherWorkflowPath = ".github/workflows/other.yml";
const beforePath = "docs/work/<version>-release-body.md";
const afterPath = "docs/work/next-release-body.md";
const actionBefore = "1111111111111111111111111111111111111111";
const actionAfter = "2222222222222222222222222222222222222222";
const workflow = [
  `# Claims collect in ${beforePath}.`,
  "steps:",
  `  - run: echo 'Move the claim to ${beforePath}.'`,
  "",
].join("\n");

function fixture(sensitive = false) {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-workflow-handoff-"));
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
  writeFileSync(join(root, otherWorkflowPath), workflow, "utf8");
  writeFileSync(
    join(root, ".github", "workflows", "maintenance.yml"),
    `steps:\n  - uses: example/action@${actionBefore} # v1.2.3\n`,
    "utf8",
  );
  writeFileSync(
    join(root, ".github", "policy", "repo-policy.json"),
    JSON.stringify({
      cloud_source_gate: {
        sensitive_workflows: sensitive ? [workflowPath] : [],
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
  const fakeBin = mkdtempSync(join(tmpdir(), "ha-nova-workflow-handoff-gh-"));
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

function verify(
  mutation: (root: string) => void,
  sensitive = false,
): ReturnType<typeof spawnSync> {
  const { root, script, base, env } = fixture(sensitive);
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

describe("one-time README workflow maintenance handoff", () => {
  it("accepts only the complete two-token rewrite", () => {
    const result = verify((root) => {
      rewrite(root, workflowPath, workflow.replaceAll(beforePath, afterPath));
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it.each([
    ["partial", workflow.replace(beforePath, afterPath)],
    ["extra", `${workflow.replaceAll(beforePath, afterPath)}# extra\n`],
    ["different", workflow.replaceAll(beforePath, "docs/work/future.md")],
  ])("rejects a %s rewrite", (_name, changed) => {
    const result = verify((root) => rewrite(root, workflowPath, changed));
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it("rejects the same rewrite in another workflow", () => {
    const result = verify((root) => {
      rewrite(root, otherWorkflowPath, workflow.replaceAll(beforePath, afterPath));
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it("rejects an otherwise valid sibling uses bump", () => {
    const result = verify((root) => {
      rewrite(root, workflowPath, workflow.replaceAll(beforePath, afterPath));
      rewrite(
        root,
        ".github/workflows/maintenance.yml",
        `steps:\n  - uses: example/action@${actionAfter} # v1.2.4\n`,
      );
    });
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("must be the sole workflow delta");
  });

  it("does not override the sensitive-workflow denylist", () => {
    const result = verify((root) => {
      rewrite(root, workflowPath, workflow.replaceAll(beforePath, afterPath));
    }, true);
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("is Cloud-release-sensitive");
  });
});
