import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  renameSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";

import { describe, expect, it } from "vitest";

import {
  type CloudGateFixture,
  cloudGateFixture,
  currentFixtureHead,
  runCloudGate,
  validCloudEvidence,
} from "./cloud-release-gate-fixture.js";

const ACTION_SHA_123 = "1111111111111111111111111111111111111111";
const ACTION_SHA_124 = "2222222222222222222222222222222222222222";

function commitFixtureChanges(fixture: CloudGateFixture, files: Record<string, string>): string {
  for (const [relativePath, body] of Object.entries(files)) {
    const target = join(fixture.root, relativePath);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, body, "utf8");
  }
  execFileSync("git", ["add", "."], { cwd: fixture.root });
  execFileSync("git", ["commit", "-qm", "fixture delta"], {
    cwd: fixture.root,
  });
  return execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: fixture.root,
    encoding: "utf8",
  }).trim();
}

function preparePRGateFixture(
  cloudEnabled = false,
  strictProtection = true,
  sourceGateProvisioned = true,
) {
  const fixture = cloudGateFixture({
    min_relay_version: "0.8.0",
    cloud_remote_enabled: cloudEnabled,
    cloud_remote_platforms: cloudEnabled ? ["linux"] : [],
  });
  const workflowDir = join(fixture.root, ".github", "workflows");
  const policyDir = join(fixture.root, ".github", "policy");
  const releaseDir = join(fixture.root, "scripts", "release");
  mkdirSync(workflowDir, { recursive: true });
  mkdirSync(policyDir, { recursive: true });
  copyFileSync(
    ".github/workflows/cloud-source-gate.yml",
    join(workflowDir, "cloud-source-gate.yml"),
  );
  writeFileSync(
    join(workflowDir, "maintenance.yml"),
    `steps:\n  - uses: example/action@${ACTION_SHA_123} # v1.2.3\n`,
    "utf8",
  );
  copyFileSync(".github/policy/repo-policy.json", join(policyDir, "repo-policy.json"));
  const policyPath = join(policyDir, "repo-policy.json");
  const policy = JSON.parse(readFileSync(policyPath, "utf8"));
  const source = policy.cloud_source_gate.check_name;
  policy.main_branch_protection.strict_required_status_checks =
    strictProtection;
  if (sourceGateProvisioned) {
    policy.cloud_source_gate.reporter_app_id = 42;
    policy.main_branch_protection.required_status_checks = [
      ...new Set([
        ...policy.main_branch_protection.required_status_checks,
        source,
      ]),
    ];
    policy.main_branch_protection.required_status_check_apps[source] = 42;
  } else {
    policy.cloud_source_gate.reporter_app_id = 0;
    policy.main_branch_protection.required_status_checks =
      policy.main_branch_protection.required_status_checks.filter(
        (name: string) => name !== source,
      );
    delete policy.main_branch_protection.required_status_check_apps[source];
  }
  writeFileSync(policyPath, `${JSON.stringify(policy, null, 2)}\n`, "utf8");
  const prGate = join(releaseDir, "verify-cloud-pr-source-gate.sh");
  const usesOnlyGate = join(releaseDir, "verify-cloud-workflow-uses-only.mjs");
  const targetGate = join(releaseDir, "verify-cloud-target-source-gate.sh");
  copyFileSync("scripts/release/verify-cloud-pr-source-gate.sh", prGate);
  copyFileSync("scripts/release/verify-cloud-target-source-gate.sh", targetGate);
  copyFileSync("scripts/release/verify-cloud-workflow-uses-only.mjs", usesOnlyGate);
  chmodSync(prGate, 0o755);
  chmodSync(targetGate, 0o755);
  execFileSync("git", ["add", "."], { cwd: fixture.root });
  execFileSync("git", ["commit", "-qm", "trusted gate"], {
    cwd: fixture.root,
  });
  const trustedHead = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: fixture.root,
    encoding: "utf8",
  }).trim();
  execFileSync("git", ["remote", "add", "origin", fixture.root], {
    cwd: fixture.root,
  });
  const fakeBin = mkdtempSync(join(tmpdir(), "ha-nova-cloud-action-gh-"));
  const gh = join(fakeBin, "gh");
  writeFileSync(
    gh,
    `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  *git/ref/tags/v1.2.3*) printf '%s\\n' '{"object":{"type":"commit","sha":"${ACTION_SHA_123}"}}' ;;
  *git/ref/tags/v1.2.4*) printf '%s\\n' '{"object":{"type":"commit","sha":"${ACTION_SHA_124}"}}' ;;
  *) exit 1 ;;
esac
`,
    "utf8",
  );
  chmodSync(gh, 0o755);
  return {
    fixture,
    prGate,
    targetGate,
    trustedHead,
    workflowDir,
    env: { PATH: `${fakeBin}:${process.env.PATH ?? ""}` },
  };
}

