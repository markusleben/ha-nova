#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-census-deployment.sh <expected-sha> <expected-version-id>

Requires HA_NOVA_CENSUS_ACCESS_CLIENT_ID and
HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET for a Cloudflare Access service-token
policy. Verifies the private stats contract and exact Worker deployment, then
uses one ephemeral schema-2 ID to prove deduplication and withdrawal.
EOF
}

fail() {
  echo "[verify-census-deployment] ERROR: $*" >&2
  exit 1
}

if [[ "$#" -ne 2 ]]; then
  usage
  exit 2
fi

expected_sha="$1"
expected_version_id="$2"
[[ "$expected_sha" =~ ^[0-9a-f]{40}$ ]] || fail "expected SHA must be 40 lowercase hexadecimal characters"
[[ "$expected_version_id" =~ ^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$ ]] || fail "invalid Cloudflare version ID"

for command in awk curl jq openssl; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

access_id="${HA_NOVA_CENSUS_ACCESS_CLIENT_ID:-}"
access_secret="${HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET:-}"
[[ -n "$access_id" ]] || fail "HA_NOVA_CENSUS_ACCESS_CLIENT_ID is required"
[[ -n "$access_secret" ]] || fail "HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET is required"
[[ "$access_id" != *$'\n'* && "$access_id" != *$'\r'* ]] || fail "Access client ID contains a line break"
[[ "$access_secret" != *$'\n'* && "$access_secret" != *$'\r'* ]] || fail "Access client secret contains a line break"

base_url="https://ha-nova-census.markusleben.workers.dev"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-census-verify.XXXXXX")"
installation_id=""
baseline_smoke_count=""
smoke_version="0.0.0-rc999999"
cleanup() {
  cleanup_status=$?
  if [[ -n "$installation_id" ]]; then
    if ! withdraw_smoke_and_restore; then
      echo "[verify-census-deployment] ERROR: automatic cleanup failed; manually POST {\"schema\":2,\"installation_id\":\"${installation_id}\"} to ${base_url}/withdraw and verify the reserved release-smoke count returns to ${baseline_smoke_count}" >&2
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
printf 'CF-Access-Client-Id: %s\nCF-Access-Client-Secret: %s\n' \
  "$access_id" "$access_secret" >"$access_headers"
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

verify_contract() {
  local deployment_sha version_id
  deployment_sha="$(awk 'tolower($1) == "x-ha-nova-deployment-sha:" { gsub("\\r", "", $2); value=$2 } END { print value }' "$headers_file")"
  version_id="$(awk 'tolower($1) == "x-ha-nova-version-id:" { gsub("\\r", "", $2); value=$2 } END { print value }' "$headers_file")"
  [[ "$deployment_sha" == "$expected_sha" ]] || return 1
  [[ "$version_id" == "$expected_version_id" ]] || return 1
  jq -e '
    def nonnegint: type == "number" and . >= 0 and floor == .;
    def counts: type == "object" and all(.[]; nonnegint);
    .schema == 2
    and (.generated_at | type == "string")
    and (.client_installations.active_21_days | nonnegint)
    and (.client_installations.known_60_days | nonnegint)
    and (.client_installations.release_smoke_installations | nonnegint)
    and .client_installations.active_21_days <= .client_installations.known_60_days
    and .client_installations.release_smoke_installations <= .client_installations.active_21_days
    and (.client_installations.by_version | counts)
    and (.client_installations.by_os | counts)
    and (.client_installations.relay_versions | counts)
    and (.client_installations.relay_not_recently_observed | nonnegint)
    and (.client_installations.new_installation_rejections_today | nonnegint)
    and ([.client_installations.by_version[]] | add // 0) == .client_installations.active_21_days
    and ([.client_installations.by_os[]] | add // 0) == .client_installations.active_21_days
    and (([.client_installations.relay_versions[]] | add // 0) + .client_installations.relay_not_recently_observed) == .client_installations.active_21_days
    and .relay_app_installations.source == "https://analytics.home-assistant.io/addons.json"
    and .relay_app_installations.slug == "2368fcfa_ha_nova_relay"
    and (
      (.relay_app_installations.status == "available"
        and (.relay_app_installations.total | nonnegint)
        and (.relay_app_installations.by_version | counts)
        and ([.relay_app_installations.by_version[]] | add // 0) == .relay_app_installations.total
        and (.relay_app_installations | has("error") | not))
      or
      (.relay_app_installations.status == "unavailable"
        and (.relay_app_installations.error | type == "string" and length > 0)
        and (.relay_app_installations | has("total") | not)
        and (.relay_app_installations | has("by_version") | not))
    )
    and (.legacy_ping_activity.weekly | type == "array"
      and all(.[];
        (.iso_week | type == "string" and test("^[0-9]{4}-W[0-9]{2}$"))
        and (.count | nonnegint)))
    and ([paths | select(.[-1] == "id_hash" or .[-1] == "installation_id")] | length == 0)
  ' >/dev/null "$payload_file"
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
      current="$(jq -er '
        .client_installations.release_smoke_installations
        | select(type == "number" and . >= 0 and floor == .)
      ' "$payload_file")" || current=""
      if [[ "$current" == "$baseline_smoke_count" ]]; then
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

verified=0
for ((attempt = 1; attempt <= 30; attempt++)); do
  if fetch_stats "$(date -u +%s)-$$-${attempt}" && verify_contract; then
    verified=1
    break
  fi
  sleep 2
done
[[ "$verified" -eq 1 ]] || fail "private stats did not expose the reviewed Worker and schema-2 contract"

baseline_smoke_count="$(jq -er '
  .client_installations.release_smoke_installations
  | select(type == "number" and . >= 0 and floor == .)
' "$payload_file")"
installation_id="cns-$(openssl rand -hex 16)"
ping_body="$(jq -cn --arg id "$installation_id" --arg version "$smoke_version" \
  '{schema:2,installation_id:$id,version:$version,os:"linux"}')"

for attempt in 1 2; do
  status="$(curl --disable --silent --show-error --max-time 15 --proto '=https' \
    --request POST --header 'Content-Type: application/json' \
    --data "$ping_body" --output /dev/null --write-out '%{http_code}' \
    "${base_url}/ping")"
  [[ "$status" == "204" ]] || fail "production schema-2 ping ${attempt} returned HTTP ${status}"
done

deduplicated=0
for ((attempt = 1; attempt <= 15; attempt++)); do
  fetch_stats "dedup-$(date -u +%s)-$$-${attempt}"
  current="$(jq -er '
    .client_installations.release_smoke_installations
    | select(type == "number" and . >= 0 and floor == .)
  ' "$payload_file")" || current=""
  if [[ -n "$current" && "$current" -eq $((baseline_smoke_count + 1)) ]]; then
    deduplicated=1
    break
  fi
  sleep 1
done
[[ "$deduplicated" -eq 1 ]] || fail "two reports from one ephemeral ID did not produce exactly one active installation"

withdraw_smoke_and_restore \
  || fail "withdrawal did not return HTTP 204 and restore the pre-smoke version count"
installation_id=""

echo "[verify-census-deployment] OK: ${expected_sha}/${expected_version_id}, private stats, one-ID deduplication, and withdrawal"
