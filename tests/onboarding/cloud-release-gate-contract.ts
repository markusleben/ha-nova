import { readFileSync } from "node:fs";

import { expect, it } from "vitest";

export function registerCloudReleaseGateContractTests(): void {
  const cloudReleaseGateVerifier = readFileSync(
    "scripts/release/verify-cloud-release-gate.sh",
    "utf8",
  );
  const cloudWorkflowGateVerifier = [
    "scripts/release/verify-cloud-workflow-gate.sh",
    "scripts/release/verify-cloud-workflow-gate.mjs",
    "scripts/release/verify-cloud-workflow-syntax.mjs",
  ]
    .map((file) => readFileSync(file, "utf8"))
    .join("\n");
  const releaseWorkflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(".github/workflows/release-candidate.yml", "utf8");
  const ciWorkflow = readFileSync(".github/workflows/ci.yml", "utf8");
  const sourceGateWorkflow = readFileSync(".github/workflows/cloud-source-gate.yml", "utf8");
  const checkTokenScript = readFileSync(
    "scripts/release/create-cloud-source-check-token.mjs",
    "utf8",
  );
  const ciGate = ciWorkflow.slice(
    ciWorkflow.indexOf("  ci-gate:"),
    ciWorkflow.indexOf("  go-build:"),
  );
  const releasing = readFileSync("docs/releasing.md", "utf8");

  it("keeps Cloud publication disabled until commit-bound real-device evidence exists", () => {
    const version = JSON.parse(readFileSync("version.json", "utf8")) as {
      cloud_remote_enabled?: unknown;
      cloud_remote_platforms?: unknown;
    };
    const appVersion = JSON.parse(readFileSync("nova/version.json", "utf8")) as {
      cloud_remote_enabled?: unknown;
      cloud_remote_platforms?: unknown;
    };
    expect(version.cloud_remote_enabled).toBe(false);
    expect(version.cloud_remote_platforms).toEqual([]);
    expect(appVersion.cloud_remote_enabled).toBe(version.cloud_remote_enabled);
    expect(appVersion.cloud_remote_platforms).toEqual(version.cloud_remote_platforms);

    for (const workflow of [releaseWorkflow, rcWorkflow]) {
      const protectionIndex = workflow.indexOf(
        "name: Verify live main protection",
      );
      const metadataIndex = workflow.indexOf("name: Verify release metadata");
      const gateIndex = workflow.indexOf("bash scripts/release/verify-cloud-release-gate.sh");
      expect(protectionIndex).toBeGreaterThanOrEqual(0);
      expect(metadataIndex).toBeGreaterThan(protectionIndex);
      expect(metadataIndex).toBeGreaterThanOrEqual(0);
      expect(gateIndex).toBeGreaterThan(metadataIndex);
      expect(workflow).toContain(
        "HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: ${{ secrets.HA_NOVA_CLOUD_GATE_EVIDENCE_JSON }}",
      );
      expect(workflow).toContain(
        "run: bash scripts/release/verify-cloud-publication-main-protection.sh",
      );
    }

    expect(cloudReleaseGateVerifier).toContain("version.cloud_remote_enabled");
    expect(cloudReleaseGateVerifier).toContain("version.cloud_remote_platforms");
    expect(cloudReleaseGateVerifier).toContain('readVersionMetadata("nova/version.json")');
    expect(cloudReleaseGateVerifier).toContain("Cloud release metadata must match exactly");
    expect(cloudReleaseGateVerifier).toContain('new Set(["darwin", "linux", "windows"])');
    expect(cloudReleaseGateVerifier).toContain("HA_NOVA_CLOUD_GATE_EVIDENCE_JSON");
    expect(cloudReleaseGateVerifier).toContain("evidence.schema !== 2");
    expect(cloudReleaseGateVerifier).toContain("evidenceCommitTree !== evidence.tree_sha");
    expect(cloudReleaseGateVerifier).toContain("verify-cloud-workflow-uses-only.mjs");
    expect(cloudReleaseGateVerifier).toContain("workflowCommit !== commit");
    for (const check of [
      "parity",
      "stress_10000",
      "roles",
      "domains_mfa",
      "lifecycle",
      "redirects_non_disclosure",
      "installed_relay_app",
      "routing",
      "signing_and_update_matrix",
    ]) {
      expect(cloudReleaseGateVerifier).toContain(`"${check}"`);
    }
    expect(cloudReleaseGateVerifier).toContain("requireExactKeys(keyrings, platforms");
    expect(cloudReleaseGateVerifier).toContain("readRelayAppVersion()");
    expect(cloudReleaseGateVerifier).toContain('platforms.includes("darwin")');
    expect(cloudReleaseGateVerifier).toContain(
      "evidence.relay_app.source_tree_sha !== evidence.tree_sha",
    );
    expect(cloudReleaseGateVerifier).not.toMatch(/console\.(?:log|error)\([^)]*rawEvidence/);
    expect(checkTokenScript).toContain(
      'tokenMode !== "reporter" && tokenMode !== "administration-read"',
    );
    expect(checkTokenScript).toContain(
      '? { administration: "read" }',
    );

    const releasePipelineVerifier = readFileSync(
      "scripts/release/verify-release-pipeline.sh",
      "utf8",
    );
    expect(releasePipelineVerifier).toContain("verify-cloud-workflow-gate.sh");
    expect(releasePipelineVerifier).toContain(
      "verify-cloud-publication-main-protection.sh",
    );
    expect(cloudWorkflowGateVerifier).toContain(
      "must run live main protection, release metadata, and the Cloud gate consecutively",
    );
    expect(cloudWorkflowGateVerifier).toContain(
      "may contain only Checkout, Setup Node, production environment policy, live main protection, release metadata, and the Cloud gate",
    );
    expect(cloudWorkflowGateVerifier).toContain(
      "must mint and use only the exact Cloud source App administration-read token",
    );
    expect(cloudWorkflowGateVerifier).toContain("must depend on Cloud gate job");
    expect(cloudWorkflowGateVerifier).toContain("must not build, upload, or publish artifacts");
    expect(cloudWorkflowGateVerifier).toContain("continue-on-error");
    expect(releasing).toContain("### Home Assistant Cloud publication gate");
    expect(releasing).toContain("`keyrings` keys must exactly match `cloud_remote_platforms`");
    expect(releasing).toContain("checked-out `HEAD` must equal `GITHUB_SHA`");
    expect(releasing).toContain("`refs/pull/<number>/merge`");
    expect(releasing).toMatch(/`merge_group` creates another\s+synthetic checkout commit/);
    expect(releasing).toContain(
      "After squash merge, the resulting `main` commit has a different SHA",
    );
  });

  it("runs the source gate in CI and blocks direct main App-source bypasses", () => {
    expect(ciWorkflow).toContain("on:\n  pull_request:");
    expect(ciWorkflow).toContain("  merge_group:");
    expect(ciGate).not.toContain("environment:");
    expect(ciGate).toContain("fetch-depth: 0");
    expect(ciGate).toContain(
      "HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: ${{ secrets.HA_NOVA_CLOUD_GATE_EVIDENCE_JSON }}",
    );
    expect(ciGate).toContain("run: bash scripts/release/verify-cloud-release-gate.sh");
    expect(ciGate.indexOf("verify-cloud-release-gate.sh")).toBeLessThan(
      ciGate.indexOf("name: Verify repository"),
    );
    expect(cloudReleaseGateVerifier).not.toContain("cloudDeliveryRelevant");
    expect(cloudReleaseGateVerifier).not.toContain("merge-base");
    expect(cloudReleaseGateVerifier).not.toContain("allowedActivationDeltaPaths");
    expect(sourceGateWorkflow).toContain("workflow_run:");
    expect(sourceGateWorkflow).toContain("- CI");
    expect(sourceGateWorkflow).toContain("- in_progress");
    expect(sourceGateWorkflow).toContain("- requested");
    expect(sourceGateWorkflow).toContain("name: trusted-cloud-source-reporter");
    expect(sourceGateWorkflow).toContain("name: production");
    expect(sourceGateWorkflow).toContain("node scripts/release/run-cloud-source-check.mjs");
    expect(sourceGateWorkflow).toContain(
      "node scripts/release/create-cloud-source-check-token.mjs",
    );
    expect(sourceGateWorkflow).not.toContain("actions/download-artifact");
    expect(sourceGateWorkflow).not.toContain("actions/cache");
    expect(sourceGateWorkflow).not.toContain("github.event.pull_request.head");
    expect(sourceGateWorkflow).not.toMatch(/^\s*run:.*github\.event\.pull_request/m);
    const sourceGateScript = readFileSync(
      "scripts/release/verify-cloud-target-source-gate.sh",
      "utf8",
    );
    expect(sourceGateScript).toContain("HEAD:.github/workflows");
    expect(sourceGateScript).toContain("${target_commit}:.github/workflows");
    expect(sourceGateScript).toContain(
      '[[ "${target_workflows_tree}" == "${trusted_workflows_tree}" ]]',
    );
    expect(sourceGateScript).toContain("verify-cloud-workflow-uses-only.mjs");
    expect(sourceGateScript).toContain(
      "pull request merge commit does not bind the expected base and head",
    );
    const reporter = readFileSync("scripts/release/run-cloud-source-check.mjs", "utf8");
    const reporterHelper = readFileSync(
      "scripts/release/cloud-source-check-reporter.mjs",
      "utf8",
    );
    expect(reporterHelper).toContain('const checkName = "cloud-source-gate"');
    expect(reporterHelper).toContain(
      '`workflow-run:${workflowRun.id}:attempt:${workflowRun.run_attempt}:target:${requireSHA(targetSHA, "source check target SHA")}`',
    );
    expect(reporter).toContain(
      "ensurePendingCheck(\n    currentWorkflowRun,\n    verifiedTargetSHA,\n  )",
    );
    expect(reporter).toContain("terminalSuccess");
    expect(reporterHelper).toContain(
      "source checks have conflicting terminal conclusions",
    );
    expect(reporter).toContain('if (action !== "completed" || terminalSuccess)');
    expect(reporter).toContain("currentPullRequest(headSHA)");
    expect(reporter).toContain("latest.base?.sha !== currentPR.base.sha");
    expect(reporter).toContain(
      '["scripts/release/verify-github-main-protection.sh", repository, "main"]',
    );
    expect(reporter).toContain("{ GH_TOKEN: checkToken }");
    expect(reporter).toContain("HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT: verifiedTargetSHA");
    expect(reporter).toContain("verifiedTargetSHA = resolveRemoteRef(sourceRef)");
    expect(reporter).toContain("resolveRemoteRef(sourceRef) !== verifiedTargetSHA");
    expect(reporter).toContain("const finalPR = await currentPullRequest(headSHA)");
    expect(reporter).toContain("finalPR.number !== currentPR.number");
    expect(reporter).toContain(
      'finalPR.merge_commit_sha,\n        "final pull request merge commit SHA"',
    );
    expect(reporter).toContain("pull request identity changed after final source verification");
    expect(reporter).toContain("source ref changed while the trusted source gate was running");
    expect(checkTokenScript).toContain(
      '{ administration: "read", checks: "write" }',
    );
    expect(checkTokenScript).toContain('access.permissions?.administration !== "read"');
    expect(checkTokenScript).toContain("`app-id=${appId}\\n`");
    expect(reporter).not.toContain("download-artifact");
    const codeowners = readFileSync(".github/CODEOWNERS", "utf8");
    for (const workflow of [
      "cloud-source-gate.yml",
      "ci.yml",
      "release.yml",
      "release-candidate.yml",
    ]) {
      expect(codeowners).toContain(`/.github/workflows/${workflow} @markusleben`);
    }
  });

  it("keeps production artifacts outside the compile-time developer path", () => {
    const goreleaser = readFileSync(".goreleaser.yml", "utf8");
    const releaseIdentity = readFileSync("cli/cloud_feature_build_release.go", "utf8");
    const developmentIdentity = readFileSync("cli/cloud_feature_build_dev.go", "utf8");
    const officialIdentity = readFileSync("cli/cloud_feature_build_official.go", "utf8");
    const provenance = readFileSync("cli/cloud_release_provenance.go", "utf8");
    expect(goreleaser).not.toContain("cloudremote_dev");
    expect(goreleaser).not.toContain("cloudRemoteDevAppSlug");
    expect(goreleaser).toContain("cloudremote_official");
    expect(releaseIdentity).toContain(
      "//go:build !cloudremote_official && !cloudremote_dev && !cloudremote_disabled",
    );
    expect(releaseIdentity).toContain("Disabled: true");
    expect(officialIdentity).toContain("//go:build cloudremote_official");
    expect(officialIdentity).toContain("Official: true");
    expect(developmentIdentity).toContain("//go:build cloudremote_dev");
    expect(developmentIdentity).toContain("cloudRemoteDevAppSlug");
    expect(provenance).toContain("ed25519.Verify");
    expect(provenance).toContain("BinarySHA256");
    expect(provenance).toContain("SourceTreeSHA");
    expect(provenance).toContain("cloudReleaseEvidencePublicKey");
    expect(rcWorkflow).toContain(
      'bash scripts/release/build-rc-binaries.sh "${{ inputs.version_tag }}"',
    );
    expect(rcWorkflow).not.toContain("build --snapshot");
    expect(rcWorkflow).toContain("internal-cloud-release-check");
  });
}
