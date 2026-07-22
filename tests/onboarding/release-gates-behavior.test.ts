import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync, spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

const ACCOUNT_ID = "58e387e1204bdfe78781caca64f2cd15";
const VERSION_ID = "version-123";
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

function releaseFixture(): {
  root: string;
  sha: string;
  script: string;
  fakeBin: string;
  callLog: string;
} {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-release-gates-"));
  const releaseDir = join(root, "scripts", "release");
  const workerDir = join(root, "census-worker");
  const fakeBin = join(root, "fake-bin");
  mkdirSync(releaseDir, { recursive: true });
  mkdirSync(workerDir, { recursive: true });
  mkdirSync(fakeBin, { recursive: true });

  const script = join(releaseDir, "deploy-census-worker.sh");
  copyFileSync("scripts/release/deploy-census-worker.sh", script);
  copyFileSync(
    "scripts/release/verify-census-deployment.sh",
    join(releaseDir, "verify-census-deployment.sh"),
  );
  chmodSync(script, 0o755);
  writeFileSync(
    join(workerDir, "wrangler.toml"),
    `name = "ha-nova-census"\naccount_id = "${ACCOUNT_ID}"\n`,
    "utf8",
  );

  const callLog = join(root, "calls.log");
  writeExecutable(
    join(fakeBin, "node"),
    `#!/usr/bin/env bash
set -euo pipefail
printf '22\n'
`,
  );
  writeExecutable(
    join(fakeBin, "sleep"),
    `#!/usr/bin/env bash
set -euo pipefail
exit 0
`,
  );
  writeExecutable(
    join(fakeBin, "npx"),
    `#!/usr/bin/env bash
set -euo pipefail
printf 'npx %s\n' "$*" >> "$FAKE_CALL_LOG"
case " $* " in
  *" wrangler@4.113.0 dev "*) exec /bin/sleep 300 ;;
  *" wrangler@4.113.0 deploy "*)
    [[ "\${FAKE_MODE:-valid}" != "deploy_fail" ]] || exit 42
    target="https://ha-nova-census.markusleben.workers.dev"
    [[ "\${FAKE_MODE:-valid}" != "wrong_target" ]] || target="https://ha-nova-census-wrong.example.workers.dev"
    targets="[\\\"$target\\\"]"
    [[ "\${FAKE_MODE:-valid}" != "extra_target" ]] || targets="[\\\"$target\\\",\\\"https://extra.example.test\\\"]"
    printf '{"type":"deploy","version":1,"worker_name":"ha-nova-census","version_id":"%s","targets":%s}\n' \
      "$TEST_VERSION_ID" "$targets" > "$WRANGLER_OUTPUT_FILE_PATH"
    ;;
  *) exit 2 ;;
esac
`,
  );
  writeExecutable(
    join(fakeBin, "gh"),
    `#!/usr/bin/env bash
set -euo pipefail
printf 'gh %s\n' "$*" >> "$FAKE_CALL_LOG"
status="identical"
base="$TEST_SHA"
if [[ "\${FAKE_MODE:-valid}" == "not_upstream" ]]; then
  status="diverged"
  base="$REMOTE_MAIN_SHA"
fi
printf '{"status":"%s","base_commit":{"sha":"%s"},"merge_base_commit":{"sha":"%s"}}\n' \
  "$status" "$base" "$base"
`,
  );
  writeExecutable(
    join(fakeBin, "curl"),
    `#!/usr/bin/env bash
set -euo pipefail
printf 'curl %s\n' "$*" >> "$FAKE_CALL_LOG"
args=" $* "
if [[ "$args" == *" --request POST "* ]]; then
  if [[ "\${FAKE_MODE:-valid}" == "local_post_fail" ]]; then
    printf '500'
  else
    printf '204'
  fi
  exit 0
fi
if [[ "$args" == *"http://127.0.0.1:"* ]]; then
  printf '%s\n' '{"schema":1,"weekly":[{"iso_week":"2026-W30","count":1}],"window_weeks":4,"by_os":{"linux":1},"by_version":{"0.0.0":1},"by_relay":{"unknown":1},"peak_weekly_pings":1,"footnotes":{"counting":"not verified unique installs; duplicates; fabricated","identifiers":"no identifier"}}'
  exit 0
fi
headers=""
output=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --dump-header) headers="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
public_sha="$TEST_SHA"
public_version="$TEST_VERSION_ID"
payload='{"schema":1,"generated_at":"2026-07-23T00:00:00Z","weekly":[],"window_weeks":4,"by_os":{},"by_version":{},"by_relay":{},"peak_weekly_pings":0,"footnotes":{"counting":"not verified unique installs; duplicates; fabricated","identifiers":"no identifier"}}'
[[ "\${FAKE_MODE:-valid}" != "wrong_public_sha" ]] || public_sha="0000000000000000000000000000000000000000"
[[ "\${FAKE_MODE:-valid}" != "wrong_public_version" ]] || public_version="wrong-version"
[[ "\${FAKE_MODE:-valid}" != "malformed_public_stats" ]] || payload='{"schema":1}'
printf 'HTTP/2 200\\r\\nX-HA-NOVA-Deployment-SHA: %s\\r\\nX-HA-NOVA-Version-ID: %s\\r\\n\\r\\n' \
  "$public_sha" "$public_version" > "$headers"
printf '%s\n' "$payload" > "$output"
printf '200'
`,
  );

  execFileSync("git", ["init", "-q"], { cwd: root });
  execFileSync("git", ["config", "user.name", "Release Test"], { cwd: root });
  execFileSync("git", ["config", "user.email", "release-test@example.invalid"], {
    cwd: root,
  });
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", "fixture"], { cwd: root });
  const sha = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: root,
    encoding: "utf8",
  }).trim();
  return { root, sha, script, fakeBin, callLog };
}

