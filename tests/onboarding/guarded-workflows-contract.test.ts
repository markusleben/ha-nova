import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const policy = JSON.parse(
  readFileSync(".github/policy/repo-policy.json", "utf8"),
) as {
  guarded_workflows: string[];
  main_branch_protection: { required_status_checks: string[] };
};

// Workflow files that deliver each required status check on main. A required
// check whose workflow is disabled never appears, so PRs hang BLOCKED forever.
const requiredCheckWorkflows: Record<string, string> = {
  "ci-gate": ".github/workflows/ci.yml",
  analyze: ".github/workflows/codeql.yml",
  "dependency-review": ".github/workflows/dependency-review.yml",
  "manifest-review-gate": ".github/workflows/manifest-review-gate.yml",
  "readme-release-gate": ".github/workflows/readme-release-gate.yml",
  "cloud-source-gate": ".github/workflows/cloud-source-gate.yml",
};

function jobSection(workflowYaml: string, jobId: string): string {
  const match = workflowYaml.match(
    new RegExp(`^  ${jobId}:\\n([\\s\\S]*?)(?=^  [a-z0-9-]+:|(?![\\s\\S]))`, "m"),
  );
  expect(match, `job ${jobId} not found`).not.toBeNull();
  return match![0];
}

function runGate(ghScript: string): { status: number | null; output: string } {
  const dir = mkdtempSync(join(tmpdir(), "guarded-wf-"));
  const gh = join(dir, "gh");
  writeFileSync(gh, `#!/usr/bin/env bash\n${ghScript}\n`);
  chmodSync(gh, 0o755);
  const result = spawnSync(
    "bash",
    ["scripts/release/verify-required-workflows-active.sh", "example/repo"],
    {
      encoding: "utf8",
      env: { ...process.env, PATH: `${dir}:${process.env.PATH}` },
    },
  );
  return { status: result.status, output: `${result.stdout}${result.stderr}` };
}

describe("guarded workflows contract", () => {
  it("guards every workflow that delivers a required status check", () => {
    for (const check of policy.main_branch_protection.required_status_checks) {
      const workflow = requiredCheckWorkflows[check];
      expect(workflow, `unmapped required check ${check}`).toBeDefined();
      expect(policy.guarded_workflows).toContain(workflow);
      expect(readFileSync(workflow!, "utf8"),
        `${workflow} no longer mentions its required check ${check}`,
      ).toContain(check);
    }
  });

  it("guards the Dependabot safe lane and the release path", () => {
    for (const workflow of [
      ".github/workflows/dependabot-safe-lane-prepare.yml",
      ".github/workflows/dependabot-safe-auto-merge.yml",
      ".github/workflows/release.yml",
      ".github/workflows/release-candidate.yml",
    ]) {
      expect(policy.guarded_workflows).toContain(workflow);
    }
  });

  it("lists only workflow files that exist", () => {
    expect(policy.guarded_workflows.length).toBeGreaterThan(0);
    for (const workflow of policy.guarded_workflows) {
      expect(workflow).toMatch(/^\.github\/workflows\/[a-z0-9-]+\.yml$/);
      expect(existsSync(workflow), `${workflow} does not exist`).toBe(true);
    }
  });

  it("runs the active-state gate as an uncommented step of the required ci-gate job", () => {
    const ciGate = jobSection(
      readFileSync(".github/workflows/ci.yml", "utf8"),
      "ci-gate",
    );
    const gateLine = ciGate
      .split("\n")
      .find((line) =>
        line.includes("bash scripts/release/verify-required-workflows-active.sh"),
      );
    expect(gateLine, "gate step missing from ci-gate job").toBeDefined();
    expect(gateLine!.trimStart().startsWith("#")).toBe(false);
    expect(ciGate).toContain("actions: read");
  });

  it("runs the active-state gate inside the release preflight", () => {
    expect(readFileSync("scripts/release/verify-release-pipeline.sh", "utf8")).toContain(
      "verify-required-workflows-active.sh",
    );
  });

  it("fails when a guarded workflow is disabled", () => {
    const { status, output } = runGate('echo "disabled_manually"');
    expect(status).toBe(1);
    expect(output).toContain("guarded workflow is not active");
    expect(output).toContain("gh workflow enable");
  });

  it("passes when every guarded workflow is active", () => {
    const { status, output } = runGate('echo "active"');
    expect(status).toBe(0);
    expect(output).toContain("guarded workflows are active");
  });

  it("fails loudly when the policy lists no guarded workflows", () => {
    const dir = mkdtempSync(join(tmpdir(), "guarded-wf-policy-"));
    const emptyPolicy = join(dir, "policy.json");
    writeFileSync(emptyPolicy, "{}");
    const result = spawnSync(
      "bash",
      [
        "scripts/release/verify-required-workflows-active.sh",
        "example/repo",
        emptyPolicy,
      ],
      { encoding: "utf8" },
    );
    expect(result.status).toBe(1);
    expect(`${result.stdout}${result.stderr}`).toContain("guarded_workflows");
  });

  it("forbids agents from disabling workflows in AGENTS.md", () => {
    const agents = readFileSync("AGENTS.md", "utf8");
    expect(agents).toContain("NEVER run `gh workflow disable`");
  });
});
