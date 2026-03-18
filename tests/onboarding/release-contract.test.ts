import { mkdtempSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("release contract", () => {
  const goreleaser = readFileSync(".goreleaser.yml", "utf8");
  const workflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(".github/workflows/release-candidate.yml", "utf8");
  const bundleBuilder = readFileSync("scripts/release/build-install-bundle.sh", "utf8");
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
    expect(goreleaser).toContain("## Upgrade Notes");
    expect(goreleaser).toContain("ha-nova update");
    expect(goreleaser).toContain("ha-nova check-update");
    expect(goreleaser).toContain("Legacy pre-Go installs still require the legacy cleanup script before reinstalling.");
    expect(goreleaser).not.toContain("~/.config/ha-nova/update");
  });

  it("groups release notes into user-facing sections instead of a flat commit dump", () => {
    expect(goreleaser).toContain("changelog:");
    expect(goreleaser).toContain("title: New Features");
    expect(goreleaser).toContain("title: Bug Fixes");
    expect(goreleaser).toContain("title: UX, Docs, and Refactors");
    expect(goreleaser).toContain("title: Internal Maintenance");
    expect(goreleaser).toContain('      - "^Merge "');
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

  it("runs the canonical verify command before publishing and smoke-checks install, update, and uninstall afterwards", () => {
    expect(workflow).toContain("actions/setup-node@v6");
    expect(workflow).toContain('node-version: "20"');
    expect(workflow).toContain("npm ci");
    expect(workflow).toContain("npm run verify");
    expect(workflow).toContain("environment:");
    expect(workflow).toContain("name: production");
    expect(workflow).toContain("Build install bundles");
    expect(workflow).toContain("Upload install bundles");
    expect(workflow).toContain("dist/install-bundles");
    expect(workflow).toContain("Post-publish smoke installers");
    expect(workflow).toContain("install.ps1");
    expect(workflow).toContain("bash install.sh");
    expect(workflow).toContain("check-update --quiet");
    expect(workflow).toContain("update --version");
    expect(workflow).toContain("uninstall --yes");
  });

  it("keeps release signing policy consistent with the active workflow", () => {
    expect(workflow).toContain("--skip=sign");
    expect(goreleaser).not.toContain("\nsigns:\n");
    expect(goreleaser).not.toContain("codesign");
  });

  it("keeps npm run verify aligned with CI-safe release gates", () => {
    expect(pkg.scripts?.verify).toContain("npm run typecheck");
    expect(pkg.scripts?.verify).toContain("npm run test:safe");
    expect(pkg.scripts?.verify).toContain("npm run build");
    expect(pkg.scripts?.verify).toContain("bash scripts/check-docs.sh");
    expect(pkg.scripts?.verify).toContain("npm run test:cli");
    expect(pkg.scripts?.verify).not.toContain("test:desktop");
  });

  it("defines a manual release-candidate workflow that can optionally publish bundle-based prereleases", () => {
    expect(rcWorkflow).toContain("workflow_dispatch:");
    expect(rcWorkflow).toContain("publish_release");
    expect(rcWorkflow).toContain("version_tag");
    expect(rcWorkflow).toContain("actions/setup-node@v6");
    expect(rcWorkflow).toContain("node-version: 20");
    expect(rcWorkflow).toContain("npm ci");
    expect(rcWorkflow).toContain("npm run verify");
    expect(rcWorkflow).toContain("goreleaser/goreleaser-action@v6");
    expect(rcWorkflow).toContain("args: build --snapshot --clean");
    expect(rcWorkflow).toContain('build-install-bundle.sh "${VERSION_TAG#v}"');
    expect(rcWorkflow).toContain("Build install bundles");
    expect(rcWorkflow).toContain("Upload RC artifacts");
    expect(rcWorkflow).toContain("Smoke bundles");
    expect(rcWorkflow).toContain("version_tag must match vX.Y.Z-rcN");
    expect(rcWorkflow).toContain("actions/upload-artifact@v4");
    expect(rcWorkflow).toContain("actions/download-artifact@v4");
    expect(rcWorkflow).toContain("gh release create");
    expect(rcWorkflow).toContain("--prerelease");
    expect(rcWorkflow).toContain('install_ref="${GITHUB_REF_NAME}"');
    expect(rcWorkflow).toContain("raw.githubusercontent.com/markusleben/ha-nova/");
    expect(rcWorkflow).toContain("install.ps1");
    expect(rcWorkflow).not.toContain("goreleaser release");
  });

  it("documents the required GitHub production gate for final release", () => {
    const releasing = readFileSync("docs/releasing.md", "utf8");

    expect(releasing).toContain("Release Notes Structure");
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
    expect(releasing).toContain("only Claude currently has the extra automatic SessionStart update banner");
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