function runGate(
  fixture: ReturnType<typeof releaseFixture>,
  mode: string,
  sha = fixture.sha,
): ReturnType<typeof spawnSync> {
  return spawnSync("bash", [fixture.script, sha, "--require-empty"], {
    cwd: fixture.root,
    encoding: "utf8",
    timeout: 15_000,
    env: {
      ...process.env,
      PATH: `${fixture.fakeBin}:${process.env.PATH ?? ""}`,
      FAKE_CALL_LOG: fixture.callLog,
      FAKE_MODE: mode,
      TEST_SHA: fixture.sha,
      TEST_VERSION_ID: VERSION_ID,
      REMOTE_MAIN_SHA: fixture.sha,
    },
  });
}

describe("release gate behavior", () => {
  it("stops a dirty or wrong-SHA checkout before Wrangler", () => {
    const wrong = releaseFixture();
    const wrongResult = runGate(wrong, "valid", "0".repeat(40));
    expect(wrongResult.status).not.toBe(0);
    expect(existsSync(wrong.callLog)).toBe(false);

    const dirty = releaseFixture();
    writeFileSync(join(dirty.root, "unreviewed.txt"), "dirty\n", "utf8");
    const dirtyResult = runGate(dirty, "valid");
    expect(dirtyResult.status).not.toBe(0);
    expect(existsSync(dirty.callLog)).toBe(false);
  });

  it("rejects a clean local commit that is not in hard-pinned upstream main", () => {
    const fixture = releaseFixture();
    writeFileSync(join(fixture.root, "local-only.txt"), "local-only\n", "utf8");
    execFileSync("git", ["add", "."], { cwd: fixture.root });
    execFileSync("git", ["commit", "-qm", "local only"], { cwd: fixture.root });
    const localSha = execFileSync("git", ["rev-parse", "HEAD"], {
      cwd: fixture.root,
      encoding: "utf8",
    }).trim();
    const result = runGate(fixture, "not_upstream", localSha);
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    expect(result.stderr).toContain("not in the hard-pinned markusleben/ha-nova main history");
    expect(readFileSync(fixture.callLog, "utf8")).not.toContain("wrangler");
  });

  it.each([
    "local_post_fail",
    "deploy_fail",
    "wrong_target",
    "extra_target",
    "wrong_public_sha",
    "wrong_public_version",
    "malformed_public_stats",
  ])(
    "fails closed for %s",
    (mode) => {
      const fixture = releaseFixture();
      const result = runGate(fixture, mode);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    },
  );

  it("accepts only the exact local-write, deploy-target, and public-version chain", () => {
    const fixture = releaseFixture();
    const result = runGate(fixture, "valid");
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(result.stdout).toContain("local Worker + Durable Object write/read smoke OK");
    expect(result.stdout).toContain(`${fixture.sha}/${VERSION_ID}`);
  });
});

function releaseJSON(assets = RELEASE_ASSETS.map((name) => ({
  name,
  state: "uploaded",
  size: 1,
  digest: `sha256:${"a".repeat(64)}`,
}))): string {
  return JSON.stringify({
    tagName: "v0.21.0-rc1",
    isDraft: true,
    isPrerelease: true,
    assets,
  });
}

function runAssetGate(payload: string, apiFails = false): ReturnType<typeof spawnSync> {
  const fakeBin = mkdtempSync(join(tmpdir(), "ha-nova-release-assets-"));
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
    ["starter", (assets: ReturnType<typeof JSON.parse>) => { assets[0].state = "starter"; }],
    ["zero-size", (assets: ReturnType<typeof JSON.parse>) => { assets[0].size = 0; }],
    ["bad-digest", (assets: ReturnType<typeof JSON.parse>) => { assets[0].digest = "sha256:bad"; }],
    ["missing", (assets: ReturnType<typeof JSON.parse>) => { assets.pop(); }],
    ["extra", (assets: ReturnType<typeof JSON.parse>) => { assets.push({ ...assets[0], name: "unexpected" }); }],
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
