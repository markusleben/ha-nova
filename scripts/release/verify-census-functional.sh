#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-census-functional.sh

Functional census verification — ping, deduplication, and withdrawal — run
EXCLUSIVELY against the isolated test Worker (wrangler env "test", its own
Durable Object namespace). This script hardcodes the test host and must never
point at production; the production deployment check is read-only by contract
(#446, scripts/release/verify-census-deployment.sh).

Deploy the target first:  cd census-worker && npx wrangler@4.113.0 deploy --env test

Optional HA_NOVA_CENSUS_ACCESS_CLIENT_ID / HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET
are sent when set (if the test Worker sits behind the same Access policy).
EOF
}

fail() {
  echo "[verify-census-functional] ERROR: $*" >&2
  exit 1
}

if [[ "$#" -ne 0 ]]; then
  usage
  exit 2
fi

for command in curl jq openssl; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

# The ISOLATED test Worker (wrangler.toml [env.test]) — a distinct Worker with
# its own Durable Object storage. Production is ha-nova-census (no suffix) and
# is off-limits here by contract.
base_url="https://ha-nova-census-test.markusleben.workers.dev"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-census-functional.XXXXXX")"
installation_id=""
baseline_smoke_count=""
smoke_version="0.0.0-rc999999"
cleanup() {
  cleanup_status=$?
  if [[ -n "$installation_id" ]]; then
    if ! withdraw_smoke_and_restore; then
      echo "[verify-census-functional] ERROR: automatic cleanup failed; manually POST {\"schema\":2,\"installation_id\":\"${installation_id}\"} to ${base_url}/withdraw" >&2
      cleanup_status=1
    fi
  fi
  rm -rf -- "$temp_dir"
  trap - EXIT
  exit "$cleanup_status"
}
trap cleanup EXIT
headers_file="${temp_dir}/headers"
payload_file="${temp_dir}/payload"
access_headers="${temp_dir}/access-headers"
umask 077
: >"$access_headers"
if [[ -n "${HA_NOVA_CENSUS_ACCESS_CLIENT_ID:-}" && -n "${HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET:-}" ]]; then
  printf 'CF-Access-Client-Id: %s\nCF-Access-Client-Secret: %s\n' \
    "$HA_NOVA_CENSUS_ACCESS_CLIENT_ID" "$HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET" >"$access_headers"
fi
unset HA_NOVA_CENSUS_ACCESS_CLIENT_ID HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET

fetch_stats() {
  local nonce="$1"
  curl --disable --silent --show-error --fail \
    --connect-timeout 5 \
    --max-time 15 \
    --proto '=https' \
    --header 'Accept: application/json' \
    --header 'Cache-Control: no-cache' \
    --header "@${access_headers}" \
    --dump-header "$headers_file" \
    --output "$payload_file" \
    "${base_url}/stats/api?ha_nova_release_gate=${nonce}"
}

read_smoke_count() {
  jq -er '
    .client_installations.release_smoke_installations
    | select(type == "number" and . >= 0 and floor == .)
  ' "$payload_file"
}

withdraw_smoke_and_restore() {
  local status current
  for ((attempt = 1; attempt <= 5; attempt++)); do
    status="$(curl --disable --silent --show-error --max-time 15 --proto '=https' \
      --request POST --header 'Content-Type: application/json' \
      --data "{\"schema\":2,\"installation_id\":\"${installation_id}\"}" \
      --output /dev/null --write-out '%{http_code}' \
      "${base_url}/withdraw")" || status=""
    if [[ "$status" == "204" ]]; then
      break
    fi
    sleep 1
  done
  [[ "$status" == "204" ]] || return 1
  for ((attempt = 1; attempt <= 15; attempt++)); do
    if fetch_stats "withdraw-$(date -u +%s)-$$-${attempt}"; then
      current="$(read_smoke_count)" || current=""
      if [[ "$current" == "$baseline_smoke_count" ]]; then
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

fetch_stats "$(date -u +%s)-$$-baseline" || fail "test worker stats unavailable — deploy with: cd census-worker && npx wrangler@4.113.0 deploy --env test"
baseline_smoke_count="$(read_smoke_count)" || fail "test worker stats do not expose the schema-2 smoke count"
installation_id="cns-$(openssl rand -hex 16)"
ping_body="$(jq -cn --arg id "$installation_id" --arg version "$smoke_version" \
  '{schema:2,installation_id:$id,version:$version,os:"linux"}')"

for attempt in 1 2; do
  status="$(curl --disable --silent --show-error --max-time 15 --proto '=https' \
    --request POST --header 'Content-Type: application/json' \
    --data "$ping_body" --output /dev/null --write-out '%{http_code}' \
    "${base_url}/ping")"
  [[ "$status" == "204" ]] || fail "test-worker schema-2 ping ${attempt} returned HTTP ${status}"
done

deduplicated=0
for ((attempt = 1; attempt <= 15; attempt++)); do
  fetch_stats "dedup-$(date -u +%s)-$$-${attempt}"
  current="$(read_smoke_count)" || current=""
  if [[ -n "$current" && "$current" -eq $((baseline_smoke_count + 1)) ]]; then
    deduplicated=1
    break
  fi
  sleep 1
done
[[ "$deduplicated" -eq 1 ]] || fail "two reports from one ephemeral ID did not produce exactly one active installation"

withdraw_smoke_and_restore \
  || fail "withdrawal did not return HTTP 204 and restore the pre-smoke count"
installation_id=""

echo "[verify-census-functional] OK (isolated test worker): one-ID deduplication and withdrawal"
