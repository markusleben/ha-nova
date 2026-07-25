import { execFileSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  type CloudGateFixture,
  cloudGateFixture,
  currentFixtureHead,
  currentFixtureTree,
  runCloudGate,
  validCloudEvidence,
} from "./cloud-release-gate-fixture.js";

const ACTION_SHA_123 = "1111111111111111111111111111111111111111";
const ACTION_SHA_124 = "2222222222222222222222222222222222222222";
const ACTION_SHA_130 = "3333333333333333333333333333333333333333";

function normalizationFixture() {
  const fixture = cloudGateFixture({
    min_relay_version: "0.8.0",
    cloud_remote_enabled: true,
    cloud_remote_platforms: ["linux"],
  });
  const workflowDir = join(fixture.root, ".github", "workflows");
  const policyDir = join(fixture.root, ".github", "policy");
  mkdirSync(workflowDir, { recursive: true });
  mkdirSync(policyDir, { recursive: true });
  copyFileSync(
    "scripts/release/verify-cloud-workflow-uses-only.mjs",
    join(fixture.root, "scripts", "release", "verify-cloud-workflow-uses-only.mjs"),
  );
  copyFileSync(
    ".github/policy/repo-policy.json",
    join(policyDir, "repo-policy.json"),
  );
  const binDir = join(fixture.root, "test-bin");
  mkdirSync(binDir);
  const gh = join(binDir, "gh");
  writeFileSync(
    gh,
    `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  *git/ref/tags/v1.2.3*) printf '%s\\n' '{"object":{"type":"commit","sha":"${ACTION_SHA_123}"}}' ;;
  *git/ref/tags/v1.2.4*) printf '%s\\n' '{"object":{"type":"commit","sha":"${ACTION_SHA_124}"}}' ;;
  *git/ref/tags/v1.3.0*) printf '%s\\n' '{"object":{"type":"commit","sha":"${ACTION_SHA_130}"}}' ;;
  *) exit 1 ;;
esac
`,
    "utf8",
  );
  chmodSync(gh, 0o755);
  writeFileSync(
    join(workflowDir, "maintenance.yml"),
    `steps:\n  - uses: example/action@${ACTION_SHA_123} # v1.2.3\n`,
    "utf8",
  );
  execFileSync("git", ["add", "."], { cwd: fixture.root });
  execFileSync("git", ["commit", "-qm", "trusted normalization gate"], {
    cwd: fixture.root,
  });
  return {
    fixture,
    workflow: join(workflowDir, "maintenance.yml"),
    evidence: {
      sha: currentFixtureHead(fixture),
      tree: currentFixtureTree(fixture),
    },
    env: {
      PATH: `${binDir}:${process.env.PATH ?? ""}`,
    },
  };
}

function commit(fixture: CloudGateFixture, message: string) {
  execFileSync("git", ["add", "."], { cwd: fixture.root });
  execFileSync("git", ["commit", "-qm", message], { cwd: fixture.root });
  return currentFixtureHead(fixture);
}

describe("Cloud evidence uses-only normalization", () => {
  it("accepts stale evidence for safe PR-merge and post-squash shapes", () => {
    const linear = normalizationFixture();
    writeFileSync(
      linear.workflow,
      `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.4\n`,
      "utf8",
    );
    const squashTarget = commit(linear.fixture, "squashed safe bump");
    const squash = runCloudGate(
      linear.fixture,
      validCloudEvidence(linear.evidence, ["linux"]),
      squashTarget,
      linear.env,
    );
    expect(squash.status, `${squash.stdout}\n${squash.stderr}`).toBe(0);

    const merged = normalizationFixture();
    execFileSync("git", ["checkout", "-qb", "safe-bump"], {
      cwd: merged.fixture.root,
    });
    writeFileSync(
      merged.workflow,
      `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.4\n`,
      "utf8",
    );
    commit(merged.fixture, "safe bump head");
    execFileSync("git", ["checkout", "-qb", "merge-target", merged.evidence.sha], {
      cwd: merged.fixture.root,
    });
    execFileSync("git", ["merge", "--no-ff", "-qm", "synthetic merge", "safe-bump"], {
      cwd: merged.fixture.root,
    });
    const mergeTarget = currentFixtureHead(merged.fixture);
    const merge = runCloudGate(
      merged.fixture,
      validCloudEvidence(merged.evidence, ["linux"]),
      mergeTarget,
      merged.env,
    );
    expect(merge.status, `${merge.stdout}\n${merge.stderr}`).toBe(0);
  });

  it("accepts chained safe bumps from the evidence ancestor", () => {
    const source = normalizationFixture();
    for (const [sha, version] of [
      [ACTION_SHA_124, "v1.2.4"],
      [ACTION_SHA_130, "v1.3.0"],
    ]) {
      writeFileSync(
        source.workflow,
        `steps:\n  - uses: example/action@${sha} # ${version}\n`,
        "utf8",
      );
      commit(source.fixture, `safe bump ${version}`);
    }
    const result = runCloudGate(
      source.fixture,
      validCloudEvidence(source.evidence, ["linux"]),
      currentFixtureHead(source.fixture),
      source.env,
    );
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it("rejects a safe bump combined with any product delta", () => {
    const source = normalizationFixture();
    writeFileSync(
      source.workflow,
      `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.4\n`,
      "utf8",
    );
    writeFileSync(join(source.fixture.root, "product.txt"), "delta\n", "utf8");
    commit(source.fixture, "mixed bump");
    const result = runCloudGate(
      source.fixture,
      validCloudEvidence(source.evidence, ["linux"]),
      currentFixtureHead(source.fixture),
      source.env,
    );
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it("rejects evidence from a non-ancestor commit", () => {
    const source = normalizationFixture();
    const base = source.evidence.sha;
    writeFileSync(join(source.fixture.root, "evidence.txt"), "proof\n", "utf8");
    const evidenceCommit = commit(source.fixture, "evidence branch");
    const evidenceTree = currentFixtureTree(source.fixture);
    execFileSync("git", ["checkout", "-q", base], { cwd: source.fixture.root });
    writeFileSync(
      source.workflow,
      `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.4\n`,
      "utf8",
    );
    commit(source.fixture, "unrelated safe bump");
    const result = runCloudGate(
      source.fixture,
      validCloudEvidence(
        { sha: evidenceCommit, tree: evidenceTree },
        ["linux"],
      ),
      currentFixtureHead(source.fixture),
      source.env,
    );
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it.each([
    [
      `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.3\n`,
      "a SHA that does not match its tag",
    ],
    [
      `steps:\n  - uses: example/action@${ACTION_SHA_124} # v2.0.0\n`,
      "a major update",
    ],
    ["steps:\n  - uses: example/action@main\n", "a mutable ref"],
  ])("rejects %s", (workflow) => {
    const source = normalizationFixture();
    writeFileSync(source.workflow, workflow, "utf8");
    commit(source.fixture, "unsafe action update");
    const result = runCloudGate(
      source.fixture,
      validCloudEvidence(source.evidence, ["linux"]),
      currentFixtureHead(source.fixture),
      source.env,
    );
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });
});
