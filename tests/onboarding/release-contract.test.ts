import { existsSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("release contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const installer = readFileSync("install.ps1", "utf8");
  const bundleBuilder = readFileSync("scripts/release/build-install-bundle.sh", "utf8");
  const relayImageVerifier = readFileSync("scripts/release/verify-relay-image.sh", "utf8");
  const releaseAssetVerifier = readFileSync("scripts/release/verify-release-assets.sh", "utf8");
  const censusDeploymentVerifier = readFileSync(
    "scripts/release/verify-census-deployment.sh",
    "utf8",
  );
  const censusDeployer = readFileSync(
    "scripts/release/deploy-census-worker.sh",
    "utf8",
  );
  const releaseWorkflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(".github/workflows/release-candidate.yml", "utf8");
  const censusWorkerReadme = readFileSync("census-worker/README.md", "utf8");
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const readme = readFileSync("README.md", "utf8");
  const linuxHeadlessHelper = readFileSync("scripts/smoke/linux-headless-setup-check.sh", "utf8");
  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    dependencies?: Record<string, string>;
    devDependencies?: Record<string, string>;
    scripts?: Record<string, string>;
  };

  it("keeps local RC packaging focused on install bundles only", () => {
    expect(pkg.scripts?.["release:rc:local"]).toBe(
      "goreleaser build --snapshot --clean && bash scripts/release/build-install-bundle.sh"
    );
    expect(pkg.scripts?.["release:winget:stage-submission"]).toBeUndefined();
    expect(existsSync("scripts/release/build-install-bundle.sh")).toBe(true);
    expect(existsSync("scripts/release/build-winget-manifest.sh")).toBe(false);
    expect(existsSync("scripts/release/prepare-winget-pkgs-submission.sh")).toBe(false);
    expect(existsSync("release/winget-publication-state.json")).toBe(false);
  });

  it("builds Unix install bundles without macOS copyfile metadata noise", () => {
    expect(bundleBuilder).toContain('COPYFILE_DISABLE=1 tar --format ustar -czf "${output}" -C "${stage_dir}" ha-nova');
  });

  it("ships the privacy document targeted by bundled relative links", () => {
    expect(bundleBuilder).toContain('cp "${ROOT_DIR}/PRIVACY.md" "${bundle_root}/PRIVACY.md"');
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
      "curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash"
    );
    expect(readme).toContain(
      "irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex"
    );
    expect(readme).toContain("The installer selects the latest stable release.");
    expect(readme).not.toMatch(/HA_NOVA_VERSION=v\d/);
    expect(readme).not.toMatch(/\$env:HA_NOVA_VERSION\s*=\s*['\"]v\d/);
  });

  it("pins the GoReleaser release tag to the triggering workflow ref", () => {
    expect(releaseWorkflow).toContain("GORELEASER_CURRENT_TAG: ${{ github.ref_name }}");
  });

  it("keeps releases private until every required asset is present", () => {
    expect(goreleaser).toMatch(/^\s*draft:\s*true\s*$/m);
    expect(goreleaser).toMatch(/^\s*replace_existing_draft:\s*true\s*$/m);
    expect(goreleaser).toMatch(/^\s*mode:\s*replace\s*$/m);
    expect(releaseWorkflow).toContain("publish-release:");
    expect(releaseWorkflow).toContain("name: Verify complete draft and publish");
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
    expect(releaseWorkflow.match(/verify-release-assets\.sh "\$GITHUB_REF_NAME"/g)).toHaveLength(2);
    expect(releaseAssetVerifier).toContain('repository="markusleben/ha-nova"');
    expect(releaseAssetVerifier).toContain("(.assets | length) == ($expected | length)");
    expect(releaseAssetVerifier).toContain('([.assets[].name] | sort) == $expected');
    expect(releaseAssetVerifier).toContain('.state == "uploaded"');
    expect(releaseAssetVerifier).toContain('.size | type == "number" and . > 0');
    expect(releaseAssetVerifier).toContain('test("^sha256:[0-9a-f]{64}$")');
    expect(releaseWorkflow).toContain('gh release edit "$GITHUB_REF_NAME" --draft=false');
    expect(releaseWorkflow).toMatch(
      /if \[\[ "\$expected_prerelease" == "true" \]\]; then[\s\S]*?else\s+# Re-assert Latest[\s\S]*?gh release edit "\$GITHUB_REF_NAME" --draft=false --latest --verify-tag\s+fi/,
    );
    expect(releaseWorkflow).toContain(
      "gh api 'repos/markusleben/ha-nova/releases/latest' --jq '.tag_name'",
    );
    expect(releaseWorkflow).toContain('[[ "$latest_tag" != "$GITHUB_REF_NAME" ]]');
    expect(releaseWorkflow).toContain("needs: publish-release");
    expect(releaseWorkflow.indexOf("Upload install bundles")).toBeLessThan(
      releaseWorkflow.indexOf("publish-release:"),
    );
    expect(releaseWorkflow.indexOf("publish-release:")).toBeLessThan(
      releaseWorkflow.indexOf("smoke-installers:"),
    );
  });

  it("uses only the curated release header as the changelog", () => {
    expect(goreleaser).toMatch(/changelog:\n\s+#[^\n]*\n(?:\s+#[^\n]*\n)*\s+disable: true/);
    expect(goreleaser).not.toContain("regexp: '^feat");
    expect(goreleaser).not.toContain("regexp: '^fix");
  });

  it("keeps the next-release-version check immune to release-payload growth", () => {
    // The default spawnSync maxBuffer (1 MiB) failed the v0.14.0 publish with
    // ENOBUFS once the release list outgrew it. The gh call must project the
    // payload down to the fields it reads and carry an explicit buffer.
    const versionCheck = readFileSync("scripts/release/verify-next-release-version.sh", "utf8");
    expect(versionCheck).toContain('".[] | {tag_name, draft, prerelease}"');
    expect(versionCheck).toContain("maxBuffer: 64 * 1024 * 1024");
  });

  it("keeps the RC workflow free of winget artifacts and guidance", () => {
    expect(rcWorkflow).toContain("Build install bundles");
    expect(rcWorkflow).toContain("Upload RC artifacts");
    expect(rcWorkflow).toContain("Smoke Windows bundle");
    expect(rcWorkflow).not.toContain("Build winget manifests");
    expect(rcWorkflow).not.toContain("Upload RC winget manifests");
    expect(rcWorkflow).not.toContain("Download RC winget manifests");
    expect(rcWorkflow).not.toContain("winget validate");
    expect(rcWorkflow).not.toContain("$ProgressPreference = 'SilentlyContinue'");
    expect(rcWorkflow).not.toContain("dist/winget");
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
    expect(goreleaser).toContain("{{ if .Prerelease }}## Preview / Release Candidate");
    expect(goreleaser).toContain("Stable users should ignore this prerelease");
  });

  it("keeps the Windows installer bundle-managed while preserving quiet download UX", () => {
    expect(installer).toContain("Invoke-DownloadFile");
    expect(installer).toContain('$global:ProgressPreference = "SilentlyContinue"');
    expect(installer).not.toContain("Stop-ForWingetInstall");
    expect(installer).not.toContain("Get-Command winget");
    expect(installer).not.toContain("winget upgrade --id");
    expect(installer).not.toContain("winget uninstall --id");
  });

  it("keeps release docs aligned to the pinned stable install contract", () => {
    expect(releasing).toContain("Windows uses a single supported install path: `install.ps1`");
    expect(releasing).toContain("Supported stable selection:");
    expect(releasing).toContain("curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z bash");
    expect(releasing).toContain("irm https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.ps1 | iex");
    expect(releasing).toContain("stable release notes must publish tag-pinned install commands, never `main` bootstrap URLs");
    expect(releasing).toContain("npm run dev:validation:harness");
    expect(releasing).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(releasing).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(releasing).not.toContain("winget");
    expect(releasing).not.toContain("$ProgressPreference = 'SilentlyContinue'");
  });

  it("documents the mandatory tag-first release rehearsal and its drift guard", () => {
    expect(releasing).toContain("tag-first dress rehearsal");
    expect(releasing).toContain("bash scripts/release/verify-release-pipeline.sh");
    expect(releasing).toContain("HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1");
    expect(releasing).toContain("release-pipeline-audit.yml");
    expect(releasing).toContain("GORELEASER_CURRENT_TAG");
  });

  it("gates a Relay release on the exact successful push run and immutable GHCR digest", () => {
    expect(relayImageVerifier).toContain("^[0-9a-f]{40}$");
    expect(relayImageVerifier).toContain("actions/workflows/relay-image.yml/runs");
    expect(relayImageVerifier).toContain('head_sha=${commit_sha}');
    expect(relayImageVerifier).toContain("-f branch=main");
    expect(relayImageVerifier).toContain("-f event=push");
    expect(relayImageVerifier).toContain("-f status=success");
    expect(relayImageVerifier).toContain('.head_sha == $sha');
    expect(relayImageVerifier).toContain('.head_branch == "main"');
    expect(relayImageVerifier).toContain('.conclusion == "success"');
    expect(relayImageVerifier).toContain(
      'latest_ref="${image_repository}:latest"',
    );
    expect(relayImageVerifier).toContain(
      'version_ref="${image_repository}:${relay_version}"',
    );
    expect(relayImageVerifier).toContain('sha_ref="${image_repository}:sha-${commit_sha}"');
    expect(relayImageVerifier).toContain("docker buildx imagetools inspect");
    expect(relayImageVerifier).toContain(".manifest.digest");
    expect(relayImageVerifier).toContain('[[ "$latest_digest" == "$version_digest" ]]');
    expect(relayImageVerifier).toContain('[[ "$version_digest" == "$sha_digest" ]]');
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
    expect(relayImageVerifier).toContain('["build-arg:RELAY_VERSION"] == $version');
    expect(relayImageVerifier).toContain('["label:org.opencontainers.image.version"] == $version');
  });

  it("keeps the census deployment gate read-only, cache-busted, and first-launch aware", () => {
    expect(censusDeploymentVerifier).toContain(
      "https://ha-nova-census.markusleben.workers.dev/stats",
    );
    expect(censusDeploymentVerifier).toContain("cache_busted_url=");
    expect(censusDeploymentVerifier).toContain('"$cache_busted_url"');
    expect(censusDeploymentVerifier).toContain("curl --disable --request GET");
    expect(censusDeploymentVerifier).toContain("--proto '=https'");
    expect(censusDeploymentVerifier).toContain('"$deployment_sha" == "$expected_sha"');
    expect(censusDeploymentVerifier).toContain('"$version_id" == "$expected_version_id"');
    expect(censusDeploymentVerifier).toContain("x-ha-nova-deployment-sha:");
    expect(censusDeploymentVerifier).toContain("x-ha-nova-version-id:");
    expect(censusDeploymentVerifier).toContain(".schema == 1");
    expect(censusDeploymentVerifier).toContain(".window_weeks == 4");
    expect(censusDeploymentVerifier).toContain("all(.by_os[];");
    expect(censusDeploymentVerifier).toContain("all(.by_version[];");
    expect(censusDeploymentVerifier).toContain("all(.by_relay[];");
    expect(censusDeploymentVerifier).toContain('. >= 0 and floor == .');
    expect(censusDeploymentVerifier).toContain(".peak_weekly_pings");
    expect(censusDeploymentVerifier).toContain('monthly_lower_bound');
    expect(censusDeploymentVerifier).toContain('contains("not verified unique installs")');
    expect(censusDeploymentVerifier).toContain("--require-empty");
    expect(censusDeploymentVerifier).toContain("(.weekly | length) == 0");
    expect(censusDeploymentVerifier).toContain("(.by_os | length) == 0");
    expect(censusDeploymentVerifier).toContain("(.by_version | length) == 0");
    expect(censusDeploymentVerifier).toContain("(.by_relay | length) == 0");
    expect(censusDeploymentVerifier).toContain(".peak_weekly_pings == 0");
    expect(censusDeploymentVerifier).not.toContain("/ping");
    expect(censusDeploymentVerifier).not.toMatch(/(?:--request|-X)\s+POST/);
    expect(censusDeploymentVerifier).not.toContain("--data ");
  });

  it("deploys the census only through one exact-target fail-closed wrapper", () => {
    expect(censusDeployer).toContain("set -euo pipefail");
    expect(censusDeployer).toContain('status --porcelain');
    expect(censusDeployer).toContain('rev-parse HEAD');
    expect(censusDeployer).toContain(
      'repos/markusleben/ha-nova/compare/${reviewed_sha}...main',
    );
    expect(censusDeployer).toContain("gh auth status --hostname github.com");
    expect(censusDeployer).toContain("gh api --hostname github.com");
    expect(censusDeployer).toContain(".merge_base_commit.sha == $sha");
    expect(censusDeployer).toContain("Node.js 22 or newer");
    expect(censusDeployer).toContain("npx --yes wrangler@4.113.0 dev");
    expect(censusDeployer).toContain("--local");
    expect(censusDeployer).toContain("--persist-to");
    expect(censusDeployer).toContain("--request POST");
    expect(censusDeployer).toContain('"0.0.0"');
    expect(censusDeployer).toContain("CLOUDFLARE_ACCOUNT_ID");
    expect(censusDeployer).toContain('expected_worker="ha-nova-census"');
    expect(censusDeployer).toContain(
      'expected_target="https://ha-nova-census.markusleben.workers.dev"',
    );
    expect(censusDeployer).toContain("WRANGLER_OUTPUT_FILE_PATH");
    expect(censusDeployer).toContain("$deploys[0].targets == [$target]");
    expect(censusDeployer).toContain('--tag "$reviewed_sha"');
    expect(censusDeployer).toContain("--strict");
    expect(censusDeployer).toContain("--no-autoconfig");
    expect(censusDeployer).toContain("verify-census-deployment.sh");
  });

  it("orders external publication gates around the RC and final tag", () => {
    const rehearsal = releasing.slice(
      releasing.indexOf("**Rehearsal steps.**"),
      releasing.indexOf("The weekly `release-pipeline-audit.yml`"),
    );
    const orderedMarkers = [
      "Merge the reviewed PR state",
      "verify-relay-image.sh <reviewed-merge-sha> <relay-version>",
      "HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1",
      "production census Worker is still the old reviewed deployment",
      "Verify the published RC over the real",
      "deploy-census-worker.sh <reviewed-merge-sha> --require-empty",
      "cut the final tag",
    ];
    let previousIndex = -1;
    for (const marker of orderedMarkers) {
      const markerIndex = rehearsal.indexOf(marker);
      expect(markerIndex, `missing release-order marker: ${marker}`).toBeGreaterThan(
        previousIndex,
      );
      previousIndex = markerIndex;
    }
    expect(rehearsal).toContain("whose `headSha` equals `<reviewed-merge-sha>`");
    expect(rehearsal).toContain("does not replace the dispatched run");
  });

  it("pins reproducible census Worker deployments without adding an unlocked CLI dependency", () => {
    const pinnedDeploy = "npx --yes wrangler@4.113.0 deploy";
    expect(censusDeployer).toContain(pinnedDeploy);
    expect(releasing).toContain(
      "bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha> --require-empty",
    );
    expect(censusWorkerReadme).toContain(
      "bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha> --require-empty",
    );
    expect(releasing).toContain("requires Node.js 22 or newer");
    expect(censusWorkerReadme).toContain("requires a clean checkout at that exact SHA, Node.js 22 or newer");
    expect(releasing).not.toMatch(/npx(?:\s+--yes)?\s+wrangler\s+deploy/);
    expect(censusWorkerReadme).not.toMatch(/npx(?:\s+--yes)?\s+wrangler\s+deploy/);
    expect(releasing).toContain("steps 1–2 of the rehearsal completed");
    expect(pkg.dependencies?.["wrangler"]).toBeUndefined();
    expect(pkg.devDependencies?.["wrangler"]).toBeUndefined();
  });

  it("requires a dispatched live e2e run as release evidence", () => {
    // The weekly cron is monitoring, not release evidence: v0.14.0 shipped
    // before the workflow had ever fired. The rehearsal must dispatch it on
    // the exact commit being tagged and wait for green.
    expect(releasing).toContain("gh workflow run e2e-disposable-ha.yml");
    expect(releasing).toContain("one green `e2e-disposable-ha.yml` run (dispatched, not the weekly cron) on the commit being tagged");
    const e2e = readFileSync("scripts/e2e/disposable-ha/run.sh", "utf8");
    expect(e2e).toContain("readwrite roundtrip");
    expect(e2e).toContain("secrets.yaml stays unreachable even with readwrite");
  });

  it("keeps the Linux real-machine onboarding lane documented for Linux setup changes", () => {
    expect(releasing).toContain("Linux real-machine onboarding:");
    expect(releasing).toContain("scripts/smoke/linux-headless-setup-check.sh");
    expect(releasing).toContain("by default the helper runs `HA_NOVA_NO_BROWSER=1 ha-nova setup`");
    expect(releasing).toContain("npm run test:desktop:linux:antigravity");
    expect(releasing).toContain("HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup antigravity'");
    expect(releasing).toContain("HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'");
    expect(releasing).toContain("HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup --service hermes'");
    expect(releasing).toContain("HA_NOVA_LIVE_SKIP_INSTALL=1");
    expect(releasing).toContain("Secret Service");
    expect(releasing).toContain("GNOME Keyring");
    expect(releasing).toContain("non-GNOME");
    expect(releasing).toContain("local Linux keyring password");
    expect(releasing).toContain("headlessly over SSH");
    expect(releasing).toContain("repairable Hermes mismatch");
    expect(releasing).toContain("run `ha-nova setup hermes` and confirm the Hermes route repairs cleanly");
    expect(releasing).toContain("run `ha-nova setup --service hermes`, then `ha-nova doctor`, then one authenticated relay call from a fresh SSH/service-like shell without an unlocked desktop keyring");
    expect(releasing).toContain("confirm the service token file is removed");
    expect(releasing).toContain("Hermes Agent ready now");
    expect(releasing).toContain("when Linux setup or secure-storage behavior changes, the release-bound manual matrix must include the Linux real-machine onboarding lane above");
  });

  it("keeps the Linux live helper generic by default and Hermes-specific only by override", () => {
    expect(linuxHeadlessHelper).toContain("default: HA_NOVA_NO_BROWSER=1 ha-nova setup");
    expect(linuxHeadlessHelper).toContain("HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'");
    expect(pkg.scripts?.["test:desktop:linux:antigravity"]).toContain("linux-headless-setup-check.sh");
    expect(pkg.scripts?.["test:desktop:linux:antigravity"]).toContain("ha-nova setup antigravity");
    expect(linuxHeadlessHelper).toContain("release-lane proof");
    expect(linuxHeadlessHelper).toContain("remote user D-Bus session");
    expect(linuxHeadlessHelper).toContain("remote gdbus");
    expect(linuxHeadlessHelper).toContain(
      'setup_cmd="${HA_NOVA_LIVE_SETUP_CMD:-HA_NOVA_NO_BROWSER=1 ha-nova setup}"'
    );
    expect(linuxHeadlessHelper).not.toContain(
      'setup_cmd="${HA_NOVA_LIVE_SETUP_CMD:-HA_NOVA_NO_BROWSER=1 ha-nova setup hermes}"'
    );
  });
});
