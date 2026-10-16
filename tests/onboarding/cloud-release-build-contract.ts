import { spawnSync } from "node:child_process";
import { cpSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { expect, it } from "vitest";

import { makeTempDir } from "../helpers/temp-dir.js";

// Cloud Remote stays enabled only through release-time signing and these
// guards; shrinking the platform list is a maintainer decision (AGENTS.md).
export function registerCloudReleaseBuildContractTests(): void {
  const releaseWorkflow = readFileSync(".github/workflows/release.yml", "utf8");
  const rcWorkflow = readFileSync(".github/workflows/release-candidate.yml", "utf8");

  it("keeps Cloud Remote enabled on every desktop platform in both version files", () => {
    const cli = JSON.parse(readFileSync("version.json", "utf8"));
    const app = JSON.parse(readFileSync("nova/version.json", "utf8"));
    expect(cli.cloud_remote_enabled).toBe(true);
    expect(cli.cloud_remote_platforms).toEqual(["darwin", "linux", "windows"]);
    expect(app.cloud_remote_enabled).toBe(cli.cloud_remote_enabled);
    expect(app.cloud_remote_platforms).toEqual(cli.cloud_remote_platforms);
  });

  it("rejects Cloud metadata drift between the CLI bundle and the Relay App", () => {
    const root = makeTempDir("ha-nova-metadata-");
    for (const file of ["version.json", "package.json", "package-lock.json", ".claude-plugin", "nova/version.json", "nova/config.yaml"]) {
      cpSync(file, join(root, file), { recursive: true });
    }
    const run = () =>
      spawnSync("bash", ["scripts/release/verify-release-metadata.sh"], {
        env: { ...process.env, HA_NOVA_SOURCE_ROOT: root },
        encoding: "utf8",
      });
    expect(run().status).toBe(0);
    const app = JSON.parse(readFileSync(join(root, "nova/version.json"), "utf8"));
    writeFileSync(join(root, "nova/version.json"), JSON.stringify({ ...app, cloud_remote_platforms: ["darwin"] }));
    const drifted = run();
    expect(drifted.status).toBe(1);
    expect(drifted.stderr).toContain("cloud_remote_platforms must be an array and identical");
  });

  it("signs Cloud evidence only for official builds of commits on main", () => {
    const goreleaser = readFileSync(".goreleaser.yml", "utf8");
    const bundleBuilder = readFileSync("scripts/release/build-install-bundle.sh", "utf8");
    expect(goreleaser).toContain("cloudremote_official");
    expect(goreleaser).not.toContain("cloudremote_dev");
    expect(goreleaser).not.toMatch(/goos:\s*\n\s*-\s*darwin/);
    expect(readFileSync("scripts/release/build-rc-binaries.sh", "utf8")).toContain("cloudremote_official");
    expect(readFileSync("scripts/release/build-sign-darwin-binaries.sh", "utf8")).toContain("cloudremote_official");
    expect(bundleBuilder).toContain("sign-cloud-release-evidence.mjs");
    expect(releaseWorkflow).toContain("Verify tag is on main");
    expect(rcWorkflow.match(/name: Verify ref is on main/g)).toHaveLength(2);
    for (const workflow of [releaseWorkflow, rcWorkflow]) {
      expect(workflow.match(/secrets\.HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM/g)).toHaveLength(1);
      expect(workflow).toContain("internal-cloud-release-check");
    }
  });

  it("keeps the macOS signing hygiene of the release builders", () => {
    const darwinBuilder = readFileSync("scripts/release/build-sign-darwin-binaries.sh", "utf8");
    const darwinVerifier = readFileSync("scripts/release/verify-macos-signature.sh", "utf8");
    for (const workflow of [releaseWorkflow, rcWorkflow]) {
      expect(workflow.match(/secrets\.HA_NOVA_MACOS_CERTIFICATE_P12_BASE64/g)).toHaveLength(1);
      expect(workflow.match(/secrets\.HA_NOVA_MACOS_CERTIFICATE_PASSWORD/g)).toHaveLength(1);
      expect(workflow).toContain("bash scripts/release/verify-macos-signature.sh");
    }
    expect(releaseWorkflow).toContain('cmp "$root/ha-nova" "$raw_binary"');
    expect(releaseWorkflow.indexOf("Download signed Darwin release binaries")).toBeLessThan(
      releaseWorkflow.indexOf("Build install bundles"),
    );
    expect(rcWorkflow.indexOf("Download signed Darwin RC binaries")).toBeLessThan(
      rcWorkflow.indexOf("Build install bundles"),
    );
    expect(darwinBuilder.indexOf("unset HA_NOVA_MACOS_CERTIFICATE_P12_BASE64")).toBeLessThan(
      darwinBuilder.indexOf("go build -trimpath"),
    );
    expect(darwinBuilder).toContain("-T /usr/bin/codesign");
    expect(darwinBuilder).toContain("umask 077");
    expect(darwinBuilder).toContain("--options runtime,hard,kill,library");
    expect(darwinBuilder.indexOf('security list-keychains -d user -s "${original_keychains[@]}"')).toBeLessThan(
      darwinBuilder.indexOf("security delete-keychain"),
    );
    expect(darwinVerifier).toContain('EXPECTED_IDENTIFIER="com.markusleben.ha-nova.cli"');
    expect(darwinVerifier).toContain('EXPECTED_TEAM_ID="CTF9J94274"');
  });
}
