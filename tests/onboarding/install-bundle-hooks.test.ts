/**
 * The release bundle carries the Claude Code SessionStart hook (hooks/), so
 * CLI installs get skills/ha-nova/index.md injected into every session.
 * Builds real bundles with fake binaries and a throwaway signing key.
 */
import { execFileSync, spawnSync } from "node:child_process";
import { generateKeyPairSync } from "node:crypto";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { beforeAll, describe, expect, it } from "vitest";

import { makeTempDir } from "../helpers/temp-dir.js";

describe("install bundle ships the Claude session hook", () => {
  let bundles = "";

  beforeAll(() => {
    const dist = makeTempDir("ha-nova-bundle-dist-");
    for (const target of ["darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64.exe"]) {
      writeFileSync(join(dist, `ha-nova-${target}`), "#!/bin/sh\nexit 0\n", { mode: 0o755 });
    }
    // ponytail: throwaway key, never the production signing secret.
    const key = generateKeyPairSync("ed25519").privateKey.export({ type: "pkcs8", format: "pem" }).toString();
    const result = spawnSync("bash", ["scripts/release/build-install-bundle.sh"], {
      encoding: "utf8",
      timeout: 120000,
      env: { ...process.env, DIST_DIR: dist, HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM: key },
    });
    expect(result.status, result.stderr).toBe(0);
    bundles = join(dist, "install-bundles");
  }, 120000);

  it.each(["macos-amd64", "macos-arm64", "linux-amd64", "linux-arm64"])("%s tarball has an executable hook", (target) => {
    const listing = execFileSync("tar", ["-tvzf", join(bundles, `ha-nova-installer-bundle-${target}.tar.gz`)], { encoding: "utf8" });
    expect(listing).toMatch(/ ha-nova\/hooks\/hooks\.json\n/);
    expect(listing).toMatch(/-rwxr-xr-x .* ha-nova\/hooks\/session-start\n/);
  });

  it("windows zip has the hook", () => {
    const listing = execFileSync("unzip", ["-Z1", join(bundles, "ha-nova-installer-bundle-windows-amd64.zip")], { encoding: "utf8" });
    expect(listing).toContain("ha-nova/hooks/hooks.json\n");
    expect(listing).toContain("ha-nova/hooks/session-start\n");
  });

  it("the extracted hook runs from the bundle and injects the index", () => {
    const root = makeTempDir("ha-nova-bundle-root-");
    execFileSync("tar", ["-xzf", join(bundles, "ha-nova-installer-bundle-linux-amd64.tar.gz"), "-C", root]);
    const home = makeTempDir("ha-nova-bundle-home-");
    // No CLI on PATH or in HOME: nothing outside the bundle runs.
    const result = spawnSync("bash", [join(root, "ha-nova", "hooks", "session-start")], {
      encoding: "utf8",
      timeout: 15000,
      input: JSON.stringify({ hook_event_name: "SessionStart", source: "startup" }),
      env: { PATH: "/usr/bin:/bin", HOME: home },
    });
    expect(result.status, result.stderr).toBe(0);
    const json = JSON.parse(result.stdout.trim());
    expect(json.hookSpecificOutput.additionalContext).toContain("# HA NOVA Index");
    expect(json.hookSpecificOutput.additionalContext).toContain("# Session Bootstrap");
  });
});
