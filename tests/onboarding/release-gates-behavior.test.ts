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

  const callLog = join(
    mkdtempSync(join(tmpdir(), "ha-nova-release-call-log-")),
    "calls.log",
  );
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
[[ -z "\${HA_NOVA_CENSUS_ACCESS_CLIENT_ID:-}" ]]
[[ -z "\${HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET:-}" ]]
printf 'npx %s\n' "$*" >> "$FAKE_CALL_LOG"
case " $* " in
  *" wrangler@4.113.0 secret list "*)
    if [[ "\${FAKE_MODE:-valid}" == "missing_worker_secret" ]]; then
      printf '[{"name":"ACCESS_TEAM_DOMAIN","type":"secret_text"}]\n'
    else
      printf '[{"name":"ACCESS_TEAM_DOMAIN","type":"secret_text"},{"name":"ACCESS_AUD","type":"secret_text"}]\n'
    fi
    ;;
  *" wrangler@4.113.0 dev "*)
    if [[ "\${FAKE_MODE:-valid}" == "local_mutates_checkout" ]]; then
      printf 'unreviewed\n' > unreviewed-after-local.txt
    fi
    exec /bin/sleep 300
    ;;
  *" wrangler@4.113.0 deployments list "*)
    printf '[{"versions":[{"version_id":"previous-version","percentage":100}]}]\n'
    ;;
  *" wrangler@4.113.0 rollback "*)
    printf 'rollback ok\n'
    ;;
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
if [[ " $* " == *" api --hostname github.com user --jq .login "* ]]; then
  if [[ "\${FAKE_MODE:-valid}" == "wrong_github_user" ]]; then
    printf 'other-user\\n'
  else
    printf 'markusleben\\n'
  fi
  exit 0
fi
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
if [[ "$args" == *"ha-nova-census.markusleben.workers.dev/stats"* && "$args" != *"/stats/api"* ]]; then
  if [[ "\${FAKE_MODE:-valid}" == "access_probe_failure" ]]; then
    exit 7
  elif [[ "$args" == *" --header @"* ]]; then
    if [[ "\${FAKE_MODE:-valid}" == "service_token_denied" ]]; then
      printf '302'
    else
      printf '200'
    fi
  else
    printf '302'
  fi
  exit 0
fi
if [[ "$args" == *" --request POST "* ]]; then
  if [[ "\${FAKE_MODE:-valid}" == "local_post_fail" ]]; then
    printf '500'
  elif [[ "\${FAKE_MODE:-valid}" == "cleanup_withdraw_fail" && "$args" == *"ha-nova-census.markusleben.workers.dev/withdraw"* ]]; then
    printf '500'
  else
    printf '204'
  fi
  exit 0
fi
if [[ "$args" == *"/not-found"* ]]; then
  printf '404'
  exit 0
fi
if [[ "$args" == *"http://127.0.0.1:"* && "$args" == *"/stats/api"* && "$args" != *"X-HA-NOVA-Local-Stats-Token: local-release-smoke-"* ]]; then
  printf '403'
  exit 0
fi
if [[ "$args" == *"http://127.0.0.1:"* ]]; then
  if [[ "$args" == *"release_withdraw_smoke"* ]]; then
    printf '%s\n' '{"schema":2,"client_installations":{"active_21_days":0,"known_60_days":0,"by_os":{},"by_version":{},"relay_versions":{},"relay_not_recently_observed":0,"new_installation_rejections_today":0},"legacy_ping_activity":{"weekly":[{"iso_week":"2026-W30","count":1}]}}'
  else
    printf '%s\n' '{"schema":2,"client_installations":{"active_21_days":1,"known_60_days":1,"by_os":{"linux":1},"by_version":{"0.0.0":1},"relay_versions":{},"relay_not_recently_observed":1,"new_installation_rejections_today":0},"legacy_ping_activity":{"weekly":[{"iso_week":"2026-W30","count":1}]}}'
  fi
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
smoke_count=0
[[ "$args" != *"dedup-"* ]] || smoke_count=1
[[ "\${FAKE_MODE:-valid}" != "dedup_failed" ]] || smoke_count=0
active_count=$smoke_count
if [[ "\${FAKE_MODE:-valid}" == "unrelated_linux_install" ]]; then
  [[ "$args" != *"dedup-"* ]] || active_count=2
  [[ "$args" != *"withdraw-"* ]] || active_count=1
