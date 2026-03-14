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

  it("builds ha-nova binaries instead of relay-named release artifacts", () => {
    expect(goreleaser).toContain("project_name: ha-nova");
    expect(goreleaser).toContain("binary: ha-nova");
    expect(goreleaser).toContain('name_template: "ha-nova-{{ .Os }}-{{ .Arch }}"');
    expect(goreleaser).not.toContain("binary: relay");
  });

  it("publishes release notes with the Go-first public commands", () => {
    expect(goreleaser).toContain("install.sh");
    expect(goreleaser).toContain("install.ps1");
    expect(goreleaser).toContain("ha-nova update");
    expect(goreleaser).toContain("ha-nova check-update");
    expect(goreleaser).not.toContain("~/.config/ha-nova/update");
  });

  it("builds macOS, Linux, and Windows install bundles with bundle metadata", () => {
    expect(bundleBuilder).toContain("ha-nova-macos");
    expect(bundleBuilder).toContain("ha-nova-linux");
    expect(bundleBuilder).toContain("ha-nova-windows");
    expect(bundleBuilder).toContain("bundle.json");
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

  it("defines a manual release-candidate workflow that can optionally publish bundle-based prereleases", () => {
    expect(rcWorkflow).toContain("workflow_dispatch:");
    expect(rcWorkflow).toContain("publish_release");
    expect(rcWorkflow).toContain("version_tag");
    expect(rcWorkflow).toContain("npm ci");
    expect(rcWorkflow).toContain("npm run verify");
    expect(rcWorkflow).toContain("goreleaser/goreleaser-action@v6");
    expect(rcWorkflow).toContain("args: build --snapshot --clean");
    expect(rcWorkflow).toContain('build-install-bundle.sh "${VERSION_TAG#v}"');
    expect(rcWorkflow).toContain("Build install bundles");
    expect(rcWorkflow).toContain("Upload RC artifacts");
    expect(rcWorkflow).toContain("Smoke bundles");
    expect(rcWorkflow).toContain("Verify RC publish ref is on main");
    expect(rcWorkflow).toContain("version_tag must match vX.Y.Z-rcN");
    expect(rcWorkflow).toContain("actions/upload-artifact@v4");
    expect(rcWorkflow).toContain("actions/download-artifact@v4");
    expect(rcWorkflow).toContain("gh release create");
    expect(rcWorkflow).toContain("--prerelease");
    expect(rcWorkflow).toContain("install.ps1");
    expect(rcWorkflow).not.toContain("goreleaser release");
  });

  it("documents the required GitHub production gate for final release", () => {
    const releasing = readFileSync("docs/releasing.md", "utf8");

    expect(releasing).toContain("production");
    expect(releasing).toContain("required reviewers");
    expect(releasing).toContain("prevent self-review");
    expect(releasing).toContain("v*");
    expect(releasing).toContain("Maintainer-only step");
    expect(releasing).toContain("if `required reviewers` is configured");
    expect(releasing).toContain("one direct admin collaborator");
    expect(releasing).toContain("npm run release:rc:local");
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
    expect(readFileSync(join(outputDir, "ha-nova-macos-amd64.tar.gz.sha256"), "utf8")).toContain(
      "ha-nova-macos-amd64.tar.gz"
    );
    expect(readFileSync(join(outputDir, "ha-nova-linux-arm64.tar.gz.sha256"), "utf8")).toContain(
      "ha-nova-linux-arm64.tar.gz"
    );
    expect(readFileSync(join(outputDir, "ha-nova-windows-amd64.zip.sha256"), "utf8")).toContain(
      "ha-nova-windows-amd64.zip"
    );
  });
});
