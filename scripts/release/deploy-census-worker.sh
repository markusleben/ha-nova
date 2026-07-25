#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha>

From a clean checkout of the exact reviewed merge, exercises the Worker and
SQLite Durable Object locally, deploys the pinned production Worker, attests
Wrangler's exact worker/target/version output, then verifies that same
Cloudflare version, private dashboard contract, deduplication, and withdrawal.
Cloudflare Access must already protect /stats*.
EOF
}

fail() {
  echo "[deploy-census-worker] ERROR: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

single_deployment_version_id() {
  jq -ser '
    select(length == 1)
    | .[0]
    | select(type == "object")
    | .versions
    | select(
        type == "array"
        and length == 1
        and .[0].percentage == 100
        and (
          .[0].version_id
          | type == "string"
            and test("^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$")
        )
      )
    | .[0].version_id
  '
}

deployment_output_version_id() {
  jq -ser '
    [.[] | select(.type == "deploy")]
    | select(length == 1)
    | .[0].version_id
    | select(
        type == "string"
        and test("^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$")
      )
  ' "$1"
}

if [[ "$#" -ne 1 ]]; then
  usage
  exit 2
fi

reviewed_sha="$1"
[[ "$reviewed_sha" =~ ^[0-9a-f]{40}$ ]] || fail "reviewed SHA must be exactly 40 lowercase hexadecimal characters"

require_command curl
require_command awk
require_command git
require_command gh
require_command jq
require_command node
require_command npx
require_command openssl
require_command sed

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root_dir="$(cd "${script_dir}/../.." && pwd)"
worker_dir="${root_dir}/census-worker"
config_file="${worker_dir}/wrangler.toml"
expected_account="58e387e1204bdfe78781caca64f2cd15"
expected_worker="ha-nova-census"
expected_target="https://ha-nova-census.markusleben.workers.dev"
access_id="${HA_NOVA_CENSUS_ACCESS_CLIENT_ID:-}"
access_secret="${HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET:-}"
browser_access_verified="${HA_NOVA_CENSUS_BROWSER_ACCESS_VERIFIED:-}"
[[ -n "$access_id" ]] || fail "HA_NOVA_CENSUS_ACCESS_CLIENT_ID is required"
[[ -n "$access_secret" ]] || fail "HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET is required"
[[ "$browser_access_verified" == "1" ]] \
  || fail "set HA_NOVA_CENSUS_BROWSER_ACCESS_VERIFIED=1 only after a fresh maintainer browser login reaches /stats"
[[ "$access_id" != *$'\n'* && "$access_id" != *$'\r'* ]] || fail "Access client ID contains a line break"
[[ "$access_secret" != *$'\n'* && "$access_secret" != *$'\r'* ]] || fail "Access client secret contains a line break"

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-census-release.XXXXXX")"
dev_pid=""
rollback_armed=0
rollback_version=""
deployed_version=""
read_current_deployment() {
  CLOUDFLARE_ENV='' CLOUDFLARE_ACCOUNT_ID="$expected_account" \
    npx --yes wrangler@4.113.0 deployments status \
      --cwd "$worker_dir" \
      --config "$config_file" \
      --name "$expected_worker" \
      --json
}

cleanup() {
  cleanup_status=$?
  if [[ -n "$dev_pid" ]] && kill -0 "$dev_pid" 2>/dev/null; then
    kill "$dev_pid" 2>/dev/null || true
    wait "$dev_pid" 2>/dev/null || true
  fi
  if [[ "$rollback_armed" -eq 1 && -n "$rollback_version" ]]; then
    if [[ -z "$deployed_version" && -n "${wrangler_output:-}" && -s "$wrangler_output" ]]; then
      deployed_version="$(deployment_output_version_id "$wrangler_output")" \
        || deployed_version=""
    fi
    current_deployment="$(read_current_deployment)" || current_deployment=""
    current_version="$(
      single_deployment_version_id <<<"$current_deployment"
    )" || current_version=""
    if [[ "$current_version" == "$rollback_version" ]]; then
      echo "[deploy-census-worker] production stayed on ${rollback_version}; rollback not needed" >&2
    elif [[ -z "$current_version" ]]; then
      echo "[deploy-census-worker] ERROR: could not determine the active Worker version; refusing automatic rollback" >&2
      cleanup_status=1
    elif [[ -n "$deployed_version" && "$current_version" != "$deployed_version" ]]; then
      echo "[deploy-census-worker] ERROR: active Worker version changed outside this deploy; refusing automatic rollback" >&2
      cleanup_status=1
    else
      echo "[deploy-census-worker] restoring previous Worker version ${rollback_version}" >&2
      if ! CLOUDFLARE_ENV='' CLOUDFLARE_ACCOUNT_ID="$expected_account" \
        npx --yes wrangler@4.113.0 rollback "$rollback_version" \
          --cwd "$worker_dir" \
          --config "$config_file" \
          --name "$expected_worker" \
          --message "Automatic rollback after failed HA NOVA Census verification" \
          --yes; then
        echo "[deploy-census-worker] ERROR: automatic Worker rollback failed" >&2
        cleanup_status=1
      else
        restored_deployment="$(read_current_deployment)" \
          || restored_deployment=""
        restored_version="$(
          single_deployment_version_id <<<"$restored_deployment"
        )" || restored_version=""
        if [[ "$restored_version" != "$rollback_version" ]]; then
          echo "[deploy-census-worker] ERROR: could not verify the restored Worker version" >&2
          cleanup_status=1
        fi
      fi
    fi
  fi
  rm -rf -- "$temp_dir"
  trap - EXIT
  exit "$cleanup_status"
}
trap cleanup EXIT