fi
relay_analytics='{"status":"available","source":"https://analytics.home-assistant.io/addons.json","slug":"2368fcfa_ha_nova_relay","total":9,"by_version":{"0.7.0":7,"0.6.0":1,"0.2.0":1}}'
[[ "\${FAKE_MODE:-valid}" != "analytics_unavailable" ]] || relay_analytics='{"status":"unavailable","source":"https://analytics.home-assistant.io/addons.json","slug":"2368fcfa_ha_nova_relay","error":"upstream timeout"}'
[[ "\${FAKE_MODE:-valid}" != "malformed_relay_analytics" ]] || relay_analytics='{"status":"unavailable","source":"https://analytics.home-assistant.io/addons.json","slug":"2368fcfa_ha_nova_relay"}'
[[ "\${FAKE_MODE:-valid}" != "stale_relay_analytics" ]] || relay_analytics='{"status":"unavailable","source":"https://analytics.home-assistant.io/addons.json","slug":"2368fcfa_ha_nova_relay","error":"upstream timeout","total":9,"by_version":{"0.7.0":9}}'
payload="{\\"schema\\":2,\\"generated_at\\":\\"2026-07-23T00:00:00Z\\",\\"client_installations\\":{\\"active_21_days\\":$active_count,\\"known_60_days\\":$active_count,\\"release_smoke_installations\\":$smoke_count,\\"by_os\\":{\\"linux\\":$active_count},\\"by_version\\":{\\"other\\":$active_count},\\"relay_versions\\":{},\\"relay_not_recently_observed\\":$active_count,\\"new_installation_rejections_today\\":0},\\"relay_app_installations\\":$relay_analytics,\\"legacy_ping_activity\\":{\\"weekly\\":[]}}"
[[ "\${FAKE_MODE:-valid}" != "wrong_public_sha" ]] || public_sha="0000000000000000000000000000000000000000"
[[ "\${FAKE_MODE:-valid}" != "wrong_public_version" ]] || public_version="wrong-version"
[[ "\${FAKE_MODE:-valid}" != "malformed_public_stats" ]] || payload='{"schema":2}'
printf 'HTTP/2 200\\r\\nX-HA-NOVA-Deployment-SHA: %s\\r\\nX-HA-NOVA-Version-ID: %s\\r\\n\\r\\n' \
  "$public_sha" "$public_version" > "$headers"
printf '%s\n' "$payload" > "$output"
printf '200'
`,
  );

  execFileSync("git", ["init", "-q"], { cwd: root });
  execFileSync("git", ["config", "user.name", "Release Test"], { cwd: root });
  execFileSync(
    "git",
    ["config", "user.email", "release-test@example.invalid"],
    {
      cwd: root,
    },
  );
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
  return spawnSync("bash", [fixture.script, sha], {
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
      HA_NOVA_CENSUS_ACCESS_CLIENT_ID: "test-id",
      HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET: "test-secret",
      HA_NOVA_CENSUS_BROWSER_ACCESS_VERIFIED: "1",
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
    expect(result.stderr).toContain(
      "not in the hard-pinned markusleben/ha-nova main history",
    );
    expect(readFileSync(fixture.callLog, "utf8")).not.toContain("wrangler");
  });

  it.each([
    "access_probe_failure",
    "service_token_denied",
    "wrong_github_user",
    "missing_worker_secret",
    "local_mutates_checkout",
    "local_post_fail",
    "deploy_fail",
    "wrong_target",
    "extra_target",
    "wrong_public_sha",
    "wrong_public_version",
    "malformed_public_stats",
    "malformed_relay_analytics",
    "stale_relay_analytics",
    "cleanup_withdraw_fail",
  ])("fails closed for %s", (mode) => {
    const fixture = releaseFixture();
    const result = runGate(fixture, mode);
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it.each(["valid", "analytics_unavailable", "unrelated_linux_install"])(
    "accepts the exact deployment chain in %s mode",
    (mode) => {
      const fixture = releaseFixture();
      const result = runGate(fixture, mode);
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "local Worker + Durable Object write/read smoke OK",
      );
      expect(result.stdout).toContain(`${fixture.sha}/${VERSION_ID}`);
    },
  );

  it("withdraws the ephemeral production ID when verification fails after ping", () => {
    const fixture = releaseFixture();
    const result = runGate(fixture, "dedup_failed");
    expect(result.status).not.toBe(0);
    const calls = readFileSync(fixture.callLog, "utf8");
    expect(calls).toContain("/withdraw");
  });

  it("rolls production back after any post-deploy verification failure", () => {
    const fixture = releaseFixture();
    const result = runGate(fixture, "wrong_public_sha");
    expect(result.status).not.toBe(0);
    expect(readFileSync(fixture.callLog, "utf8")).toContain(
      "wrangler@4.113.0 rollback previous-version",
    );
  });

  it("blocks with the exact manual cleanup action when withdrawal stays non-204", () => {
    const fixture = releaseFixture();
    const result = runGate(fixture, "cleanup_withdraw_fail");
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("automatic cleanup failed");
    expect(result.stderr).toContain('"installation_id":"cns-');
    expect(result.stderr).toContain("/withdraw");
  });
});

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
