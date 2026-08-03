#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-census-deployment.sh <expected-sha> <expected-version-id>

Requires HA_NOVA_CENSUS_ACCESS_CLIENT_ID and
HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET for a Cloudflare Access service-token
policy.

READ-ONLY by contract (#446): verifies deployment identity, authentication,
headers, and the schema-2 stats contract of the PRODUCTION census Worker.
It never calls the production mutation endpoints (ping, withdraw) —
production statistics represent voluntary real participants only. Functional
ping, deduplication, and withdrawal checks run exclusively against the
isolated test Worker via scripts/release/verify-census-functional.sh.
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

for command in awk curl jq; do
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
cleanup() {
  cleanup_status=$?
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

verified=0
for ((attempt = 1; attempt <= 30; attempt++)); do
  if fetch_stats "$(date -u +%s)-$$-${attempt}" && verify_contract; then
    verified=1
    break
  fi
  sleep 2
done
[[ "$verified" -eq 1 ]] || fail "private stats did not expose the reviewed Worker and schema-2 contract"

echo "[verify-census-deployment] OK (read-only): ${expected_sha}/${expected_version_id}, authentication, headers, and schema-2 stats contract"
