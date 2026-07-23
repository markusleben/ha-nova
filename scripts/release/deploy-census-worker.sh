#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha> [--require-empty]

From a clean checkout of the exact reviewed merge, exercises the Worker and
SQLite Durable Object locally, deploys the pinned production Worker, attests
Wrangler's exact worker/target/version output, then verifies that same public
Cloudflare version by SHA and version ID. --require-empty is mandatory for the
first public census launch.
EOF
}

fail() {
  echo "[deploy-census-worker] ERROR: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

if [[ "$#" -lt 1 || "$#" -gt 2 ]]; then
  usage
  exit 2
fi

reviewed_sha="$1"
require_empty=0
if [[ "$#" -eq 2 ]]; then
  [[ "$2" == "--require-empty" ]] || {
    usage
    exit 2
  }
  require_empty=1
fi
[[ "$reviewed_sha" =~ ^[0-9a-f]{40}$ ]] || fail "reviewed SHA must be exactly 40 lowercase hexadecimal characters"

require_command curl
require_command git
require_command gh
require_command jq
require_command node
require_command npx

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root_dir="$(cd "${script_dir}/../.." && pwd)"
worker_dir="${root_dir}/census-worker"
config_file="${worker_dir}/wrangler.toml"
expected_account="58e387e1204bdfe78781caca64f2cd15"
expected_worker="ha-nova-census"
expected_target="https://ha-nova-census.markusleben.workers.dev"

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

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-census-release.XXXXXX")"
dev_pid=""
cleanup() {
  if [[ -n "$dev_pid" ]] && kill -0 "$dev_pid" 2>/dev/null; then
    kill "$dev_pid" 2>/dev/null || true
    wait "$dev_pid" 2>/dev/null || true
  fi
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT

# Runtime-level, non-production proof: a fresh local workerd instance must
# accept one ping, execute the real Durable Object SQLite upsert, and expose
# that exact row through the public stats adapter.
local_port=$((20000 + ($$ % 20000)))
local_url="http://127.0.0.1:${local_port}"
local_log="${temp_dir}/wrangler-dev.log"
CLOUDFLARE_ENV='' CLOUDFLARE_ACCOUNT_ID="$expected_account" \
  npx --yes wrangler@4.113.0 dev \
    --cwd "$worker_dir" \
    --config "$config_file" \
    --name "$expected_worker" \
    --local \
    --ip 127.0.0.1 \
    --port "$local_port" \
    --inspector-port 0 \
    --persist-to "${temp_dir}/worker-state" \
    --log-level error \
    --show-interactive-dev-session=false \
    >"$local_log" 2>&1 &
dev_pid=$!

local_ready=0
for ((attempt = 0; attempt < 100; attempt++)); do
  if curl --disable --silent --fail --max-time 1 "${local_url}/stats" >/dev/null 2>&1; then
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

local_status="$({
  curl --disable --request POST \
    --silent --show-error \
    --max-time 5 \
    --header 'Content-Type: application/json' \
    --data '{"schema":1,"version":"0.0.0","os":"linux"}' \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${local_url}/ping"
})" || fail "local Worker POST failed"
[[ "$local_status" == "204" ]] || fail "local Worker POST returned HTTP ${local_status}"

local_stats="$(curl --disable --silent --show-error --fail --max-time 5 "${local_url}/stats?release_smoke=$$")" \
  || fail "local Worker stats read failed"
jq -e '
  .schema == 1
  and .weekly == [{"iso_week": .weekly[0].iso_week, "count": 1}]
  and .by_os == {"linux": 1}
  and .by_version == {"0.0.0": 1}
  and .by_relay == {"unknown": 1}
  and .peak_weekly_pings == 1
' >/dev/null <<<"$local_stats" || fail "local Worker did not persist and read back exactly one smoke ping"

kill "$dev_pid" 2>/dev/null || true
wait "$dev_pid" 2>/dev/null || true
dev_pid=""
echo "[deploy-census-worker] local Worker + Durable Object write/read smoke OK"

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
deploy_record="$({
  jq -sce \
    --arg worker "$expected_worker" \
    --arg target "$expected_target" '
      [.[] | select(.type == "deploy")] as $deploys
      | ($deploys | length) == 1
      and $deploys[0].worker_name == $worker
      and $deploys[0].targets == [$target]
      and ($deploys[0].version_id | type == "string" and length > 0)
      | if . then $deploys[0] else error("unexpected deployment target") end
    ' "$wrangler_output"
})" || fail "Wrangler output did not attest exactly ${expected_worker} at ${expected_target}"
version_id="$(jq -er '.version_id' <<<"$deploy_record")"

verify_args=("$reviewed_sha" "$version_id")
if [[ "$require_empty" -eq 1 ]]; then
  verify_args+=("--require-empty")
fi
bash "${script_dir}/verify-census-deployment.sh" "${verify_args[@]}"

echo "[deploy-census-worker] OK: ${expected_worker} ${version_id} from ${reviewed_sha}"