access_headers="${temp_dir}/access-headers"
umask 077
printf 'CF-Access-Client-Id: %s\nCF-Access-Client-Secret: %s\n' \
  "$access_id" "$access_secret" >"$access_headers"
unset HA_NOVA_CENSUS_ACCESS_CLIENT_ID HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET

[[ -f "$config_file" ]] || fail "missing ${config_file}"
configured_account="$(sed -nE 's/^account_id[[:space:]]*=[[:space:]]*"([0-9a-f]+)"[[:space:]]*$/\1/p' "$config_file")"
[[ "$configured_account" == "$expected_account" ]] || fail "wrangler.toml account_id does not match the pinned production account"

node_major="$(node -p 'Number(process.versions.node.split(".")[0])')"
[[ "$node_major" =~ ^[0-9]+$ ]] || fail "could not determine Node.js major version"
(( node_major >= 22 )) || fail "Node.js 22 or newer is required (found ${node_major})"

[[ -z "$(git -C "$root_dir" status --porcelain)" ]] || fail "checkout is dirty; deploy only the exact reviewed merge"
actual_sha="$(git -C "$root_dir" rev-parse HEAD)"
[[ "$actual_sha" == "$reviewed_sha" ]] || fail "HEAD ${actual_sha} does not match reviewed SHA ${reviewed_sha}"
gh auth status --hostname github.com >/dev/null 2>&1 \
  || fail "gh is not authenticated for github.com"
active_github_user="$(gh api --hostname github.com user --jq .login)" \
  || fail "could not determine the active GitHub account"
[[ "$active_github_user" == "markusleben" ]] \
  || fail "active GitHub account is ${active_github_user}; expected markusleben"

# The dashboard must be private before code exposing its new routes is
# deployed. The existing production /stats route is a reliable Access probe:
# without Access it answers 200; with Access it challenges or denies.
unauthenticated_stats_status="$(
  curl --disable --silent --show-error \
    --connect-timeout 5 \
    --max-time 15 \
    --proto '=https' \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${expected_target}/stats"
)" || unauthenticated_stats_status=""
case "$unauthenticated_stats_status" in
  302|401|403) ;;
  *) fail "unauthenticated Access probe returned HTTP ${unauthenticated_stats_status:-transport-error}; require an Access challenge or denial before deployment" ;;
esac

authenticated_stats_status="$(
  curl --disable --silent --show-error \
    --connect-timeout 5 \
    --max-time 15 \
    --proto '=https' \
    --header "@${access_headers}" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${expected_target}/stats"
)" || authenticated_stats_status=""
[[ "$authenticated_stats_status" == "200" ]] \
  || fail "Cloudflare Access service token did not reach the existing private stats route (HTTP ${authenticated_stats_status:-transport-error})"

# A clean local commit is not release provenance. Bind the requested SHA to
# the hard-pinned production repository's main history through GitHub's compare
# API; a fork, detached local commit, or unmerged PR head must fail closed.
upstream_comparison="$(gh api --hostname github.com --method GET "repos/markusleben/ha-nova/compare/${reviewed_sha}...main")" \
  || fail "could not prove ${reviewed_sha} against markusleben/ha-nova main"
jq -e --arg sha "$reviewed_sha" '
  (.status == "ahead" or .status == "identical")
  and .base_commit.sha == $sha
  and .merge_base_commit.sha == $sha
' >/dev/null <<<"$upstream_comparison" \
  || fail "${reviewed_sha} is not in the hard-pinned markusleben/ha-nova main history"

