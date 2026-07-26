import { spawnSync } from "node:child_process";
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
import { parse } from "yaml";

const sourceGateModeScript = join(
  process.cwd(),
  "scripts",
  "release",
  "resolve-cloud-source-gate-mode.mjs",
);

function runSourceGateMode(
  enabled: unknown,
  reporterAppID: unknown,
  invalidatorAppID: unknown,
): {
  status: number | null;
  stdout: string;
  stderr: string;
  output: string;
} {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-source-gate-mode-"));
  const policyDir = join(root, ".github", "policy");
  const outputPath = join(root, "output");
  mkdirSync(policyDir, { recursive: true });
  writeFileSync(
    join(root, "version.json"),
    JSON.stringify({ cloud_remote_enabled: enabled }),
    "utf8",
  );
  writeFileSync(
    join(policyDir, "repo-policy.json"),
    JSON.stringify({
      cloud_source_gate: {
        reporter_app_id: reporterAppID,
        synchronous_invalidator_app_id: invalidatorAppID,
      },
    }),
    "utf8",
  );
  writeFileSync(outputPath, "", "utf8");
  const result = spawnSync(process.execPath, [sourceGateModeScript], {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, GITHUB_OUTPUT: outputPath },
  });
  return {
    status: result.status,
    stdout: result.stdout,
    stderr: result.stderr,
    output: readFileSync(outputPath, "utf8"),
  };
}

function workflowGateFixture(mutateRelease?: (workflow: string) => string): {
  root: string;
  script: string;
  releaseWorkflow: string;
  rcWorkflow: string;
  sourceWorkflow: string;
  ciWorkflow: string;
} {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-cloud-workflow-gate-"));
  const releaseDir = join(root, "scripts", "release");
  const workflowDir = join(root, ".github", "workflows");
  mkdirSync(releaseDir, { recursive: true });
  mkdirSync(workflowDir, { recursive: true });

  const script = join(releaseDir, "verify-cloud-workflow-gate.sh");
  const module = join(releaseDir, "verify-cloud-workflow-gate.mjs");
  const actionPins = join(releaseDir, "verify-cloud-action-pins.mjs");
  const releaseWorkflow = join(workflowDir, "release.yml");
  const rcWorkflow = join(workflowDir, "release-candidate.yml");
  const sourceWorkflow = join(workflowDir, "cloud-source-gate.yml");
  const ciWorkflow = join(workflowDir, "ci.yml");
  copyFileSync("scripts/release/verify-cloud-workflow-gate.sh", script);
  copyFileSync("scripts/release/verify-cloud-workflow-gate.mjs", module);
  copyFileSync("scripts/release/verify-cloud-action-pins.mjs", actionPins);
  copyFileSync(
    "scripts/release/verify-cloud-workflow-syntax.mjs",
    join(releaseDir, "verify-cloud-workflow-syntax.mjs"),
  );
  chmodSync(script, 0o755);
  const release = readFileSync(".github/workflows/release.yml", "utf8");
  writeFileSync(
    releaseWorkflow,
    mutateRelease ? mutateRelease(release) : release,
    "utf8",
  );
  copyFileSync(".github/workflows/release-candidate.yml", rcWorkflow);
  copyFileSync(".github/workflows/cloud-source-gate.yml", sourceWorkflow);
  copyFileSync(".github/workflows/ci.yml", ciWorkflow);
  return {
    root,
    script,
    releaseWorkflow,
    rcWorkflow,
    sourceWorkflow,
    ciWorkflow,
  };
}

function runWorkflowGate(
  fixture: ReturnType<typeof workflowGateFixture>,
): ReturnType<typeof spawnSync> {
  return spawnSync(
    "bash",
    [
      fixture.script,
      fixture.releaseWorkflow,
      fixture.rcWorkflow,
      fixture.sourceWorkflow,
      fixture.ciWorkflow,
    ],
    {
      cwd: fixture.root,
      encoding: "utf8",
      env: process.env,
    },
  );
}

