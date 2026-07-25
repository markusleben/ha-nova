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

function workflowGateFixture(mutateRelease?: (workflow: string) => string): {
  root: string;
  script: string;
  releaseWorkflow: string;
  rcWorkflow: string;
} {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-cloud-workflow-gate-"));
  const releaseDir = join(root, "scripts", "release");
  const workflowDir = join(root, ".github", "workflows");
  mkdirSync(releaseDir, { recursive: true });
  mkdirSync(workflowDir, { recursive: true });

  const script = join(releaseDir, "verify-cloud-workflow-gate.sh");
  const module = join(releaseDir, "verify-cloud-workflow-gate.mjs");
  const releaseWorkflow = join(workflowDir, "release.yml");
  const rcWorkflow = join(workflowDir, "release-candidate.yml");
  copyFileSync("scripts/release/verify-cloud-workflow-gate.sh", script);
  copyFileSync("scripts/release/verify-cloud-workflow-gate.mjs", module);
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
  return { root, script, releaseWorkflow, rcWorkflow };
}

function runWorkflowGate(
  fixture: ReturnType<typeof workflowGateFixture>,
): ReturnType<typeof spawnSync> {
  return spawnSync(
    "bash",
    [fixture.script, fixture.releaseWorkflow, fixture.rcWorkflow],
    {
      cwd: fixture.root,
      encoding: "utf8",
      env: process.env,
    },
  );
}

export function registerCloudWorkflowGateBehaviorTests(): void {
  describe("Home Assistant Cloud workflow gate behavior", () => {
    it("accepts the reviewed release workflow ordering", () => {
      const result = runWorkflowGate(workflowGateFixture());
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("release.yml metadata -> Cloud gate");
      expect(result.stdout).toContain(
        "release-candidate.yml metadata -> Cloud gate",
      );
    });

    it.each([
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
