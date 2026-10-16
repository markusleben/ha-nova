import { chmodSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

import { makeTempDir } from "../helpers/temp-dir.js";

const RELEASE_ASSETS = [
  "checksums.txt",
  "ha-nova-darwin-amd64",
  "ha-nova-darwin-arm64",
  "ha-nova-linux-amd64",
  "ha-nova-linux-arm64",
  "ha-nova-windows-amd64.exe",
  "ha-nova-installer-bundle-linux-amd64.tar.gz",
  "ha-nova-installer-bundle-linux-amd64.tar.gz.sha256",
  "ha-nova-installer-bundle-linux-arm64.tar.gz",
  "ha-nova-installer-bundle-linux-arm64.tar.gz.sha256",
  "ha-nova-installer-bundle-macos-amd64.tar.gz",
  "ha-nova-installer-bundle-macos-amd64.tar.gz.sha256",
  "ha-nova-installer-bundle-macos-arm64.tar.gz",
  "ha-nova-installer-bundle-macos-arm64.tar.gz.sha256",
  "ha-nova-installer-bundle-windows-amd64.zip",
  "ha-nova-installer-bundle-windows-amd64.zip.sha256",
];

function writeExecutable(path: string, body: string): void {
  writeFileSync(path, body, "utf8");
  chmodSync(path, 0o755);
}

function releaseJSON(
  assets = RELEASE_ASSETS.map((name) => ({
    name,
    state: "uploaded",
    size: 1,
    digest: `sha256:${"a".repeat(64)}`,
  })),
): string {
  return JSON.stringify({
    tagName: "v0.21.0-rc1",
    isDraft: true,
    isPrerelease: true,
    assets,
  });
}

function runAssetGate(
  payload: string,
  apiFails = false,
): ReturnType<typeof spawnSync> {
  const fakeBin = makeTempDir("ha-nova-release-assets-");
  writeExecutable(
    join(fakeBin, "gh"),
    `#!/usr/bin/env bash
set -euo pipefail
[[ "\${FAKE_GH_FAIL:-0}" != "1" ]] || exit 42
printf '%s\n' "$FAKE_RELEASE_JSON"
`,
  );
  return spawnSync(
    "bash",
    ["scripts/release/verify-release-assets.sh", "v0.21.0-rc1"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${fakeBin}:${process.env.PATH ?? ""}`,
        FAKE_GH_FAIL: apiFails ? "1" : "0",
        FAKE_RELEASE_JSON: payload,
      },
    },
  );
}

describe("release asset gate behavior", () => {
  it("accepts the exact complete uploaded SHA-256-attested set", () => {
    const result = runAssetGate(releaseJSON());
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it.each([
    [
      "starter",
      (assets: ReturnType<typeof JSON.parse>) => {
        assets[0].state = "starter";
      },
    ],
    [
      "zero-size",
      (assets: ReturnType<typeof JSON.parse>) => {
        assets[0].size = 0;
      },
    ],
    [
      "bad-digest",
      (assets: ReturnType<typeof JSON.parse>) => {
        assets[0].digest = "sha256:bad";
      },
    ],
    [
      "missing",
      (assets: ReturnType<typeof JSON.parse>) => {
        assets.pop();
      },
    ],
    [
      "extra",
      (assets: ReturnType<typeof JSON.parse>) => {
        assets.push({ ...assets[0], name: "unexpected" });
      },
    ],
  ])("rejects %s assets", (_name, mutate) => {
    const assets = JSON.parse(releaseJSON()).assets;
    mutate(assets);
    const result = runAssetGate(releaseJSON(assets));
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it("fails closed when GitHub asset discovery fails", () => {
    const result = runAssetGate(releaseJSON(), true);
    expect(result.status).not.toBe(0);
  });
});
