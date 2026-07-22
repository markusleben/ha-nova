#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-census-deployment.sh <expected-sha> <expected-version-id> [--require-empty]

Performs cache-busted GETs of the public census /stats endpoint and validates
the v1 response contract plus the exact Cloudflare deployment metadata.
--require-empty additionally proves a clean first-launch namespace: no weekly
rows, no breakdown rows, and a zero peak.
EOF
}

fail() {
  echo "[verify-census-deployment] ERROR: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

if [[ "$#" -lt 2 || "$#" -gt 3 ]]; then
  usage
  exit 2
fi
expected_sha="$1"
expected_version_id="$2"
[[ "$expected_sha" =~ ^[0-9a-f]{40}$ ]] || fail "expected SHA must be exactly 40 lowercase hexadecimal characters"
[[ "$expected_version_id" =~ ^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$ ]] || fail "invalid expected Cloudflare version ID"

require_empty=0
if [[ "$#" -eq 3 ]]; then
  if [[ "$3" == "--require-empty" ]]; then
    require_empty=1
  else
      usage
      exit 2
  fi
fi

require_command curl
require_command jq

stats_url="https://ha-nova-census.markusleben.workers.dev/stats"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-census-verify.XXXXXX")"
trap 'rm -rf -- "$temp_dir"' EXIT
headers_file="${temp_dir}/headers"
payload_file="${temp_dir}/payload"

verified=0
for ((attempt = 1; attempt <= 30; attempt++)); do
  cache_busted_url="${stats_url}?ha_nova_release_gate=$(date -u +%s)-$$-${attempt}"
  http_status="$(
    curl --disable --request GET \
      --silent --show-error \
      --connect-timeout 5 \
      --max-time 15 \
      --proto '=https' \
      --header 'Accept: application/json' \
      --header 'Cache-Control: no-cache' \
      --dump-header "$headers_file" \
      --output "$payload_file" \
      --write-out '%{http_code}' \
      "$cache_busted_url"
  )" || http_status=""
  deployment_sha="$(awk 'tolower($1) == "x-ha-nova-deployment-sha:" { gsub("\\r", "", $2); value=$2 } END { print value }' "$headers_file" 2>/dev/null || true)"
  version_id="$(awk 'tolower($1) == "x-ha-nova-version-id:" { gsub("\\r", "", $2); value=$2 } END { print value }' "$headers_file" 2>/dev/null || true)"

  if [[ "$http_status" == "200" && "$deployment_sha" == "$expected_sha" && "$version_id" == "$expected_version_id" ]] && jq -e '
  type == "object"
  and .schema == 1
  and (.generated_at | type == "string")
  and (.weekly | type == "array")
  and all(.weekly[];
    (.iso_week | type == "string")
    and (.count | type == "number" and . >= 0 and floor == .)
  )
  and .window_weeks == 4
  and (.by_os | type == "object")
  and all(.by_os[]; type == "number" and . >= 0 and floor == .)
  and (.by_version | type == "object")
  and all(.by_version[]; type == "number" and . >= 0 and floor == .)
  and (.by_relay | type == "object")
  and all(.by_relay[]; type == "number" and . >= 0 and floor == .)
  and (.peak_weekly_pings | type == "number" and . >= 0 and floor == .)
  and ([paths | select(.[-1] == "monthly_lower_bound")] | length == 0)
  and (.footnotes.counting | type == "string")
  and (.footnotes.identifiers | type == "string")
  and (
    .footnotes.counting
    | ascii_downcase
    | contains("not verified unique installs")
      and contains("duplicates")
      and contains("fabricated")
  )
' >/dev/null "$payload_file"; then
    verified=1
    break
  fi
  if [[ "$attempt" -lt 30 ]]; then
    sleep 2
  fi
done

[[ "$verified" -eq 1 ]] || fail "${stats_url} did not expose reviewed SHA ${expected_sha}, version ${expected_version_id}, and the census v1 contract"
payload="$(cat "$payload_file")"

if [[ "$require_empty" -eq 1 ]] && ! jq -e '
  (.weekly | length) == 0
  and (.by_os | length) == 0
  and (.by_version | length) == 0
  and (.by_relay | length) == 0
  and .peak_weekly_pings == 0
' >/dev/null <<<"$payload"; then
  fail "${stats_url} is not an empty first-launch namespace"
fi

if [[ "$require_empty" -eq 1 ]]; then
  echo "[verify-census-deployment] OK: ${expected_sha}/${expected_version_id}, schema v1, honest counters, empty first-launch namespace"
else
  echo "[verify-census-deployment] OK: ${expected_sha}/${expected_version_id}, schema v1 and honest counter contract"
fi