worker_secrets="$(
  CLOUDFLARE_ENV='' CLOUDFLARE_ACCOUNT_ID="$expected_account" \
    npx --yes wrangler@4.113.0 secret list \
      --cwd "$worker_dir" \
      --config "$config_file" \
      --name "$expected_worker" \
      --format json
)" || fail "could not list production Worker secrets"
jq -e '
  type == "array"
  and ([.[].name] | index("ACCESS_TEAM_DOMAIN") != null)
  and ([.[].name] | index("ACCESS_AUD") != null)
  and ([.[].name] | index("LOCAL_STATS_BYPASS_TOKEN") == null)
' >/dev/null <<<"$worker_secrets" \
  || fail "production Worker needs ACCESS_TEAM_DOMAIN/ACCESS_AUD and must not have LOCAL_STATS_BYPASS_TOKEN"

# Runtime-level, non-production proof: a fresh local workerd instance must
# accept one ping, execute the real Durable Object SQLite upsert, and expose
# that exact row through the loopback-only stats adapter.
local_port=$((20000 + ($$ % 20000)))
local_url="http://127.0.0.1:${local_port}"
local_log="${temp_dir}/wrangler-dev.log"
local_stats_token="local-release-smoke-$$"
MINIFLARE_CACHE_DIR="${temp_dir}/miniflare-cache" \
CLOUDFLARE_ENV='' CLOUDFLARE_ACCOUNT_ID="$expected_account" \
  npx --yes wrangler@4.113.0 dev \
    --cwd "$worker_dir" \
    --config "$config_file" \
    --name "$expected_worker" \
    --local \
    --ip 127.0.0.1 \
    --port "$local_port" \
    --inspector-port 0 \
    --var "LOCAL_STATS_BYPASS_TOKEN:${local_stats_token}" \
    --persist-to "${temp_dir}/worker-state" \
    --log-level error \
    --show-interactive-dev-session=false \
    >"$local_log" 2>&1 &
dev_pid=$!

local_ready=0
for ((attempt = 0; attempt < 100; attempt++)); do
  local_probe_status="$(curl --disable --silent --max-time 1 \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${local_url}/not-found" 2>/dev/null)" || local_probe_status=""
  if [[ "$local_probe_status" == "404" ]]; then
    local_ready=1
    break
  fi
  if ! kill -0 "$dev_pid" 2>/dev/null; then
    break
  fi
  sleep 0.1
done
if [[ "$local_ready" -ne 1 ]]; then
  sed -n '1,160p' "$local_log" >&2 || true
  fail "local Worker did not become ready"
fi

local_auth_status="$(curl --disable --silent --max-time 2 \
  --output /dev/null \
  --write-out '%{http_code}' \
  "${local_url}/stats/api")"
[[ "$local_auth_status" == "403" ]] \
  || fail "local stats no-credential probe returned HTTP ${local_auth_status}"

local_auth_status="$(curl --disable --silent --max-time 2 \
  --header "X-HA-NOVA-Local-Stats-Token: wrong" \
  --output /dev/null \
  --write-out '%{http_code}' \
  "${local_url}/stats/api")"
[[ "$local_auth_status" == "403" ]] \
  || fail "local stats wrong-credential probe returned HTTP ${local_auth_status}"

legacy_status="$(curl --disable --silent --show-error --request POST \
  --max-time 5 \
  --header 'Content-Type: application/json' \
  --data '{"schema":1,"version":"0.21.2","relay":"0.7.1","os":"linux"}' \
  --output /dev/null \
  --write-out '%{http_code}' \
  "${local_url}/ping")"
[[ "$legacy_status" == "204" ]] || fail "local schema-1 compatibility ping returned HTTP ${legacy_status}"

local_id="cns-0123456789abcdef0123456789abcdef"
local_status="$({
  curl --disable --request POST \
    --silent --show-error \
    --max-time 5 \
    --header 'Content-Type: application/json' \
    --data "{\"schema\":2,\"installation_id\":\"${local_id}\",\"version\":\"0.0.0\",\"os\":\"linux\"}" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${local_url}/ping"
})" || fail "local Worker POST failed"
[[ "$local_status" == "204" ]] || fail "local Worker POST returned HTTP ${local_status}"

local_status="$({
  curl --disable --request POST \
    --silent --show-error \
    --max-time 5 \
    --header 'Content-Type: application/json' \
    --data "{\"schema\":2,\"installation_id\":\"${local_id}\",\"version\":\"0.0.0\",\"os\":\"linux\"}" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${local_url}/ping"
})" || fail "second local Worker POST failed"
[[ "$local_status" == "204" ]] || fail "second local Worker POST returned HTTP ${local_status}"

