import { existsSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("release contract", () => {
  const ciWorkflow = readFileSync(".github/workflows/ci.yml", "utf8");
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const workflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(".github/workflows/release-candidate.yml", "utf8");
  const prTemplate = readFileSync(".github/PULL_REQUEST_TEMPLATE.md", "utf8");
  const bundleBuilder = readFileSync("scripts/release/build-install-bundle.sh", "utf8");
  const wingetManifestBuilder = readFileSync("scripts/release/build-winget-manifest.sh", "utf8");
  const wingetSubmissionHelper = readFileSync("scripts/release/prepare-winget-pkgs-submission.sh", "utf8");
  const onboardingLifecycleSpec = readFileSync("docs/superpowers/specs/2026-03-22-onboarding-lifecycle-implementation.md", "utf8");
  const windowsDistributionUxSpec = readFileSync("docs/superpowers/specs/2026-03-22-windows-distribution-ux-review.md", "utf8");
  const activeContractCleanupSpec = readFileSync("docs/superpowers/specs/2026-03-23-active-onboarding-contract-cleanup.md", "utf8");
  const wingetValidationCleanupSpec = readFileSync("docs/superpowers/specs/2026-03-23-winget-validation-warning-cleanup.md", "utf8");
  const pythonRunner = readFileSync("scripts/e2e/run-python-script.mjs", "utf8");
  const expectedGoreleaserActionRef = "goreleaser/goreleaser-action@v7";
  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    scripts?: Record<string, string>;
  };

  it("builds ha-nova binaries instead of relay-named release artifacts", () => {
    expect(goreleaser).toContain("project_name: ha-nova");
    expect(goreleaser).toContain("binary: ha-nova");
    expect(goreleaser).toContain('name_template: "ha-nova-{{ .Os }}-{{ .Arch }}"');
    expect(goreleaser).not.toContain("binary: relay");
  });

  it("publishes release notes with the Go-first public commands", () => {
    expect(goreleaser).toContain("## Why This Release Exists");
    expect(goreleaser).toContain("## What You Get");
    expect(goreleaser).toContain("install.sh");
    expect(goreleaser).toContain("install.ps1");
    expect(goreleaser).toContain("$ProgressPreference = 'SilentlyContinue'");
    expect(goreleaser).toContain("## Upgrade Notes");
    expect(goreleaser).toContain("validated install and client flows for this release");
    expect(goreleaser).toContain("ha-nova update");
    expect(goreleaser).toContain("ha-nova check-update");
    expect(goreleaser).toContain("Claude shipped installs use the local staged HA NOVA release payload on disk.");
    expect(goreleaser).toContain("run `ha-nova update` and then restart Claude");
    expect(goreleaser).toContain("A `winget` manifest is attached as a release handoff artifact");
    expect(goreleaser).toContain("the public Windows package is not live until that manifest is published and proven on a fresh Windows VM.");
    expect(goreleaser).toContain("Default `ha-nova uninstall` is standard remove.");
    expect(goreleaser).toContain("Use `ha-nova uninstall --purge` for a full local wipe.");
    expect(goreleaser).toContain("%APPDATA%\\ha-nova");
    expect(goreleaser).toContain("%LOCALAPPDATA%\\ha-nova\\cache");
    expect(goreleaser).toContain("Keep exactly one Windows install channel per machine.");
    expect(goreleaser).toContain("Legacy pre-Go installs still require the legacy cleanup script before reinstalling.");
    expect(goreleaser).toContain("Do not download and run the raw `ha-nova-installer-bundle-*.tar.gz` / `.zip` assets directly;");
    expect(goreleaser).not.toContain("~/.config/ha-nova/update");
    expect(goreleaser).not.toContain("HA_NOVA_VERSION=");
    expect(goreleaser).not.toContain("winget install --id markusleben.ha-nova --exact");
  });

  it("groups release notes into user-facing sections instead of a flat commit dump", () => {
    expect(goreleaser).toContain("changelog:");
    expect(goreleaser).toContain("title: New Features");
    expect(goreleaser).toContain("regexp: '^feat(\\(.+\\))?!?:.+$'");
    expect(goreleaser).toContain("title: Bug Fixes");
    expect(goreleaser).toContain("regexp: '^fix(\\(.+\\))?!?:.+$'");
    expect(goreleaser).toContain('      - "^Merge "');
    expect(goreleaser).toContain("^(docs|refactor|perf|style|build|ci|chore|test)(\\(.+\\))?!?:.+$");
    expect(goreleaser).not.toContain("^[^:]+:.*$");
    expect(goreleaser).not.toContain("title: UX, Docs, and Refactors");
    expect(goreleaser).not.toContain("title: Internal Maintenance");
    expect(goreleaser).not.toContain("title: Other Changes");
  });

  it("builds macOS, Linux, and Windows install bundles with bundle metadata", () => {
    expect(bundleBuilder).toContain("ha-nova-installer-bundle-macos");
    expect(bundleBuilder).toContain("ha-nova-installer-bundle-linux");
    expect(bundleBuilder).toContain("ha-nova-installer-bundle-windows");
    expect(bundleBuilder).toContain("bundle.json");
    expect(bundleBuilder).toContain("compute_sha256()");
    expect(bundleBuilder).toContain("sha256sum or shasum");
    expect(bundleBuilder).toContain('cp -R "${ROOT_DIR}/clients" "${bundle_root}/clients"');
    expect(bundleBuilder).toContain(".sha256");
    expect(bundleBuilder).toContain('"binary_name": "${binary_name}"');
    expect(bundleBuilder).not.toContain('cp -R "${ROOT_DIR}/scripts"');
    expect(bundleBuilder).not.toContain('cp "${ROOT_DIR}/install.sh"');
  });

  it("keeps legacy cleanup scripts outside the steady-state install bundle", () => {
    expect(bundleBuilder).not.toContain("legacy-uninstall.sh");
    expect(bundleBuilder).not.toContain("legacy-uninstall.ps1");
  });

  it("builds a winget manifest from the published Windows bundle instead of a raw bootstrap script", () => {
    expect(wingetManifestBuilder).toContain("ha-nova-installer-bundle-windows-amd64.zip");
    expect(wingetManifestBuilder).toContain('MANIFEST_VERSION="${MANIFEST_VERSION:-1.9.0}"');
    expect(wingetManifestBuilder).toContain("PackageIdentifier: ${PACKAGE_IDENTIFIER}");
    expect(wingetManifestBuilder).toContain("InstallerType: zip");
    expect(wingetManifestBuilder).toContain("NestedInstallerType: portable");
    expect(wingetManifestBuilder).toContain("yaml-language-server");
    expect(wingetManifestBuilder).toContain("# Created by HA NOVA release automation");
    expect(wingetManifestBuilder).toContain("RelativeFilePath: ha-nova/ha-nova.exe");
    expect(wingetManifestBuilder).toContain("PortableCommandAlias: ha-nova");
    expect(wingetManifestBuilder).toContain("ha-nova-winget-manifest-");
    expect(wingetManifestBuilder).not.toContain("Scope: user");
    expect(wingetManifestBuilder).not.toContain("install.ps1");
  });

  it("stages a public winget-pkgs submission payload from the generated manifest archive", () => {
    expect(wingetSubmissionHelper).toContain("ha-nova-winget-manifest-v");
    expect(wingetSubmissionHelper).toContain('WINGET_STAGE_SOURCE="${WINGET_STAGE_SOURCE:-release_asset}"');
    expect(wingetSubmissionHelper).toContain("WINGET_STAGE_SOURCE=release_asset");
    expect(wingetSubmissionHelper).toContain("Expected release_asset or local_dist.");
    expect(wingetSubmissionHelper).toContain("gh release download");
    expect(wingetSubmissionHelper).toContain("winget validate --manifest");
    expect(wingetSubmissionHelper).toContain("without warnings");
    expect(wingetSubmissionHelper).toContain("microsoft/winget-pkgs");
    expect(wingetSubmissionHelper).toContain("InstallerUrl mismatch");
    expect(wingetSubmissionHelper).toContain("winget-pkgs-maintainer-checklist.md");
    expect(wingetSubmissionHelper).toContain("winget-pkgs-pr-body.md");
    expect(wingetSubmissionHelper).toContain("winget-pkgs-copy-path.txt");
    expect(wingetSubmissionHelper).toContain("winget-pkgs-gh-commands.md");
    expect(wingetSubmissionHelper).toContain("UPSTREAM_BASE_BRANCH");
    expect(wingetSubmissionHelper).toContain("FORK_REPO");
    expect(wingetSubmissionHelper).toContain('printf \'%s\\n\' "${raw#v}"');
    expect(wingetSubmissionHelper).toContain('InstallerSha256 mismatch. Expected');
    expect(wingetSubmissionHelper).toContain('Bundle SHA sidecar mismatch.');
    expect(wingetSubmissionHelper).toContain("winget show --id ${PACKAGE_IDENTIFIER} --exact --source winget");
    expect(wingetSubmissionHelper).toContain("## Initial Published-Source Proof");
    expect(wingetSubmissionHelper).toContain("## Upgrade Continuity Proof");
    expect(wingetSubmissionHelper).toContain("no longer resolves from the removed install");
    expect(wingetSubmissionHelper).toContain("If this is the first public \\`winget\\` publication");
    expect(wingetSubmissionHelper).toContain("<staged-submission-root-on-your-host>");
    expect(wingetSubmissionHelper).toContain("<staged-manifest-dir-on-your-validation-host>");
    expect(wingetSubmissionHelper).toContain('STAGED_ROOT="<set-this-to-your-staged-submission-root>"');
    expect(wingetSubmissionHelper).toContain('$StagedRoot = "<set-this-to-your-staged-submission-root>"');
    expect(pkg.scripts?.["release:winget:stage-submission"]).toBe("bash scripts/release/prepare-winget-pkgs-submission.sh");
  });

  it("runs the canonical verify command before publishing and smoke-checks install, update, and uninstall afterwards", () => {
    expect(workflow).toContain("actions/setup-node@v6");
    expect(workflow).toContain('node-version: "20"');
    expect(workflow).toContain(expectedGoreleaserActionRef);
    expect(workflow).toContain("npm ci");
    expect(workflow).toContain("npm run verify");
    expect(pkg.scripts?.verify).toContain("verify:release-metadata");
    expect(workflow).toContain("Verify release metadata");
    expect(workflow).toContain('verify-release-metadata.sh "${GITHUB_REF_NAME}"');
    expect(workflow).toContain("Verify next release version");
    expect(workflow).toContain('verify-next-release-version.sh "${GITHUB_REF_NAME}"');
    expect(workflow).toContain("GH_TOKEN: ${{ github.token }}");
    expect(workflow).toContain('HA_NOVA_ALLOW_EXISTING_RELEASE_TAG: "1"');
    expect(workflow).toContain("environment:");
    expect(workflow).toContain("name: production");
    expect(workflow).toContain("Build install bundles");
    expect(workflow).toContain("Build winget manifests");
    expect(workflow).toContain("Upload install bundles");
    expect(workflow).toContain("Upload winget manifests");
    expect(workflow).toContain("Upload release winget manifests artifact");
    expect(workflow).toContain("actions/upload-artifact@v7");
    expect(workflow).toContain("dist/install-bundles");
    expect(workflow).toContain("dist/winget/*.zip");
    expect(workflow).toContain("Post-publish smoke installers");
    expect(workflow).toContain("install.ps1");
    expect(workflow).toContain("bash install.sh");
    expect(workflow).toContain("Programs\\ha-nova\\ha-nova.exe");
    expect(workflow).toContain("Expected Windows runtime at");
    expect(workflow).toContain("check-update --quiet");
    expect(workflow).toContain("update --version");
    expect(workflow).toContain("uninstall --yes");
    expect(workflow).toContain("Start-Sleep -Milliseconds 500");
    expect(workflow).toContain("uninstall-status.json");
    expect(workflow).toContain("Windows uninstall did not complete cleanly");
    expect(workflow).toContain("Download release winget manifests");
    expect(workflow).toContain("actions/download-artifact@v8");
    expect(workflow).toContain("Validate Windows winget manifest");
    expect(workflow).toContain("winget validate --manifest $manifestDir");
    expect(workflow).toContain("winget validate emitted warnings");
  });

  it("keeps release signing policy consistent with the active workflow", () => {
    expect(workflow).toContain("--skip=sign");
    expect(goreleaser).not.toContain("\nsigns:\n");
    expect(goreleaser).not.toContain("codesign");
  });

  it("keeps npm run verify aligned with CI-safe release gates", () => {
    expect(pkg.scripts?.verify).toContain("npm run verify:release-metadata");
    expect(pkg.scripts?.verify).toContain("npm run typecheck");
    expect(pkg.scripts?.verify).toContain("npm run test:safe");
    expect(pkg.scripts?.verify).toContain("npm run build");
    expect(pkg.scripts?.verify).toContain("bash scripts/check-docs.sh");
    expect(pkg.scripts?.verify).toContain("npm run test:cli");
    expect(pkg.scripts?.verify).not.toContain("test:desktop");
  });

  it("keeps the PR template aligned to the canonical verify gate", () => {
    expect(prTemplate).toContain("`npm run verify`");
    expect(prTemplate).not.toContain("`npm run typecheck`");
    expect(prTemplate).not.toContain("`npm test`");
    expect(prTemplate).not.toContain("`cd cli && go test ./...`");
  });

  it("runs release metadata verification in regular PR/main CI before typecheck and tests", () => {
    expect(ciWorkflow).toContain("name: CI");
    expect(ciWorkflow).toContain("name: Verify release metadata");
    expect(ciWorkflow).toContain("bash scripts/release/verify-release-metadata.sh");
    expect(ciWorkflow).toContain("pull_request:");
    expect(ciWorkflow).toContain("branches:");
    expect(ciWorkflow).toContain("- main");
  });

  it("defines a manual release-candidate workflow that can optionally publish bundle-based prereleases", () => {
    expect(rcWorkflow).toContain("workflow_dispatch:");
    expect(rcWorkflow).toContain("publish_release");
    expect(rcWorkflow).toContain("version_tag");
    expect(rcWorkflow).toContain("actions/setup-node@v6");
    expect(rcWorkflow).toContain("node-version: 20");
    expect(rcWorkflow).toContain("npm ci");
    expect(rcWorkflow).toContain("npm run verify");
    expect(rcWorkflow).toContain("Verify release metadata");
    expect(rcWorkflow).toContain("verify-release-metadata.sh");
    expect(rcWorkflow).toContain(expectedGoreleaserActionRef);
    expect(rcWorkflow).toContain("args: build --snapshot --clean");
    expect(rcWorkflow).toContain("Build install bundles");
    expect(rcWorkflow).toContain("Build winget manifests");
    expect(rcWorkflow).toContain("bash scripts/release/build-winget-manifest.sh");
    expect(rcWorkflow).toContain('"${GITHUB_REPOSITORY}"');
    expect(rcWorkflow).toContain('"${VERSION_TAG}"');
    expect(rcWorkflow).toContain('bash scripts/release/build-install-bundle.sh "${VERSION_TAG#v}"');
    expect(rcWorkflow).toContain("Build install bundles");
    expect(rcWorkflow).toContain("Upload RC artifacts");
    expect(rcWorkflow).toContain("Upload RC winget manifests");
    expect(rcWorkflow).toContain("Download RC winget manifests");
    expect(rcWorkflow).toContain("dist/winget/manifests/**");
    expect(rcWorkflow).toContain("dist/winget/*.zip");
    expect(rcWorkflow).toContain("Smoke bundles");
    expect(rcWorkflow).toContain("version_tag must match vX.Y.Z-rcN");
    expect(rcWorkflow).toContain("Verify next release version");
    expect(rcWorkflow).toContain('verify-next-release-version.sh "${VERSION_TAG}"');
    expect(rcWorkflow).toContain("GH_TOKEN: ${{ github.token }}");
    expect(rcWorkflow).toContain('HA_NOVA_ALLOW_EXISTING_RELEASE_TAG: "1"');
    expect(rcWorkflow.indexOf("name: Checkout")).toBeLessThan(rcWorkflow.indexOf("name: Verify next release version"));
    expect(rcWorkflow).toContain("actions/upload-artifact@v7");
    expect(rcWorkflow).toContain("actions/download-artifact@v8");
    expect(rcWorkflow).toContain("gh release create");
    expect(rcWorkflow).toContain("--prerelease");
    expect(rcWorkflow).toContain("assets=(dist/install-bundles/* dist/winget/*.zip dist/winget/*.sha256)");
    expect(rcWorkflow).toContain('install_ref="${GITHUB_SHA}"');
    expect(rcWorkflow).not.toContain('install_ref="${GITHUB_REF_NAME}"');
    expect(rcWorkflow).toContain("raw.githubusercontent.com/markusleben/ha-nova/");
    expect(rcWorkflow).toContain("Stable users should ignore this prerelease.");
    expect(rcWorkflow).toContain("install.sh | HA_NOVA_VERSION=");
    expect(rcWorkflow).toContain("install.ps1");
    expect(rcWorkflow).toContain("Public Windows path stays install.ps1 until the winget package is published and proven on a fresh Windows VM.");
    expect(rcWorkflow).toContain("On Windows, RC testing uses the installer commands above.");
    expect(rcWorkflow).toContain("Do not treat the normal Windows stable path as an RC path.");
    expect(rcWorkflow).toContain("Claude shipped installs use the local staged HA NOVA release payload on disk.");
    expect(rcWorkflow).toContain("run ha-nova update and then restart Claude");
    expect(rcWorkflow).toContain("ha-nova update --version");
    expect(rcWorkflow).toContain("Installed runtime:");
    expect(rcWorkflow).toContain("Return to stable later:");
    expect(rcWorkflow).toContain("Default ha-nova uninstall is standard remove; use ha-nova uninstall --purge for a full local wipe.");
    expect(rcWorkflow).toContain("Windows now uses %APPDATA%\\\\ha-nova and %LOCALAPPDATA%\\\\ha-nova\\\\cache as the canonical config and cache paths.");
    expect(rcWorkflow).toContain("Keep exactly one Windows install channel per machine.");
    expect(rcWorkflow).toContain("The attached winget manifest artifact is a rehearsal handoff, not a live install channel and not the final public submission payload.");
    expect(rcWorkflow).toContain("use the attached winget artifact only for rehearsal/validation");
    expect(rcWorkflow).toContain("The real public winget-pkgs submission must be restaged later from the exact final stable release artifact.");
    expect(rcWorkflow).toContain("Validate Windows winget manifest");
    expect(rcWorkflow).toContain("winget validate --manifest $manifestDir");
    expect(rcWorkflow).toContain("winget validate emitted warnings");
    expect(rcWorkflow).toContain("fresh machines/profiles");
    expect(rcWorkflow).toContain("clean VM/snapshot");
    expect(rcWorkflow).not.toContain("goreleaser release");
  });

  it("documents the required GitHub production gate for final release", () => {
    const releasing = readFileSync("docs/releasing.md", "utf8");

    expect(releasing).toContain("Release Notes Structure");
    expect(releasing).toContain("Bulk Release Preflight");
    expect(releasing).toContain("npm run test:bulk:release");
    expect(releasing).toContain("npm run test:bulk:manual:area-review");
    expect(releasing).toContain("maintainer-host bulk gate");
    expect(releasing).toContain("real local HA + relay + Codex environment");
    expect(releasing).toContain("Why This Release Exists");
    expect(releasing).toContain("What You Get");
    expect(releasing).toContain("Upgrade Notes");
    expect(releasing).toContain("New Features");
    expect(releasing).toContain("Bug Fixes");
    expect(releasing).toContain("production");
    expect(releasing).toContain("required reviewers");
    expect(releasing).toContain("prevent self-review");
    expect(releasing).toContain("v*");
    expect(releasing).toContain("Maintainer-only step");
    expect(releasing).toContain("if `required reviewers` is configured");
    expect(releasing).toContain("one direct admin collaborator");
    expect(releasing).toContain("npm run release:rc:local");
    expect(releasing).toContain("Public Winget Handoff");
    expect(releasing).toContain("npm run release:winget:stage-submission");
    expect(releasing).toContain("prepare-winget-pkgs-submission.sh");
    expect(releasing).toContain("For the real public submission, stage it from the exact final stable GitHub release asset.");
    expect(releasing).toContain("Local `dist/` output or RC artifact downloads are rehearsal-only.");
    expect(releasing).toContain("`npm run release:winget:stage-submission` now defaults to `WINGET_STAGE_SOURCE=release_asset`.");
    expect(releasing).toContain("Only use `WINGET_STAGE_SOURCE=local_dist` for private rehearsal or contract validation.");
    expect(releasing).toContain("The helper refuses prerelease tags in `release_asset` mode.");
    expect(releasing).toContain("ha-nova-winget-manifest-<tag>.zip");
    expect(releasing).toContain("winget-pkgs-maintainer-checklist.md");
    expect(releasing).toContain("winget-pkgs-pr-body.md");
    expect(releasing).toContain("winget-pkgs-copy-path.txt");
    expect(releasing).toContain("winget-pkgs-gh-commands.md");
    expect(releasing).toContain("only Claude currently has the extra automatic SessionStart update banner");
    expect(releasing).toContain("Claude shipped installs use the local staged release payload on disk; HA NOVA surfaces update notices and users refresh with `ha-nova update` plus a Claude restart");
    expect(releasing).toContain("Keep `.goreleaser.yml` and RC notes in `release-candidate.yml` aligned");
    expect(releasing).toContain("RC is the pre-publish gate");
    expect(releasing).toContain("`release.yml` smoke runs after publish");
    expect(releasing).toContain("winget validate --manifest <dir>` on Windows and require a warning-free success result");
    expect(releasing).toContain("winget show --id markusleben.ha-nova --exact --source winget");
    expect(releasing).toContain("winget install --id markusleben.ha-nova --exact --source winget");
    expect(releasing).toContain("winget uninstall --id markusleben.ha-nova --exact --source winget");
    expect(releasing).toContain("do not use the local harness");
    expect(releasing).toContain("initial fresh-VM published-source install/check-update/uninstall proof");
    expect(releasing).toContain("Treat public `winget upgrade` as a second proof lane");
    expect(releasing).toContain("previous published `markusleben.ha-nova` version installed");
    expect(releasing).toContain("keep release-note/doc wording conservative about public `winget upgrade` until that continuity proof exists");
    expect(releasing).toContain("winget upgrade --id markusleben.ha-nova --exact --source winget");
    expect(releasing).toContain("release/winget-publication-state.json");
    expect(releasing).toContain('set `publication_phase = "pr_open"`');
    expect(releasing).toContain('set `public_install_proven = true`');
    expect(releasing).toContain('set `public_upgrade_proven = true`');
    expect(releasing).toContain("keep `automation_enabled = false`");
    expect(releasing).toContain("never open a second public `winget` submission while `pending_version` is non-empty");
    expect(releasing).toContain("run the same flow only if Secret Service is available");
    expect(releasing).toContain("if not live-tested, do not call the release fully verified on Linux");
    expect(releasing).toContain("CI smoke alone does not upgrade Linux to full real-machine validation");
    expect(releasing).toContain("Do not switch public Windows docs to `winget` until the actual manifest/release publication is live and the initial fresh-VM published-source proof has passed");
    expect(releasing).toContain("install.ps1`-based until the `winget` manifest is published and proven on a fresh Windows VM");
    expect(releasing).toContain("Do this from the exact commit SHA or immutable tag that published the prerelease, not from a moving branch ref and not from `main`.");
    expect(releasing).toContain("moving branch can fetch newer installer bootstrap code against older RC assets");
    expect(releasing).toContain("<rc-commit-or-tag>");
    expect(releasing).toContain("verify `check-update`, `update`, `uninstall --yes`, `uninstall --yes --purge`, and direct `winget uninstall` never guess silently which channel to mutate");
    expect(releasing).not.toContain("`ha-nova uninstall` must remove only the current Go install");
    expect(releasing).toContain("Windows uninstall continues in the background and that it is safe to close the terminal");
    expect(releasing).toContain("if you force a helper failure during validation, confirm `ha-nova doctor` blocks with the exact recovery command");
    expect(releasing).toContain("warning-free Windows `winget validate`");
    expect(releasing).toContain("polls until `%LOCALAPPDATA%\\\\Programs\\\\ha-nova` is gone and `%LOCALAPPDATA%\\\\ha-nova\\\\uninstall-status.json` is gone");
    expect(releasing).toContain("audit open PRs, especially Dependabot and workflow/release PRs");
    expect(releasing).toContain("## Release Channels (KISS)");
    expect(releasing).toContain("tester-only prerelease shape");
    expect(releasing).toContain("do not add a stored preview/stable channel toggle");
    expect(releasing).toContain("ha-nova update --version vX.Y.Z-rcN");
    expect(releasing).toContain("Return an RC install to stable:");
    expect(releasing).toContain("## Winget Scope");
    expect(releasing).toContain("only `stable` releases are candidates for public `winget` rollout");
    expect(releasing).toContain("On Windows, RC testing uses the installer commands above.");
    expect(releasing).toContain("Do not treat the normal Windows stable path as an RC path.");
    expect(releasing).toContain("<rc-tag>");
    expect(releasing).not.toContain("<rc-branch>");
    expect(releasing).toContain("## RC / Stable Validation Matrix");
    expect(releasing).toContain("keep the matrix small but explicit");
    expect(releasing).toContain("`winget` is not part of RC validation");
    expect(releasing).toContain("Secret Service-capable machine");
    expect(releasing).toContain("fresh machines/profiles");
    expect(releasing).toContain("clean VM/snapshot");
    expect(releasing).not.toContain("fresh machine/profile");
    expect(releasing).toContain("go test ./... -run 'TestCompareReleaseVersions");
    expect(releasing).toContain("TestRunUpdateExactRCFromStableInstallsThatRC");
    expect(releasing).toContain("exact RC install by rerunning the installer with `HA_NOVA_VERSION=vX.Y.Z-rcN`");
    expect(releasing).toContain("exact RC install by rerunning `install.ps1` with `HA_NOVA_VERSION=vX.Y.Z-rcN`");
    expect(releasing).toContain("ha-nova uninstall --yes");
    expect(releasing).toContain("fresh machines/profiles");
    expect(releasing).toContain("clean VM/snapshot");
  });

  it("keeps future-state winget specs distinct from the current public Windows contract", () => {
    expect(onboardingLifecycleSpec).toContain("Future-state Windows primary distribution, after public `winget` publication + proof, is `winget`.");
    expect(onboardingLifecycleSpec).toContain("Current public Windows entrypoint until that publication/proof remains `install.ps1`.");
    expect(windowsDistributionUxSpec).toContain("Choose option 2 as the target architecture");
    expect(windowsDistributionUxSpec).toContain("Until public publication + proof exists, `install.ps1` remains the current public Windows path.");
    expect(activeContractCleanupSpec).toContain("Historical future-state docs must not be read as the current public Windows contract");
    expect(wingetValidationCleanupSpec).toContain("Remove avoidable `winget validate` warnings");
    expect(wingetValidationCleanupSpec).toContain("Add YAML schema headers");
    expect(wingetValidationCleanupSpec).toContain("Drop installer fields that the current portable package shape does not support cleanly");
    expect(wingetValidationCleanupSpec).toContain("warning-free `winget validate` as the expected pre-PR outcome");
  });

  it("keeps bulk release preflight separate from the host-safe verify command", () => {
    const releasing = readFileSync("docs/releasing.md", "utf8");

    expect(releasing).toContain("`npm run verify` stays the canonical automated repo gate.");
    expect(releasing).toContain("It does not talk to a real Home Assistant instance or a live Codex/relay session.");
    expect(releasing).toContain("do not move these live bulk checks into GitHub runner CI");
    expect(releasing).toContain("The deterministic `#87` bulk contract is machine-checked here through the tracked `test:safe` suite");
    expect(pkg.scripts?.verify).not.toContain("test:bulk:fast");
    expect(pkg.scripts?.verify).not.toContain("test:bulk:smoke");
    expect(pkg.scripts?.verify).not.toContain("test:bulk:manual:area-review");
  });

  it("keeps the bulk scenario split and backing assets visible in the release delta", () => {
    expect(pkg.scripts?.["e2e:skill:codex:bulk"]).toContain("run-python-script.mjs");
    expect(pkg.scripts?.["test:bulk:smoke"]).toBe(
      "node scripts/e2e/run-python-script.mjs scripts/e2e/codex-ha-nova-bulk-live-e2e.py prefix_inventory area_inventory label_inventory"
    );
    expect(pkg.scripts?.["test:bulk:manual:area-review"]).toBe(
      "node scripts/e2e/run-python-script.mjs scripts/e2e/codex-ha-nova-bulk-live-e2e.py area_review"
    );
    expect(pkg.scripts?.["test:bulk:release"]).toContain("test:bulk:smoke");
    expect(pkg.scripts?.["test:bulk:release"]).toContain("test:safe");
    for (const file of [
      "scripts/e2e/run-python-script.mjs",
      "scripts/e2e/codex-ha-nova-bulk-live-e2e.py",
      "skills/ha-nova/bulk-patterns.md",
      "skills/ha-nova/config-body-filter.jq",
      "tests/e2e/codex-skill-bulk-live-validator.test.ts",
      "tests/skills/bulk-audit-contract.test.ts",
    ]) {
      const tracked = spawnSync("git", ["ls-files", "--error-unmatch", file], {
        cwd: process.cwd(),
        encoding: "utf8",
      });
      expect(existsSync(file)).toBe(true);
      expect(tracked.status).toBe(0);
    }
  });

  it("documents the Python launcher fallback order used by the bulk runner", () => {
    expect(pythonRunner).toContain('process.platform === "win32"');
    expect(pythonRunner).toContain('[["py", ["-3"]], ["python", []], ["python3", []]]');
    expect(pythonRunner).toContain('[["python3", []], ["python", []], ["py", ["-3"]]]');
    expect(pythonRunner).toContain('spawnSync(command, [...prefixArgs, "--version"]');
    expect(pythonRunner).toContain("if (probe.status !== 0) {");
    expect(pythonRunner).toContain("continue;");
    expect(pythonRunner).toContain("Python 3 runtime not found. Install python3, python, or py -3.");
  });

  it("documents fresh-install smoke as installer -> wizard handoff instead of a separate setup command", () => {
    const releasing = readFileSync("docs/releasing.md", "utf8");
    const smokeSection = releasing.split("### 3. Fresh Install Smoke Matrix")[1]?.split("GitHub RC smoke covers")[0] ?? "";

    expect(smokeSection).toContain("complete the installer-started setup wizard");
    expect(smokeSection).not.toContain("ha-nova setup <client>");
  });

  it("builds install bundles from goreleaser-style nested dist artifacts", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-release-dist-"));
    const outputDir = join(distDir, "install-bundles");
    const artifactDirs = [
      ["ha-nova-darwin_darwin_amd64_v1", "ha-nova"],
      ["ha-nova-darwin_darwin_arm64_v8.0", "ha-nova"],
      ["ha-nova-other_linux_amd64_v1", "ha-nova"],
      ["ha-nova-other_linux_arm64_v8.0", "ha-nova"],
      ["ha-nova-other_windows_amd64_v1", "ha-nova.exe"],
    ] as const;

    for (const [dirName, binaryName] of artifactDirs) {
      const dir = join(distDir, dirName);
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, binaryName), "binary", { mode: 0o755 });
    }

    const result = spawnSync("bash", ["scripts/release/build-install-bundle.sh"], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...process.env,
        DIST_DIR: distDir,
      },
      timeout: 30000,
    });

    expect(result.status).toBe(0);
    const macList = spawnSync("tar", ["-tzf", join(outputDir, "ha-nova-installer-bundle-macos-amd64.tar.gz")], {
      encoding: "utf8",
      timeout: 30000,
    });
    expect(macList.status).toBe(0);
    expect(macList.stdout).toContain("ha-nova/clients/registry.json");
    expect(readFileSync(join(outputDir, "ha-nova-installer-bundle-macos-amd64.tar.gz.sha256"), "utf8")).toContain(
      "ha-nova-installer-bundle-macos-amd64.tar.gz"
    );
    expect(readFileSync(join(outputDir, "ha-nova-installer-bundle-linux-arm64.tar.gz.sha256"), "utf8")).toContain(
      "ha-nova-installer-bundle-linux-arm64.tar.gz"
    );
    expect(readFileSync(join(outputDir, "ha-nova-installer-bundle-windows-amd64.zip.sha256"), "utf8")).toContain(
      "ha-nova-installer-bundle-windows-amd64.zip"
    );
  });

  it("builds a winget manifest tree and archive from the bundled Windows asset", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-dist-"));
    const bundleDir = join(distDir, "install-bundles");
    const manifestRoot = join(distDir, "winget", "manifests", "m", "markusleben", "ha-nova", "0.3.0");

    mkdirSync(bundleDir, { recursive: true });
    writeFileSync(join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip"), "fake-bundle");
    writeFileSync(
      join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip.sha256"),
      "4207F78DA0027952482882209CDF761C1F2846191CAE1C2D21E64693B74A0622  ha-nova-installer-bundle-windows-amd64.zip\n"
    );

    const result = spawnSync(
      "bash",
      ["scripts/release/build-winget-manifest.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: distDir,
        },
        timeout: 30000,
      }
    );

    expect(result.status).toBe(0);
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.installer.yaml"), "utf8")).toContain(
      "InstallerUrl: https://github.com/markusleben/ha-nova/releases/download/v0.3.0/ha-nova-installer-bundle-windows-amd64.zip"
    );
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.installer.yaml"), "utf8")).toContain(
      "# yaml-language-server: $schema=https://aka.ms/winget-manifest.installer.1.9.0.schema.json"
    );
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.installer.yaml"), "utf8")).toContain(
      "RelativeFilePath: ha-nova/ha-nova.exe"
    );
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.locale.en-US.yaml"), "utf8")).toContain("PackageName: HA NOVA");
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.locale.en-US.yaml"), "utf8")).toContain(
      "# yaml-language-server: $schema=https://aka.ms/winget-manifest.defaultLocale.1.9.0.schema.json"
    );
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.yaml"), "utf8")).toContain(
      "# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.1.9.0.schema.json"
    );
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.installer.yaml"), "utf8")).not.toContain("Scope: user");
    expect(readFileSync(join(distDir, "winget", "ha-nova-winget-manifest-v0.3.0.zip.sha256"), "utf8")).toContain(
      "ha-nova-winget-manifest-v0.3.0.zip"
    );
  });

  it("normalizes a leading v in the requested winget manifest version", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-version-"));
    const bundleDir = join(distDir, "install-bundles");
    const manifestRoot = join(distDir, "winget", "manifests", "m", "markusleben", "ha-nova", "0.3.0");

    mkdirSync(bundleDir, { recursive: true });
    writeFileSync(join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip"), "fake-bundle");
    writeFileSync(
      join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip.sha256"),
      "4207F78DA0027952482882209CDF761C1F2846191CAE1C2D21E64693B74A0622  ha-nova-installer-bundle-windows-amd64.zip\n"
    );

    const result = spawnSync(
      "bash",
      ["scripts/release/build-winget-manifest.sh", "v0.3.0", "markusleben/ha-nova"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: distDir,
        },
        timeout: 30000,
      }
    );

    expect(result.status).toBe(0);
    expect(existsSync(join(manifestRoot, "markusleben.ha-nova.installer.yaml"))).toBe(true);
    expect(existsSync(join(distDir, "winget", "ha-nova-winget-manifest-v0.3.0.zip"))).toBe(true);
    expect(existsSync(join(distDir, "winget", "ha-nova-winget-manifest-vv0.3.0.zip"))).toBe(false);
    expect(readFileSync(join(manifestRoot, "markusleben.ha-nova.installer.yaml"), "utf8")).toContain("PackageVersion: 0.3.0");
  });

  it("fails winget manifest generation when the Windows bundle sha sidecar does not match the bundle bytes", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-sha-"));
    const bundleDir = join(distDir, "install-bundles");

    mkdirSync(bundleDir, { recursive: true });
    writeFileSync(join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip"), "fake-bundle");
    writeFileSync(
      join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip.sha256"),
      "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA  ha-nova-installer-bundle-windows-amd64.zip\n"
    );

    const result = spawnSync(
      "bash",
      ["scripts/release/build-winget-manifest.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: distDir,
        },
        timeout: 30000,
      }
    );

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Bundle SHA sidecar mismatch");
  });

  it("stages a winget maintainer checklist and PR body next to the staged submission payload", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-stage-"));
    const bundleDir = join(distDir, "install-bundles");
    const manifestRoot = join(distDir, "winget", "manifests", "m", "markusleben", "ha-nova", "0.3.0");

    mkdirSync(bundleDir, { recursive: true });
    writeFileSync(join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip"), "fake-bundle");
    writeFileSync(
      join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip.sha256"),
      "4207F78DA0027952482882209CDF761C1F2846191CAE1C2D21E64693B74A0622  ha-nova-installer-bundle-windows-amd64.zip\n"
    );

    const buildResult = spawnSync(
      "bash",
      ["scripts/release/build-winget-manifest.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: distDir,
        },
        timeout: 30000,
      }
    );
    expect(buildResult.status).toBe(0);

    const stageResult = spawnSync(
      "bash",
      ["scripts/release/prepare-winget-pkgs-submission.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: distDir,
          FORK_REPO: "alt-user/winget-pkgs",
          WINGET_STAGE_SOURCE: "local_dist",
        },
        timeout: 30000,
      }
    );

    const stageRoot = join(distDir, "winget", "submission", "markusleben.ha-nova", "0.3.0");
    const checklist = join(stageRoot, "winget-pkgs-maintainer-checklist.md");
    const prBody = join(stageRoot, "winget-pkgs-pr-body.md");
    const copyPath = join(stageRoot, "winget-pkgs-copy-path.txt");
    const commands = join(stageRoot, "winget-pkgs-gh-commands.md");

    expect(stageResult.status).toBe(0);
    expect(readFileSync(copyPath, "utf8")).toContain("manifests/m/markusleben/ha-nova/0.3.0");
    expect(readFileSync(prBody, "utf8")).toContain("Add markusleben.ha-nova version 0.3.0");
    expect(readFileSync(prBody, "utf8")).toContain("Source installer URL: https://github.com/markusleben/ha-nova/releases/download/v0.3.0/ha-nova-installer-bundle-windows-amd64.zip");
    const checklistContents = readFileSync(checklist, "utf8");
    const prBodyContents = readFileSync(prBody, "utf8");
    const commandsContents = readFileSync(commands, "utf8");

    expect(prBodyContents).toContain("- [ ] `winget validate --manifest");
    expect(prBodyContents).toContain("completed on Windows without warnings");
    expect(prBodyContents).toContain("- [ ] Initial published-source install/check-update/uninstall smoke will run after merge/publication");
    expect(checklistContents).toContain("## Initial Published-Source Proof");
    expect(checklistContents).toContain("## Upgrade Continuity Proof");
    expect(checklistContents).toContain("`ha-nova check-update`");
    expect(checklistContents).toContain("confirm `ha-nova` no longer resolves");
    expect(checklistContents).toContain("do not enable or rely on `LocalManifestFiles`");
    expect(checklistContents).toContain("first public `winget` publication");
    expect(checklistContents).toContain("<staged-submission-root-on-your-host>/manifests/m/markusleben/ha-nova/0.3.0");
    expect(checklistContents).not.toContain(stageRoot);
    expect(commandsContents).toContain('STAGED_ROOT="<set-this-to-your-staged-submission-root>"');
    expect(commandsContents).toContain('$StagedRoot = "<set-this-to-your-staged-submission-root>"');
    expect(commandsContents).toContain('PR_BODY="$STAGED_ROOT/winget-pkgs-pr-body.md"');
    expect(commandsContents).not.toContain(stageRoot);
    expect(commandsContents).toContain("gh repo clone alt-user/winget-pkgs");
    expect(commandsContents).toContain("cd winget-pkgs");
    expect(commandsContents).toContain("Copy-Item");
    expect(commandsContents).toContain("--repo microsoft/winget-pkgs");
    expect(commandsContents).toContain("--base master");
    expect(commandsContents).toContain("--head alt-user:ha-nova-0.3.0");
    expect(stageResult.stdout).toContain('winget validate --manifest "<staged-manifest-dir-on-your-validation-host>"');
    expect(stageResult.stdout).toContain("same-host default from this checkout:");
    expect(stageResult.stdout).toContain("staged from: local_dist");
    expect(stageResult.stdout).toContain("require a warning-free success result before opening any PR");
    expect(stageResult.stdout).toContain("winget show --id markusleben.ha-nova --exact --source winget");
    expect(stageResult.stdout).toContain("<staged-submission-root-on-your-pr-host>/winget-pkgs-pr-body.md");
    expect(stageResult.stdout).toContain("<staged-submission-root-on-your-pr-host>/winget-pkgs-gh-commands.md");
    expect(stageResult.stdout).not.toContain(`body: ${stageRoot}`);
    expect(stageResult.stdout).not.toContain(`commands: ${stageRoot}`);
    expect(stageResult.stdout).toContain("Generated helper files:");
    expect(stageResult.stdout).toContain(`staged submission root: ${stageRoot}`);
  });

  it("defaults winget submission staging to the exact final release assets", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-release-asset-"));
    const sourceDistDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-release-src-"));
    const bundleDir = join(sourceDistDir, "install-bundles");
    const fixtureDir = mkdtempSync(join(tmpdir(), "ha-nova-winget-release-fixture-"));
    const fakeGhDir = mkdtempSync(join(tmpdir(), "ha-nova-fake-gh-"));
    const fakeGhPath = join(fakeGhDir, "gh");

    mkdirSync(bundleDir, { recursive: true });
    writeFileSync(join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip"), "fake-bundle");
    writeFileSync(
      join(bundleDir, "ha-nova-installer-bundle-windows-amd64.zip.sha256"),
      "4207F78DA0027952482882209CDF761C1F2846191CAE1C2D21E64693B74A0622  ha-nova-installer-bundle-windows-amd64.zip\n"
    );

    const buildResult = spawnSync(
      "bash",
      ["scripts/release/build-winget-manifest.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: sourceDistDir,
        },
        timeout: 30000,
      }
    );
    expect(buildResult.status).toBe(0);

    for (const asset of [
      "ha-nova-winget-manifest-v0.3.0.zip",
      "ha-nova-installer-bundle-windows-amd64.zip",
      "ha-nova-installer-bundle-windows-amd64.zip.sha256",
    ]) {
      writeFileSync(join(fixtureDir, asset), readFileSync(join(sourceDistDir, asset.startsWith("ha-nova-winget-manifest") ? "winget" : "install-bundles", asset)));
    }

    writeFileSync(
      fakeGhPath,
      `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" != "release" || "$2" != "download" ]]; then
  echo "unexpected gh args: $*" >&2
  exit 1
fi
dest=""
shift 3
while [[ $# -gt 0 ]]; do
  case "$1" in
    -D)
      dest="$2"
      shift 2
      ;;
    -p)
      cp "${fixtureDir}/$2" "$dest/$2"
      shift 2
      ;;
    -R)
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
`,
      { mode: 0o755 }
    );

    const stageResult = spawnSync(
      "bash",
      ["scripts/release/prepare-winget-pkgs-submission.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          DIST_DIR: distDir,
          PATH: `${fakeGhDir}:${process.env.PATH ?? ""}`,
        },
        timeout: 30000,
      }
    );

    const stageRoot = join(distDir, "winget", "submission", "markusleben.ha-nova", "0.3.0");
    expect(stageResult.status).toBe(0);
    expect(stageResult.stdout).toContain("staged from: release_asset");
    expect(readFileSync(join(stageRoot, "winget-pkgs-pr-body.md"), "utf8")).toContain(
      "Source installer URL: https://github.com/markusleben/ha-nova/releases/download/v0.3.0/ha-nova-installer-bundle-windows-amd64.zip"
    );
  });

  it("refuses prerelease tags when staging a public winget submission from release assets", () => {
    const fakeGhDir = mkdtempSync(join(tmpdir(), "ha-nova-fake-gh-pre-"));
    const fakeGhPath = join(fakeGhDir, "gh");
    writeFileSync(fakeGhPath, "#!/usr/bin/env bash\nexit 0\n", { mode: 0o755 });

    const result = spawnSync(
      "bash",
      ["scripts/release/prepare-winget-pkgs-submission.sh", "0.3.0", "markusleben/ha-nova", "v0.3.0-rc1"],
      {
        cwd: process.cwd(),
        encoding: "utf8",
        env: {
          ...process.env,
          PATH: `${fakeGhDir}:${process.env.PATH ?? ""}`,
        },
        timeout: 30000,
      }
    );

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("WINGET_STAGE_SOURCE=release_asset only accepts final stable tags");
  });


  it("fails instead of picking an arbitrary stale nested binary when multiple candidates exist", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-release-ambiguous-"));
    const requiredSingles = [
      ["ha-nova-darwin_darwin_amd64_v1", "ha-nova"],
      ["ha-nova-darwin_darwin_arm64_v8.0", "ha-nova"],
      ["ha-nova-other_linux_amd64_v1", "ha-nova"],
      ["ha-nova-other_linux_arm64_v8.0", "ha-nova"],
    ] as const;
    const dirA = join(distDir, "ha-nova-other_windows_amd64_v1");
    const dirB = join(distDir, "ha-nova-other_windows_amd64_v2");

    for (const [dirName, binaryName] of requiredSingles) {
      const dir = join(distDir, dirName);
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, binaryName), "binary", { mode: 0o755 });
    }
    mkdirSync(dirA, { recursive: true });
    mkdirSync(dirB, { recursive: true });
    writeFileSync(join(dirA, "ha-nova.exe"), "old-binary", { mode: 0o755 });
    writeFileSync(join(dirB, "ha-nova.exe"), "new-binary", { mode: 0o755 });

    const result = spawnSync("bash", ["scripts/release/build-install-bundle.sh"], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...process.env,
        DIST_DIR: distDir,
      },
      timeout: 30000,
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Ambiguous");
    expect(result.stderr).toContain("windows/amd64");
  });

  it("fails instead of mixing flat and nested binary candidates from different dist layouts", () => {
    const distDir = mkdtempSync(join(tmpdir(), "ha-nova-release-mixed-"));
    const requiredSingles = [
      ["ha-nova-darwin_darwin_arm64_v8.0", "ha-nova"],
      ["ha-nova-other_linux_amd64_v1", "ha-nova"],
      ["ha-nova-other_linux_arm64_v8.0", "ha-nova"],
      ["ha-nova-other_windows_amd64_v1", "ha-nova.exe"],
    ] as const;

    for (const [dirName, binaryName] of requiredSingles) {
      const dir = join(distDir, dirName);
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, binaryName), "binary", { mode: 0o755 });
    }

    writeFileSync(join(distDir, "ha-nova-darwin-amd64"), "flat-binary", { mode: 0o755 });
    const nestedDir = join(distDir, "ha-nova-darwin_darwin_amd64_v1");
    mkdirSync(nestedDir, { recursive: true });
    writeFileSync(join(nestedDir, "ha-nova"), "nested-binary", { mode: 0o755 });

    const result = spawnSync("bash", ["scripts/release/build-install-bundle.sh"], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...process.env,
        DIST_DIR: distDir,
      },
      timeout: 30000,
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Ambiguous");
    expect(result.stderr).toContain("macos/amd64");
  });
});
