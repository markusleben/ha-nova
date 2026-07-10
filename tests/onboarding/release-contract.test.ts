import { existsSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("release contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const installer = readFileSync("install.ps1", "utf8");
  const bundleBuilder = readFileSync("scripts/release/build-install-bundle.sh", "utf8");
  const releaseWorkflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(".github/workflows/release-candidate.yml", "utf8");
  const releasing = readFileSync("docs/releasing.md", "utf8");
  const linuxHeadlessHelper = readFileSync("scripts/smoke/linux-headless-setup-check.sh", "utf8");
  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    scripts?: Record<string, string>;
  };

  it("keeps release notes aligned to the single supported Windows install path", () => {
    expect(goreleaser).toContain("Stable install commands are release-pinned for this tag.");
    expect(goreleaser).toContain("https://raw.githubusercontent.com/markusleben/ha-nova/{{ .Tag }}/install.sh");
    expect(goreleaser).toContain("HA_NOVA_VERSION={{ .Tag }}");
    expect(goreleaser).toContain("https://raw.githubusercontent.com/markusleben/ha-nova/{{ .Tag }}/install.ps1");
    expect(goreleaser).toContain("Windows uses a single supported install path: `install.ps1`.");
    expect(goreleaser).toContain("Home Assistant Relay setup and token steps remain guided in the browser");
    expect(goreleaser).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(goreleaser).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1");
    expect(goreleaser).not.toContain("$ProgressPreference = 'SilentlyContinue'");
    expect(goreleaser).not.toContain("winget");
  });

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

  it("keeps v0.13.0 release-facing wording user-centric", () => {
    // Shipped release-note bodies are archived (docs/archive/work/) and
    // non-normative per documentation governance; only the active GoReleaser
    // template is contract-checked here.
    expect(goreleaser).toContain("Undo now covers multi-item changes");
    expect(goreleaser).toContain("last 5 updated automations, scripts, or standard helpers");
    expect(goreleaser).toContain("a few multi-step helper types still recover via Backups");
    expect(goreleaser).toContain("previously only the most recent update");
    expect(goreleaser).toContain("No NOVA Relay App update needed for this release — it stays on 0.2.6");
    expect(goreleaser).not.toContain("Use `v0.7.1` or the latest release command");
  });

  it("pins the GoReleaser release tag to the triggering workflow ref", () => {
    expect(releaseWorkflow).toContain("GORELEASER_CURRENT_TAG: ${{ github.ref_name }}");
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