export function registerCloudWorkflowGateBehaviorTests(): void {
  describe("Home Assistant Cloud workflow gate behavior", () => {
    it.each([
      ["both Apps absent", 0, 0],
      ["reporter only", 42, 0],
      ["invalidator only", 0, 43],
    ])(
      "skips cleanly while Cloud is disabled and source-gate provisioning is incomplete: %s",
      (_name, reporterAppID, invalidatorAppID) => {
        const result = runSourceGateMode(
          false,
          reporterAppID,
          invalidatorAppID,
        );
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(result.output).toBe("should-run=false\n");
      },
    );

    it("rejects a flow-style mutable action hidden in a sensitive job", () => {
      const fixture = workflowGateFixture();
      const workflow = readFileSync(fixture.ciWorkflow, "utf8");
      writeFileSync(
        fixture.ciWorkflow,
        workflow.replace(
          /^\s+- name: Checkout\n\s+uses: actions\/checkout@[0-9a-f]{40} # v[0-9]+\.[0-9]+\.[0-9]+\n\s+with:\n\s+fetch-depth: 0/m,
          "      - { uses: actions/checkout@v7 }",
        ),
        "utf8",
      );
      expect(() => parse(readFileSync(fixture.ciWorkflow, "utf8"))).not.toThrow();
      const result = runWorkflowGate(fixture);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
      expect(result.stderr).toContain("uses non-canonical step syntax");
    });

    it("keeps disabled source checks outside the secret-bearing environment", () => {
      const workflow = parse(
        readFileSync(".github/workflows/cloud-source-gate.yml", "utf8"),
      ) as {
        jobs: Record<string, Record<string, unknown>>;
      };
      const mode = workflow.jobs["cloud-source-mode"];
      const gate = workflow.jobs["cloud-source-gate"];
      if (mode === undefined || gate === undefined) {
        throw new Error("source-gate workflow jobs are missing");
      }
      expect(mode.environment).toBeUndefined();
      expect(mode.outputs).toEqual({
        "should-run": "${{ steps.gate-mode.outputs.should-run }}",
      });
      expect(gate.needs).toBe("cloud-source-mode");
      expect(gate.if).toBe(
        "needs.cloud-source-mode.outputs.should-run == 'true'",
      );
      expect(gate.environment).toEqual({ name: "production" });
    });

    it.each([
      ["disabled canary", false],
      ["enabled source gate", true],
    ])("runs with both exact App IDs provisioned: %s", (_name, enabled) => {
      const result = runSourceGateMode(enabled, 42, 43);
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.output).toBe("should-run=true\n");
    });

    it.each([
      ["both Apps absent", 0, 0],
      ["reporter only", 42, 0],
      ["invalidator only", 0, 43],
    ])(
      "fails closed when Cloud is enabled before complete App provisioning: %s",
      (_name, reporterAppID, invalidatorAppID) => {
        const result = runSourceGateMode(
          true,
          reporterAppID,
          invalidatorAppID,
        );
        expect(result.status).not.toBe(0);
        expect(result.stderr).toContain(
          "enabled Cloud Remote requires both source-gate App IDs",
        );
        expect(result.output).toBe("");
      },
    );

    it.each([
      ["non-boolean feature state", "false", 0, 0],
      ["negative reporter ID", false, -1, 0],
      ["string invalidator ID", false, 0, "43"],
    ])(
      "rejects malformed trusted source-gate state: %s",
      (_name, enabled, reporterAppID, invalidatorAppID) => {
        const result = runSourceGateMode(
          enabled,
          reporterAppID,
          invalidatorAppID,
        );
        expect(result.status).not.toBe(0);
        expect(result.output).toBe("");
      },
    );

    it("accepts the reviewed release workflow ordering", () => {
      const result = runWorkflowGate(workflowGateFixture());
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("release.yml metadata -> Cloud gate");
      expect(result.stdout).toContain(
        "release-candidate.yml metadata -> Cloud gate",
      );
    });

    it.each([
      ["release", "releaseWorkflow"],
      ["release candidate", "rcWorkflow"],
      ["source gate", "sourceWorkflow"],
      ["direct-main CI gate", "ciWorkflow"],
    ] as const)(
      "rejects a mutable action in the %s workflow",
      (_name, fixtureKey) => {
        const fixture = workflowGateFixture();
        const workflowPath = fixture[fixtureKey];
        const workflow = readFileSync(workflowPath, "utf8");
        writeFileSync(
          workflowPath,
          workflow.replace(
            /uses: actions\/checkout@[0-9a-f]{40} # v[0-9]+\.[0-9]+\.[0-9]+/,
            "uses: actions/checkout@v7",
          ),
          "utf8",
        );
        const result = runWorkflowGate(fixture);
        expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
        expect(result.stderr).toContain(
          "must use an immutable full commit SHA",
        );
      },
    );

    it.each([
      [
        "missing live main protection gate",
        (workflow: string) =>
          workflow.replace(
            [
              "      - name: Verify live main protection",
              "        env:",
              "          HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID: ${{ secrets.HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID }}",
              "          HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY: ${{ secrets.HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY }}",
              "        run: bash scripts/release/verify-cloud-publication-main-protection.sh",
              "",
            ].join("\n"),
            "",
          ),
      ],
      [
        "named build before the gate",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify Home Assistant Cloud release gate",
            [
              "      - name: Build unreviewed artifact",
              "        run: go build ./cli",
              "",
              "      - name: Verify Home Assistant Cloud release gate",
            ].join("\n"),
          ),
      ],
      [
        "neutral upload action before the gate",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify Home Assistant Cloud release gate",
            [
              "      - name: Store output",
              "        uses: actions/upload-artifact@v7",
              "",
              "      - name: Verify Home Assistant Cloud release gate",
            ].join("\n"),
          ),
      ],
      [
        "indirect npm artifact build before the gate",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify release metadata",
            [
              "      - name: Package assets",
              "        run: npm run release:rc:local",
              "",
              "      - name: Verify release metadata",
            ].join("\n"),
          ),
      ],
      [
        "unlisted Docker producer before the gate",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify release metadata",
            [
              "      - name: Package container",
              "        uses: docker/build-push-action@v7",
              "",
              "      - name: Verify release metadata",
            ].join("\n"),
          ),
      ],
      [
        "producer hidden inside the metadata step",
        (workflow: string) =>
          workflow.replace(
            '        run: bash scripts/release/verify-release-metadata.sh "${VERSION_TAG}"',
            "        run: npm run release:rc:local",
          ),
      ],
      [
        "metadata BASH_ENV injection",
        (workflow: string) =>
          workflow.replace(
            "          VERSION_TAG: ${{ github.ref_name }}",
            [
              "          VERSION_TAG: ${{ github.ref_name }}",
              "          BASH_ENV: scripts/release/build-install-bundle.sh",
            ].join("\n"),
          ),
      ],
      [
        "workflow-wide BASH_ENV injection",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            [
              "env:",
              "  BASH_ENV: scripts/release/build-install-bundle.sh",
              "",
              "jobs:",
            ].join("\n"),
          ),
      ],
      [
        "flow-style workflow-wide BASH_ENV injection",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            "env: { BASH_ENV: scripts/release/build-install-bundle.sh }\n\njobs:",
          ),
      ],
      [
        "quoted workflow-wide env key",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            '"env":\n  BASH_ENV: scripts/release/build-install-bundle.sh\n\njobs:',
          ),
      ],
      [
        "workflow-wide defaults block",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            "defaults:\n  run:\n    shell: scripts/release/build-install-bundle.sh\n\njobs:",
          ),
      ],
      [
        "flow-style workflow-wide defaults",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            "defaults: { run: { shell: scripts/release/build-install-bundle.sh } }\n\njobs:",
          ),
      ],
      [
        "quoted workflow-wide defaults key",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            "'defaults':\n  run:\n    shell: scripts/release/build-install-bundle.sh\n\njobs:",
          ),
      ],
      [
        "anchored workflow-wide env",
        (workflow: string) =>
          workflow.replace(
            "jobs:",
            "env: &release-env\n  BASH_ENV: scripts/release/build-install-bundle.sh\n\njobs:",
          ),
      ],
      [
        "late flow-style workflow-wide env",
        (workflow: string) =>
          `${workflow}
env: { BASH_ENV: scripts/release/build-install-bundle.sh }
`,
      ],
      [
        "late quoted workflow-wide defaults",
        (workflow: string) =>
          `${workflow}
"defaults": { run: { shell: scripts/release/build-install-bundle.sh } }
`,
      ],
      [
        "alternate checkout ref before the gate",
        (workflow: string) =>
          workflow.replace(
            "          fetch-depth: 0",
            [
              "          fetch-depth: 0",
              "          ref: unreviewed-release-source",
            ].join("\n"),
          ),
      ],
      [
        "write-capable gate job",
        (workflow: string) =>
          workflow.replace("      contents: read", "      contents: write"),
      ],
      [
        "soft-failing gate",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify Home Assistant Cloud release gate",
            [
              "      - name: Verify Home Assistant Cloud release gate",
              "        continue-on-error: true",
            ].join("\n"),
          ),
      ],
      [
        "conditional gate",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify Home Assistant Cloud release gate",
            [
              "      - name: Verify Home Assistant Cloud release gate",
              "        if: ${{ false }}",
            ].join("\n"),
          ),
      ],
      [
        "independent publish job",
        (workflow: string) => workflow.replace("    needs: release\n", ""),
      ],
      [
        "independent producer with an unrecognized command",
        (workflow: string) =>
          `${workflow}
  package-hidden:
    runs-on: ubuntu-latest
    steps:
      - name: Package assets
        run: npm run release:rc:local
`,
      ],
      [
        "flow-style hidden producer job",
        (workflow: string) =>
          `${workflow}
  package-hidden: { runs-on: ubuntu-latest, steps: [{ run: "npm run release:rc:local" }] }
`,
      ],
      [
        "failure-bypassing publish job",
        (workflow: string) =>
          workflow.replace(
            "    needs: release",
            "    needs: release\n    if: always()",
          ),
      ],
      [
        "quoted failure-bypassing release job",
        (workflow: string) =>
          workflow.replace(
            "    needs: cloud-release-gate",
            '    needs: cloud-release-gate\n    "if": always()',
          ),
      ],
      [
        "explicit failure-bypassing release job",
        (workflow: string) =>
          workflow.replace(
            "    needs: cloud-release-gate",
            "    needs: cloud-release-gate\n    ? if\n    : always()",
          ),
      ],
      [
        "merged failure-bypassing release job",
        (workflow: string) =>
          workflow.replace(
            "    needs: cloud-release-gate",
            "    needs: cloud-release-gate\n    <<: { if: always() }",
          ),
      ],
      [
        "space-separated failure-bypassing release job key",
        (workflow: string) =>
          workflow.replace(
            "    needs: cloud-release-gate",
            "    needs: cloud-release-gate\n    if : always()",
          ),
      ],
      [
        "tab-separated failure-bypassing release job key",
        (workflow: string) =>
          workflow.replace(
            "    needs: cloud-release-gate",
            "    needs: cloud-release-gate\n    if\t: always()",
          ),
      ],
      [
        "space-separated soft-failing release job key",
        (workflow: string) =>
          workflow.replace(
            "    needs: cloud-release-gate",
            "    needs: cloud-release-gate\n    continue-on-error : true",
          ),
      ],
      [
        "soft-failing artifact producer",
        (workflow: string) =>
          workflow.replace(
            "      - name: GoReleaser",
            "      - name: GoReleaser\n        continue-on-error: true",
          ),
      ],
      [
        "failure-bypassing artifact producer",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify complete draft and publish",
            "      - name: Verify complete draft and publish\n        if: always()",
          ),
      ],
      [
        "quoted soft-failing artifact producer",
        (workflow: string) =>
          workflow.replace(
            "      - name: GoReleaser",
            '      - name: GoReleaser\n        "continue-on-error": true',
          ),
      ],
      [
        "explicit failure-bypassing artifact producer",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify complete draft and publish",
            "      - name: Verify complete draft and publish\n        ? if\n        : always()",
          ),
      ],
      [
        "space-separated failure-bypassing artifact key",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify complete draft and publish",
            "      - name: Verify complete draft and publish\n        if : always()",
          ),
      ],
      [
        "tab-separated soft-failing artifact key",
        (workflow: string) =>
          workflow.replace(
            "      - name: Verify complete draft and publish",
            "      - name: Verify complete draft and publish\n        continue-on-error\t: true",
          ),
      ],
    ])("kills the %s mutant", (_name, mutateRelease) => {
      const fixture = workflowGateFixture(mutateRelease);
      expect(() =>
        parse(readFileSync(fixture.releaseWorkflow, "utf8")),
      ).not.toThrow();
      const result = runWorkflowGate(fixture);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    });
  });
}