function runPRGate(
  root: string,
  prGate: string,
  target: string,
  evidence?: Record<string, unknown>,
  env: NodeJS.ProcessEnv = {},
) {
  execFileSync("git", ["checkout", "-q", "HEAD"], { cwd: root });
  execFileSync("git", ["update-ref", "refs/pull/1/merge", target], {
    cwd: root,
  });
  return spawnSync("bash", [prGate], {
    cwd: root,
    encoding: "utf8",
    env: {
      ...process.env,
      HA_NOVA_CLOUD_GATE_PR_NUMBER: "1",
      ...(evidence === undefined
        ? {}
        : { HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: JSON.stringify(evidence) }),
      ...env,
    },
  });
}

export function registerCloudReleaseCommitGateBehaviorTests(): void {
  describe("Home Assistant Cloud full-tree evidence behavior", () => {
    it("permits only trusted candidate metadata to remain evidence-pending", () => {
      const { fixture, targetGate, trustedHead } =
        preparePRGateFixture(true);
      execFileSync(
        "git",
        ["update-ref", "refs/pull/1/merge", trustedHead],
        { cwd: fixture.root },
      );
      const env = {
        ...process.env,
        HA_NOVA_CLOUD_GATE_SOURCE_REF: "refs/pull/1/merge",
        HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT: trustedHead,
      };
      const candidate = spawnSync("bash", [targetGate, "candidate"], {
        cwd: fixture.root,
        encoding: "utf8",
        env,
      });
      expect(
        candidate.status,
        `${candidate.stdout}\n${candidate.stderr}`,
      ).toBe(0);

      const release = spawnSync("bash", [targetGate], {
        cwd: fixture.root,
        encoding: "utf8",
        env,
      });
      expect(release.status).not.toBe(0);
      expect(`${release.stdout}\n${release.stderr}`).toContain(
        "HA_NOVA_CLOUD_GATE_EVIDENCE_JSON is required",
      );
    });

    it("binds a pull request merge commit to the expected base and head", () => {
      const { fixture, prGate, trustedHead } = preparePRGateFixture();
      execFileSync("git", ["checkout", "-qb", "feature"], {
        cwd: fixture.root,
      });
      writeFileSync(join(fixture.root, "feature.txt"), "change\n", "utf8");
      const featureHead = commitFixtureChanges(fixture, {});
      execFileSync("git", ["checkout", "-qb", "merge-target", trustedHead], {
        cwd: fixture.root,
      });
      execFileSync("git", ["merge", "--no-ff", "-qm", "synthetic merge", featureHead], {
        cwd: fixture.root,
      });
      const mergeCommit = currentFixtureHead(fixture);
      execFileSync("git", ["checkout", "-q", trustedHead], {
        cwd: fixture.root,
      });

      const exact = runPRGate(fixture.root, prGate, mergeCommit, undefined, {
        HA_NOVA_CLOUD_GATE_EXPECTED_BASE_COMMIT: trustedHead,
        HA_NOVA_CLOUD_GATE_EXPECTED_HEAD_COMMIT: featureHead,
      });
      expect(exact.status, `${exact.stdout}\n${exact.stderr}`).toBe(0);

      const mismatch = runPRGate(fixture.root, prGate, mergeCommit, undefined, {
        HA_NOVA_CLOUD_GATE_EXPECTED_BASE_COMMIT: trustedHead,
        HA_NOVA_CLOUD_GATE_EXPECTED_HEAD_COMMIT: "0000000000000000000000000000000000000000",
      });
      expect(mismatch.status, `${mismatch.stdout}\n${mismatch.stderr}`).not.toBe(0);
    });

    it("allows disabled workflow maintenance but rejects unprovisioned activation", () => {
      const { fixture, prGate, trustedHead, workflowDir, env } =
        preparePRGateFixture(false, true, false);
      const clean = runPRGate(fixture.root, prGate, trustedHead);
      expect(clean.status, `${clean.stdout}\n${clean.stderr}`).toBe(0);

      writeFileSync(
        join(workflowDir, "spoof.yml"),
        "jobs:\n  spoof:\n    name: cloud-source-gate\n",
        "utf8",
      );
      execFileSync("git", ["add", "."], { cwd: fixture.root });
      execFileSync("git", ["commit", "-qm", "duplicate context"], {
        cwd: fixture.root,
      });
      const target = execFileSync("git", ["rev-parse", "HEAD"], {
        cwd: fixture.root,
        encoding: "utf8",
      }).trim();
      execFileSync("git", ["checkout", "-q", trustedHead], {
        cwd: fixture.root,
      });
      const duplicate = runPRGate(fixture.root, prGate, target);
      expect(duplicate.status, `${duplicate.stdout}\n${duplicate.stderr}`).toBe(0);

      const enabled = {
        min_relay_version: "0.8.0",
        cloud_remote_enabled: true,
        cloud_remote_platforms: ["linux"],
      };
      writeFileSync(
        join(fixture.root, "version.json"),
        `${JSON.stringify(enabled, null, 2)}\n`,
        "utf8",
      );
      writeFileSync(
        join(fixture.root, "nova", "version.json"),
        `${JSON.stringify(enabled, null, 2)}\n`,
        "utf8",
      );
      writeFileSync(
        join(workflowDir, "maintenance.yml"),
        `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.4\n`,
        "utf8",
      );
      execFileSync("git", ["add", "."], { cwd: fixture.root });
      execFileSync("git", ["commit", "-qm", "enabled workflow change"], {
        cwd: fixture.root,
      });
      const enabledTarget = execFileSync("git", ["rev-parse", "HEAD"], {
        cwd: fixture.root,
        encoding: "utf8",
      }).trim();
      const enabledTree = execFileSync("git", ["rev-parse", "HEAD^{tree}"], {
        cwd: fixture.root,
        encoding: "utf8",
      }).trim();
      execFileSync("git", ["checkout", "-q", trustedHead], {
        cwd: fixture.root,
      });
      const enabledChange = runPRGate(
        fixture.root,
        prGate,
        enabledTarget,
        validCloudEvidence({ sha: enabledTarget, tree: enabledTree }, ["linux"]),
        env,
      );
      expect(enabledChange.status, `${enabledChange.stdout}\n${enabledChange.stderr}`).not.toBe(0);
      expect(`${enabledChange.stdout}\n${enabledChange.stderr}`).toContain(
        "enabled Cloud source requires the provisioned, exact App-bound source reporter check",
      );
    });

    it("accepts ancestor evidence for a safe enabled PR merge", () => {
      const { fixture, prGate, trustedHead, workflowDir, env } = preparePRGateFixture(true);
      const trustedTree = execFileSync("git", ["rev-parse", `${trustedHead}^{tree}`], {
        cwd: fixture.root,
        encoding: "utf8",
      }).trim();
      writeFileSync(
        join(workflowDir, "maintenance.yml"),
        `steps:\n  - uses: example/action@${ACTION_SHA_124} # v1.2.4\n`,
        "utf8",
      );
      const target = commitFixtureChanges(fixture, {});
      execFileSync("git", ["checkout", "-q", trustedHead], {
        cwd: fixture.root,
      });
      const result = runPRGate(
        fixture.root,
        prGate,
        target,
        validCloudEvidence({ sha: trustedHead, tree: trustedTree }, ["linux"]),
        env,
      );
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    });

    it.each(["add", "delete", "rename", "mode", "sensitive", "run", "identity", "comment"])(
      "rejects enabled workflow %s deltas outside existing non-sensitive uses versions",
      (mutation) => {
        const { fixture, prGate, trustedHead, workflowDir } = preparePRGateFixture();
        const enabled = {
          min_relay_version: "0.8.0",
          cloud_remote_enabled: true,
          cloud_remote_platforms: ["linux"],
        };
        writeFileSync(
          join(fixture.root, "version.json"),
          `${JSON.stringify(enabled, null, 2)}\n`,
          "utf8",
        );
        writeFileSync(
          join(fixture.root, "nova", "version.json"),
          `${JSON.stringify(enabled, null, 2)}\n`,
          "utf8",
        );
        const maintenance = join(workflowDir, "maintenance.yml");
        if (mutation === "add") {
          writeFileSync(
            join(workflowDir, "added.yml"),
            "steps:\n  - uses: actions/checkout@v8\n",
            "utf8",
          );
        } else if (mutation === "delete") {
          unlinkSync(maintenance);
        } else if (mutation === "rename") {
          renameSync(maintenance, join(workflowDir, "renamed.yml"));
        } else if (mutation === "mode") {
          chmodSync(maintenance, 0o755);
        } else if (mutation === "sensitive") {
          writeFileSync(
            join(workflowDir, "cloud-source-gate.yml"),
            "steps:\n  - uses: actions/checkout@v8\n",
            "utf8",
          );
        } else if (mutation === "run") {
          writeFileSync(maintenance, "steps:\n  - run: echo unsafe\n", "utf8");
        } else if (mutation === "comment") {
          writeFileSync(
            maintenance,
            "steps:\n  - uses: actions/checkout@v7 # comment only\n",
            "utf8",
          );
        } else {
          writeFileSync(maintenance, "steps:\n  - uses: attacker/checkout@v8\n", "utf8");
        }
        const target = commitFixtureChanges(fixture, {});
        const tree = execFileSync("git", ["rev-parse", "HEAD^{tree}"], {
          cwd: fixture.root,
          encoding: "utf8",
        }).trim();
        execFileSync("git", ["checkout", "-q", trustedHead], {
          cwd: fixture.root,
        });
        const result = runPRGate(
          fixture.root,
          prGate,
          target,
          validCloudEvidence({ sha: target, tree }, ["linux"]),
        );
        expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      },
    );

    it("accepts an identical tree after a squash-style commit rewrite", () => {
      const enabled = {
        min_relay_version: "0.8.0",
        cloud_remote_enabled: true,
        cloud_remote_platforms: ["linux"],
      };
      const fixture = cloudGateFixture(enabled);
      execFileSync("git", ["commit", "--amend", "-qm", "rewritten fixture"], {
        cwd: fixture.root,
      });
      const head = execFileSync("git", ["rev-parse", "HEAD"], {
        cwd: fixture.root,
        encoding: "utf8",
      }).trim();
      expect(head).not.toBe(fixture.sha);

      const result = runCloudGate(fixture, validCloudEvidence(fixture, ["linux"]), head);
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    });

    it("rejects disabled-source evidence for a later enabled runtime", () => {
      const disabled = {
        min_relay_version: "0.8.0",
        cloud_remote_enabled: false,
        cloud_remote_platforms: [],
      };
      const fixture = cloudGateFixture(disabled);
      const enabled = {
        min_relay_version: "0.8.1",
        cloud_remote_enabled: true,
        cloud_remote_platforms: ["linux"],
      };
      const head = commitFixtureChanges(fixture, {
        "version.json": `${JSON.stringify(enabled, null, 2)}\n`,
        "nova/version.json": `${JSON.stringify(enabled, null, 2)}\n`,
        "nova/config.yaml": 'name: NOVA Relay\nversion: "0.8.1"\n',
      });

      const result = runCloudGate(fixture, validCloudEvidence(fixture, ["linux"], "0.8.0"), head);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "stale Home Assistant Cloud evidence may cover only",
      );
    });

    it("rejects provisioned enabled source while strict policy is disabled", () => {
      const { fixture, prGate, trustedHead, env } =
        preparePRGateFixture(true, false);
      const trustedTree = execFileSync(
        "git",
        ["rev-parse", `${trustedHead}^{tree}`],
        { cwd: fixture.root, encoding: "utf8" },
      ).trim();
      const result = runPRGate(
        fixture.root,
        prGate,
        trustedHead,
        validCloudEvidence(
          { sha: trustedHead, tree: trustedTree },
          ["linux"],
        ),
        env,
      );
      expect(result.status).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "enabled Cloud source requires strict up-to-date branch protection policy",
      );
    });

    it.each([
      "cli/cloud_bypass.go",
      "nova/src/cloud-bypass.ts",
      ".github/workflows/release.yml",
      ".goreleaser.yml",
      "install.sh",
      "scripts/dev-sync.sh",
      "scripts/release/cloud-bypass.sh",
      "package.json",
      "clients/cloud-bypass.json",
      "tests/cloud-bypass.test.ts",
      "AGENTS.md",
      "unknown-root-file",
    ])("rejects earlier evidence after path %s changes", (path) => {
      const enabled = {
        min_relay_version: "0.8.0",
        cloud_remote_enabled: true,
        cloud_remote_platforms: ["linux"],
      };
      const fixture = cloudGateFixture(enabled);
      const head = commitFixtureChanges(fixture, {
        [path]: "unreviewed delivery delta\n",
      });
      const result = runCloudGate(fixture, validCloudEvidence(fixture, ["linux"]), head);

      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "stale Home Assistant Cloud evidence may cover only",
      );
    });
  });
}