local_stats="$(curl --disable --silent --show-error --fail --max-time 5 \
  --header "X-HA-NOVA-Local-Stats-Token: ${local_stats_token}" \
  "${local_url}/stats/api?release_smoke=$$")" \
  || fail "local Worker stats read failed"
jq -e '
  .schema == 2
  and .client_installations.active_21_days == 1
  and .client_installations.known_60_days == 1
  and .client_installations.by_os == {"linux": 1}
  and .client_installations.by_version == {"0.0.0": 1}
  and .client_installations.relay_not_recently_observed == 1
  and (.legacy_ping_activity.weekly | map(.count) | add) == 1
' >/dev/null <<<"$local_stats" || fail "local Worker did not deduplicate two reports from one ID"

local_status="$({
  curl --disable --request POST \
    --silent --show-error \
    --max-time 5 \
    --header 'Content-Type: application/json' \
    --data "{\"schema\":2,\"installation_id\":\"${local_id}\"}" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${local_url}/withdraw"
})" || fail "local Worker withdrawal failed"
[[ "$local_status" == "204" ]] || fail "local Worker withdrawal returned HTTP ${local_status}"

local_stats="$(curl --disable --silent --show-error --fail --max-time 5 \
  --header "X-HA-NOVA-Local-Stats-Token: ${local_stats_token}" \
  "${local_url}/stats/api?release_withdraw_smoke=$$")" \
  || fail "local Worker post-withdrawal stats read failed"
jq -e '
  .client_installations.active_21_days == 0
  and .client_installations.known_60_days == 0
' >/dev/null <<<"$local_stats" || fail "local Worker withdrawal did not remove the smoke installation"

kill "$dev_pid" 2>/dev/null || true
wait "$dev_pid" 2>/dev/null || true
dev_pid=""
echo "[deploy-census-worker] local Worker + Durable Object write/read smoke OK"

[[ -z "$(git -C "$root_dir" status --porcelain)" ]] \
  || fail "checkout changed during local proof; refusing to deploy unreviewed files"
actual_sha="$(git -C "$root_dir" rev-parse HEAD)"
[[ "$actual_sha" == "$reviewed_sha" ]] \
  || fail "HEAD changed during local proof (${actual_sha}); refusing to deploy"

previous_deployment="$(read_current_deployment)" \
  || fail "could not read the current production Worker deployment"
rollback_version="$(single_deployment_version_id <<<"$previous_deployment")" \
  || fail "current Worker is not a single 100-percent version; refuse an ambiguous rollback target"
# Census deploys are a serialized single-writer operation. When Wrangler
# identifies this run's deployed version, cleanup refuses to overwrite a
# different active version.
rollback_armed=1

wrangler_output="${temp_dir}/wrangler-output.ndjson"
CLOUDFLARE_ENV='' \
CLOUDFLARE_ACCOUNT_ID="$expected_account" \
WRANGLER_OUTPUT_FILE_PATH="$wrangler_output" \
  npx --yes wrangler@4.113.0 deploy \
    --cwd "$worker_dir" \
    --config "$config_file" \
    --name "$expected_worker" \
    --tag "$reviewed_sha" \
    --message "HA NOVA reviewed merge ${reviewed_sha}" \
    --strict \
    --no-autoconfig

[[ -s "$wrangler_output" ]] || fail "Wrangler wrote no structured deployment output"
deployed_version="$(deployment_output_version_id "$wrangler_output")" \
  || fail "Wrangler output did not identify exactly one safe deployed version"
deploy_record="$({
  jq -sce \
    --arg worker "$expected_worker" \
    --arg target "$expected_target" '
      [.[] | select(.type == "deploy")] as $deploys
      | ($deploys | length) == 1
      and $deploys[0].worker_name == $worker
      and $deploys[0].targets == [$target]
      and (
        $deploys[0].version_id
        | type == "string"
          and test("^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$")
      )
      | if . then $deploys[0] else error("unexpected deployment target") end
    ' "$wrangler_output"
})" || fail "Wrangler output did not attest exactly ${expected_worker} at ${expected_target}"
version_id="$(jq -er '.version_id' <<<"$deploy_record")"
[[ "$version_id" == "$deployed_version" ]] \
  || fail "Wrangler deployment output reported inconsistent version IDs"

HA_NOVA_CENSUS_ACCESS_CLIENT_ID="$access_id" \
HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET="$access_secret" \
  bash "${script_dir}/verify-census-deployment.sh" "$reviewed_sha" "$version_id"
rollback_armed=0

echo "[deploy-census-worker] OK: ${expected_worker} ${version_id} from ${reviewed_sha}"
