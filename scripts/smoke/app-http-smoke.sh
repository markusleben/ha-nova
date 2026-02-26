#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8791}"
AUTH_TOKEN="${AUTH_TOKEN:-dev-relay-token}"

wait_for_health() {
  local max_attempts=20
  local attempt=1

  while [[ $attempt -le $max_attempts ]]; do
    if curl -fsS -H "Authorization: Bearer ${AUTH_TOKEN}" "$BASE_URL/health" >/dev/null 2>&1; then
      return 0
    fi

    sleep 0.5
    attempt=$((attempt + 1))
  done

  echo "Health endpoint not ready after ${max_attempts} attempts: ${BASE_URL}/health" >&2
  return 1
}

wait_for_health

curl -fsS -H "Authorization: Bearer ${AUTH_TOKEN}" "$BASE_URL/health"

WS_RESPONSE="$(
curl -sS \
  -X POST \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"type":"ping"}' \
  -w '\n%{http_code}' \
  "$BASE_URL/ws"
)"

WS_STATUS="$(printf '%s' "$WS_RESPONSE" | tail -n1)"
WS_BODY="$(printf '%s' "$WS_RESPONSE" | sed '$d')"

if [[ "$WS_STATUS" == "200" ]]; then
  printf '%s\n' "$WS_BODY"
elif [[ "$WS_STATUS" == "502" ]] && printf '%s' "$WS_BODY" | grep -q '"code":"UPSTREAM_WS'; then
  printf 'WS degraded mode accepted (expected outside Supervisor/HA runtime): %s\n' "$WS_BODY"
else
  printf 'Unexpected WS response (%s): %s\n' "$WS_STATUS" "$WS_BODY" >&2
  exit 1
fi

echo "HTTP smoke checks completed for $BASE_URL"
