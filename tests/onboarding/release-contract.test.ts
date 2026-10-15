import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { registerCloudReleaseBuildContractTests } from "./cloud-release-build-contract.js";
import { registerWorkflowPinsContractTests } from "./workflow-pins-contract.js";

describe("release contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const installer = readFileSync("install.ps1", "utf8");
  const bundleBuilder = readFileSync(
    "scripts/release/build-install-bundle.sh",
    "utf8",
  );
  const relayImageVerifier = readFileSync(
    "scripts/release/verify-relay-image.sh",
    "utf8",
  );
  const releaseAssetVerifier = readFileSync(
    "scripts/release/verify-release-assets.sh",
    "utf8",
  );
  const releaseWorkflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(
    ".github/workflows/release-candidate.yml",
    "utf8",
  );
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const releaseDocs = `${releasing}\n${readFileSync("docs/release-validation.md", "utf8")}`;
  const readme = readFileSync("README.md", "utf8");
  const linuxHeadlessHelper = readFileSync(
    "scripts/smoke/linux-headless-setup-check.sh",
    "utf8",
  );
  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    dependencies?: Record<string, string>;
    devDependencies?: Record<string, string>;
    scripts?: Record<string, string>;
  };

  it("keeps local RC packaging focused on install bundles only", () => {
    expect(pkg.scripts?.["release:rc:local"]).toBe(
      'bash scripts/release/build-sign-darwin-binaries.sh "${HA_NOVA_RC_TAG:?set HA_NOVA_RC_TAG to vX.Y.Z-rcN}" && bash scripts/release/build-rc-binaries.sh "${HA_NOVA_RC_TAG}" && bash scripts/release/build-install-bundle.sh "${HA_NOVA_RC_TAG#v}"',
    );
    expect(pkg.scripts?.["release:rc:local"]).not.toContain("snapshot");
    expect(pkg.scripts?.["release:winget:stage-submission"]).toBeUndefined();
    expect(existsSync("scripts/release/build-install-bundle.sh")).toBe(true);
    expect(existsSync("scripts/release/build-winget-manifest.sh")).toBe(false);
    expect(
      existsSync("scripts/release/prepare-winget-pkgs-submission.sh"),
    ).toBe(false);
    expect(existsSync("release/winget-publication-state.json")).toBe(false);
  });

  it("accepts only the exact expected Windows negative provenance result", () => {
    for (const workflow of [releaseWorkflow, rcWorkflow]) {
      expect(workflow).toContain("$provenanceExitCode -ne 1 -or");
      expect(workflow).toContain(
        '$provenanceOutput -ne "[ha-nova] ERROR: official Cloud release provenance is not enabled"',
      );
      expect(workflow).toContain("$global:LASTEXITCODE = 0");
    }
  });

  it.each(["v0.22.0-rc0", "v0.22.0-rc01", "v01.22.0-rc1"])(
    "rejects non-canonical RC tag %s across release entry points",
    (tag) => {
      for (const [script, args] of [
        ["scripts/release/verify-release-metadata.sh", [tag]],
        ["scripts/release/verify-next-release-version.sh", [tag]],
        ["scripts/release/verify-release-assets.sh", [tag]],
        ["scripts/release/build-rc-binaries.sh", [tag]],
        ["scripts/release/build-sign-darwin-binaries.sh", [tag]],
      ] as const) {
        const result = spawnSync("bash", [script, ...args], {
          encoding: "utf8",
        });
        expect(
          result.status,
          `${script}\n${result.stdout}\n${result.stderr}`,
        ).not.toBe(0);
      }
    },
  );

  it("builds Unix install bundles without macOS copyfile metadata noise", () => {
    expect(bundleBuilder).toContain(
      'COPYFILE_DISABLE=1 tar --format ustar -czf "${output}" -C "${stage_dir}" ha-nova',
    );
  });

  it("ships the privacy document targeted by bundled relative links", () => {
    expect(bundleBuilder).toContain(
      'cp "${ROOT_DIR}/PRIVACY.md" "${bundle_root}/PRIVACY.md"',
    );
  });

  it("keeps the final release workflow free of winget artifacts and validation", () => {
    expect(releaseWorkflow).toContain("Build install bundles");
    expect(releaseWorkflow).toContain("Upload install bundles");
    expect(releaseWorkflow).toContain("Smoke Windows installer");
    expect(releaseWorkflow).not.toContain("Build winget manifests");
    expect(releaseWorkflow).not.toContain("Upload winget manifests");
    expect(releaseWorkflow).not.toContain("release-winget-manifests");
    expect(releaseWorkflow).not.toContain("winget validate");
    expect(releaseWorkflow).not.toContain("dist/winget");
  });

  it("keeps README install commands visible and versionless", () => {
    expect(readme).toContain(
      "curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash",
    );
    expect(readme).toContain(
      "irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex",
    );
    expect(readme).toContain(
      "The installer selects the latest stable release.",
    );
    expect(readme).not.toMatch(/HA_NOVA_VERSION=v\d/);
    expect(readme).not.toMatch(/\$env:HA_NOVA_VERSION\s*=\s*['\"]v\d/);
  });

  it("pins the GoReleaser release tag to the triggering workflow ref", () => {
    expect(releaseWorkflow).toContain(
      "GORELEASER_CURRENT_TAG: ${{ github.ref_name }}",
    );
  });

  it("keeps releases private until every required asset is present", () => {
    expect(goreleaser).toMatch(/^\s*draft:\s*true\s*$/m);
    expect(goreleaser).toMatch(/^\s*replace_existing_draft:\s*true\s*$/m);
    expect(goreleaser).toMatch(/^\s*mode:\s*replace\s*$/m);
    expect(releaseWorkflow).toContain("publish-release:");
    expect(releaseWorkflow).toContain(
      "name: Verify complete draft and publish",
    );
    expect(releaseWorkflow).toContain('version: "v2.17.0"');
    expect(releaseWorkflow).not.toContain('version: "~> v2"');
    expect(releaseWorkflow).toContain("Detect an already-published retry");
    expect(releaseWorkflow).toContain(
      "gh api --paginate --slurp 'repos/markusleben/ha-nova/releases?per_page=100'",
    );
    expect(releaseWorkflow).not.toMatch(/if release_json=.*gh release view/);
    expect(releaseWorkflow).toContain(
      "if: steps.release-state.outputs.published != 'true'",
    );
    expect(
      releaseWorkflow.match(/verify-release-assets\.sh "\$GITHUB_REF_NAME"/g),
    ).toHaveLength(2);
    expect(releaseAssetVerifier).toContain('repository="markusleben/ha-nova"');
    expect(releaseAssetVerifier).toContain(
      "(.assets | length) == ($expected | length)",
    );
    expect(releaseAssetVerifier).toContain(
      "([.assets[].name] | sort) == $expected",
    );
    expect(releaseAssetVerifier).toContain('.state == "uploaded"');
    expect(releaseAssetVerifier).toContain(
      '.size | type == "number" and . > 0',
    );
    expect(releaseAssetVerifier).toContain('test("^sha256:[0-9a-f]{64}$")');
    expect(releaseWorkflow).toContain(
      'gh release edit "$GITHUB_REF_NAME" --draft=false',
    );
    expect(releaseWorkflow).toMatch(
      /if \[\[ "\$expected_prerelease" == "true" \]\]; then[\s\S]*?else\s+# Re-assert Latest[\s\S]*?gh release edit "\$GITHUB_REF_NAME" --draft=false --latest --verify-tag\s+fi/,
    );
    expect(releaseWorkflow).toContain(
      "gh api 'repos/markusleben/ha-nova/releases/latest' --jq '.tag_name'",
    );
    expect(releaseWorkflow).toContain(
      '[[ "$latest_tag" != "$GITHUB_REF_NAME" ]]',
    );
    expect(releaseWorkflow).toContain("needs: publish-release");
    expect(releaseWorkflow).toContain("smoke-release-bundles:");
    expect(releaseWorkflow).toContain("needs: smoke-release-bundles");
    expect(releaseWorkflow).toContain(
      'gh release download "$GITHUB_REF_NAME"',
    );
    expect(releaseWorkflow).toContain("internal-cloud-release-check");
    expect(releaseWorkflow.indexOf("Upload install bundles")).toBeLessThan(
      releaseWorkflow.indexOf("smoke-release-bundles:"),
    );
    expect(releaseWorkflow.indexOf("smoke-release-bundles:")).toBeLessThan(
      releaseWorkflow.indexOf("publish-release:"),
    );
    expect(releaseWorkflow.indexOf("publish-release:")).toBeLessThan(
      releaseWorkflow.indexOf("smoke-installers:"),
    );
  });

  it("uses only the curated release header as the changelog", () => {
    expect(goreleaser).toMatch(
      /changelog:\n\s+#[^\n]*\n(?:\s+#[^\n]*\n)*\s+disable: true/,
    );
    expect(goreleaser).not.toContain("regexp: '^feat");
    expect(goreleaser).not.toContain("regexp: '^fix");
  });

  it("keeps the next-release-version check immune to release-payload growth", () => {
    // The default spawnSync maxBuffer (1 MiB) failed the v0.14.0 publish with
    // ENOBUFS once the release list outgrew it. The gh call must project the
    // payload down to the fields it reads and carry an explicit buffer.
    const versionCheck = readFileSync(
      "scripts/release/verify-next-release-version.sh",
      "utf8",
    );
    expect(versionCheck).toContain('".[] | {tag_name, draft, prerelease}"');
    expect(versionCheck).toContain("maxBuffer: 64 * 1024 * 1024");
  });

  it("rejects non-canonical Relay image release selectors before external checks", () => {
    expect(relayImageVerifier).toContain(
      "relay version must be canonical X.Y.Z",
    );
    const result = spawnSync(
      "bash",
      [
        "scripts/release/verify-relay-image.sh",
        "0000000000000000000000000000000000000000",
        "01.2.3",
      ],
      { encoding: "utf8" },
    );
    expect(result.status).not.toBe(0);
    expect(`${result.stdout}\n${result.stderr}`).toContain(
      "relay version must be canonical X.Y.Z",
    );
  });

  it("rejects non-canonical App image versions before registry checks", () => {
    const result = spawnSync(
      "bash",
      ["scripts/release/verify-app-image.sh", "01.2.3"],
      { encoding: "utf8" },
    );
    expect(result.status).not.toBe(0);
    expect(`${result.stdout}\n${result.stderr}`).toContain(
      "App version must be canonical X.Y.Z",
    );
  });

  it("publishes App images only from the package-write job, gated by app-image-verify", () => {
    const appImage = readFileSync(".github/workflows/app-image.yml", "utf8");
    expect(appImage.match(/packages: write/g)).toHaveLength(1);
    expect(appImage).toContain("name: app-image-verify");
    // A skipped required check counts as passed: verify must always run.
    expect(appImage).toContain("if: ${{ always() }}");
    expect(appImage).toContain("github.event.pull_request.head.repo.full_name == github.repository");
    expect(appImage).toContain("refusing to publish");
    expect(releasing).toContain("app-image-verify");
  });

  it("keeps the RC workflow free of winget artifacts and guidance", () => {
    expect(rcWorkflow).toContain("Build install bundles");
    expect(rcWorkflow).toContain("Upload RC artifacts");
    expect(rcWorkflow).toContain("Smoke Windows bundle");
    expect(rcWorkflow).not.toContain("Build winget manifests");
    expect(rcWorkflow).not.toContain("Upload RC winget manifests");
    expect(rcWorkflow).not.toContain("Download RC winget manifests");
    expect(rcWorkflow).not.toContain("winget validate");
    expect(rcWorkflow).not.toContain(
      "$ProgressPreference = 'SilentlyContinue'",
    );
    expect(rcWorkflow).not.toContain("dist/winget");
    expect(rcWorkflow).toContain('platform="darwin"');
    expect(rcWorkflow).toContain('platform="linux"');
    expect(rcWorkflow).toContain(
      'elif HOME="$smoke_home" "$root/ha-nova" internal-cloud-release-check; then',
    );
    expect(rcWorkflow).toContain(
      "unlisted Windows runtime returned an unexpected provenance result",
    );
  });

  it("keeps the RC workflow build-and-smoke only, never publishing a release", () => {
    // The v* tag ruleset blocks the Actions token from creating tags, so any
    // automated publish here only 422s. Real RC publishing is the tag-first
    // rehearsal driven by release.yml. Guard the trap from coming back.
    expect(rcWorkflow).not.toContain("gh release create");
    expect(rcWorkflow).not.toContain("gh release edit");
    expect(rcWorkflow).not.toContain("gh release upload");
    expect(rcWorkflow).not.toContain("publish_release");
    expect(rcWorkflow).not.toContain("publish-rc-release");
  });

  it("keeps GoReleaser marking prerelease tags automatically", () => {
    // -rcN dress-rehearsal tags must publish as a prerelease, not a stable
    // release. release.yml relies on this for the tag-first rehearsal.
    expect(goreleaser).toMatch(/^\s*prerelease:\s*auto\s*$/m);
  });

  it("gives RC tags preview release notes instead of the stable header", () => {
    // The tag-first rehearsal publishes the -rcN tag via release.yml, so the
    // GoReleaser header must switch to preview framing for prerelease tags
    // rather than reusing the stable "Why This Release Exists" copy.
    expect(goreleaser).toContain(
      "{{ if .Prerelease }}## Preview / Release Candidate",
    );
    expect(goreleaser).toContain("Stable users should ignore this prerelease");
  });

  it("keeps the Windows installer bundle-managed while preserving quiet download UX", () => {
    expect(installer).toContain("Invoke-DownloadFile");
    expect(installer).toContain(
      '$global:ProgressPreference = "SilentlyContinue"',
    );
    expect(installer).not.toContain("Stop-ForWingetInstall");
    expect(installer).not.toContain("Get-Command winget");
    expect(installer).not.toContain("winget upgrade --id");
    expect(installer).not.toContain("winget uninstall --id");
  });

  it("keeps release docs aligned to the pinned stable install contract", () => {
    expect(releaseDocs).toContain(
      "Windows uses a single supported install path: `install.ps1`",
    );
    expect(releaseDocs).toContain("Supported stable selection:");
    expect(releaseDocs).toContain(
      "curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z bash",
    );
    expect(releaseDocs).toContain(
      "irm https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.ps1 | iex",
    );
    expect(releaseDocs).toContain(
      "stable release notes must publish tag-pinned install commands, never `main` bootstrap URLs",
    );
    expect(releaseDocs).toContain("npm run dev:validation:harness");
    expect(releaseDocs).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/install.sh",
    );
    expect(releaseDocs).not.toContain(
      "raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1",
    );
    expect(releaseDocs).not.toContain("winget");
    expect(releaseDocs).not.toContain("$ProgressPreference = 'SilentlyContinue'");
  });

  it("documents the tag-first release rehearsal", () => {
    expect(releasing).toContain("tag-first dress rehearsal");
    expect(releasing).toContain("GORELEASER_CURRENT_TAG");
  });

  it("gates a Relay release on the exact successful push run and immutable GHCR digest", () => {
    expect(relayImageVerifier).toContain("^[0-9a-f]{40}$");
    expect(relayImageVerifier).toContain(
      "actions/workflows/relay-image.yml/runs",
    );
    expect(relayImageVerifier).toContain("head_sha=${commit_sha}");
    expect(relayImageVerifier).toContain("-f branch=main");
    expect(relayImageVerifier).toContain("-f event=push");
    expect(relayImageVerifier).toContain("-f status=success");
    expect(relayImageVerifier).toContain(".head_sha == $sha");
    expect(relayImageVerifier).toContain('.head_branch == "main"');
    expect(relayImageVerifier).toContain('.conclusion == "success"');
    expect(relayImageVerifier).toContain(
      'latest_ref="${image_repository}:latest"',
    );
    expect(relayImageVerifier).toContain(
      'version_ref="${image_repository}:${relay_version}"',
    );
    expect(relayImageVerifier).toContain(
      'sha_ref="${image_repository}:sha-${commit_sha}"',
    );
    expect(relayImageVerifier).toContain("docker buildx imagetools inspect");
    expect(relayImageVerifier).toContain(".manifest.digest");
    expect(relayImageVerifier).toContain(
      '[[ "$latest_digest" == "$version_digest" ]]',
    );
    expect(relayImageVerifier).toContain(
      '[[ "$version_digest" == "$sha_digest" ]]',
    );
    expect(relayImageVerifier).toContain("{{json .Provenance}}");
    expect(relayImageVerifier).toContain('repository="markusleben/ha-nova"');
    expect(relayImageVerifier).not.toContain("HA_NOVA_GITHUB_REPOSITORY");
    expect(relayImageVerifier).toContain('"linux/amd64", "linux/arm64"');
    expect(relayImageVerifier).toContain('["vcs:revision"] == $sha');
    expect(relayImageVerifier).toContain(
      '["vcs:source"] == "https://github.com/markusleben/ha-nova"',
    );
    expect(relayImageVerifier).toContain(
      '["label:org.opencontainers.image.source"] == "https://github.com/markusleben/ha-nova"',
    );
    expect(relayImageVerifier).toContain('["vcs:localdir:context"] == "nova"');
    expect(relayImageVerifier).toContain('startswith($run + "/attempts/")');
    expect(relayImageVerifier).toContain("<= $max_attempt");
    expect(relayImageVerifier).toContain(
      '["build-arg:RELAY_VERSION"] == $version',
    );
    expect(relayImageVerifier).toContain(
      '["label:org.opencontainers.image.version"] == $version',
    );
  });

  it("orders external publication gates around the RC and final tag", () => {
    const rehearsal = releasing.slice(
      releasing.indexOf("## 4. Rehearsal and Tag Steps"),
      releasing.indexOf("## 5. What `release.yml` Does"),
    );
    const orderedMarkers = [
      "Merge the reviewed PR state",
      "verify-relay-image.sh <reviewed-merge-sha> <relay-version>",
      "Verify the published RC over the real",
      "cut the final tag",
    ];
    let previousIndex = -1;
    for (const marker of orderedMarkers) {
      const markerIndex = rehearsal.indexOf(marker);
      expect(
        markerIndex,
        `missing release-order marker: ${marker}`,
      ).toBeGreaterThan(previousIndex);
      previousIndex = markerIndex;
    }
    expect(rehearsal).toContain(
      "whose `headSha` equals `<reviewed-merge-sha>`",
    );
    expect(rehearsal).toContain("does not replace the dispatched run");
  });

  it("requires a dispatched live e2e run as release evidence", () => {
    // The weekly cron is monitoring, not release evidence: v0.14.0 shipped
    // before the workflow had ever fired. The rehearsal must dispatch it on
    // the exact commit being tagged and wait for green.
    expect(releasing).toContain("gh workflow run e2e-disposable-ha.yml");
    expect(releasing).toContain(
      "one green `e2e-disposable-ha.yml` run (dispatched, not the weekly cron) on the commit being tagged",
    );
    const e2e = readFileSync("scripts/e2e/disposable-ha/run.sh", "utf8");
    expect(e2e).toContain("readwrite roundtrip");
    expect(e2e).toContain("secrets.yaml stays unreachable even with readwrite");
  });

  it("keeps the Linux real-machine onboarding lane documented for Linux setup changes", () => {
    expect(releaseDocs).toContain("Linux real-machine onboarding:");
    expect(releaseDocs).toContain("scripts/smoke/linux-headless-setup-check.sh");
    expect(releaseDocs).toContain(
      "by default the helper runs `HA_NOVA_NO_BROWSER=1 ha-nova setup`",
    );
    expect(releaseDocs).toContain("npm run test:desktop:linux:antigravity");
    expect(releaseDocs).toContain(
      "HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup antigravity'",
    );
    expect(releaseDocs).toContain(
      "HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'",
    );
    expect(releaseDocs).toContain(
      "HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup --service hermes'",
    );
    expect(releaseDocs).toContain("HA_NOVA_LIVE_SKIP_INSTALL=1");
    expect(releaseDocs).toContain("Secret Service");
    expect(releaseDocs).toContain("GNOME Keyring");
    expect(releaseDocs).toContain("KWallet Secrets");
    expect(releaseDocs).toContain("provider-owned desktop prompt");
    expect(releaseDocs).toContain(
      "CLI terminal never asks for, reads, or confirms the keyring master password",
    );
    expect(releaseDocs).toContain(
      "validate the fail-closed SSH/headless path separately",
    );
    expect(releaseDocs).toContain("SSH (including X11 forwarding)");
    expect(releaseDocs).toContain("never launch a prompt");
    expect(releaseDocs).toContain("repairable Hermes mismatch");
    expect(releaseDocs).toContain(
      "run `ha-nova setup hermes` and confirm the Hermes route repairs cleanly",
    );
    expect(releaseDocs).toContain(
      "run `ha-nova setup --service hermes`, then `ha-nova doctor`, then one authenticated relay call from a fresh SSH/service-like shell without an unlocked desktop keyring",
    );
    expect(releaseDocs).toContain("confirm the service token file is removed");
    expect(releaseDocs).toContain("Hermes Agent ready now");
    expect(releaseDocs).toContain(
      "when Linux setup or secure-storage behavior changes, the release-bound manual matrix must include the Linux real-machine onboarding lane above",
    );
  });

  it("keeps the Linux live helper generic by default and Hermes-specific only by override", () => {
    expect(linuxHeadlessHelper).toContain(
      "default: HA_NOVA_NO_BROWSER=1 ha-nova setup",
    );
    expect(linuxHeadlessHelper).toContain(
      "HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'",
    );
    expect(pkg.scripts?.["test:desktop:linux:antigravity"]).toContain(
      "linux-headless-setup-check.sh",
    );
    expect(pkg.scripts?.["test:desktop:linux:antigravity"]).toContain(
      "ha-nova setup antigravity",
    );
    expect(linuxHeadlessHelper).toContain("release-lane proof");
    expect(linuxHeadlessHelper).toContain("remote user D-Bus session");
    expect(linuxHeadlessHelper).toContain("remote gdbus");
    expect(linuxHeadlessHelper).toContain(
      'setup_cmd="${HA_NOVA_LIVE_SETUP_CMD:-HA_NOVA_NO_BROWSER=1 ha-nova setup}"',
    );
    expect(linuxHeadlessHelper).not.toContain(
      'setup_cmd="${HA_NOVA_LIVE_SETUP_CMD:-HA_NOVA_NO_BROWSER=1 ha-nova setup hermes}"',
    );
  });
  registerCloudReleaseBuildContractTests();
  registerWorkflowPinsContractTests();
});
