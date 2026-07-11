#!/usr/bin/env bash
# End-to-end verification against a REAL, disposable Home Assistant.
#
# Boots Home Assistant, completes its onboarding through the API, mints a
# long-lived access token, starts the standalone relay container against it,
# and asserts the guarantees that only a live system can prove:
#   - authentication is enforced (401 without a token)
#   - the relay reports its baked-in version, so the CLI's compatibility check works
#   - a health probe does NOT open the upstream WebSocket (no lazy-connect)...
#   - ...but /health honestly reports the connection once real traffic opened it
#   - REST and WS proxy calls return real Home Assistant data
#   - a bare subscription is rejected, but a bounded window is served
#
# Everything is destroyed afterwards. Run: bash scripts/e2e/disposable-ha/run.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HA_URL="http://127.0.0.1:8123"
RELAY_URL="http://127.0.0.1:8791"
export RELAY_AUTH_TOKEN="e2e-relay-token-$RANDOM"
export RELAY_VERSION="$(grep -E '^version:' "$HERE/../../../nova/config.yaml" | head -1 | sed -E 's/^version:[[:space:]]*"?([^"]+)"?/\1/')"

pass() { echo "  ok: $*"; }
fail() { echo "  FAIL: $*" >&2; FAILED=1; }
FAILED=0

cleanup() {
  echo "--- tearing down"
  ( cd "$HERE" && docker compose down -v --remove-orphans > /dev/null 2>&1 || true )
  rm -rf "$HERE/config"
}
trap cleanup EXIT

echo "--- booting a disposable Home Assistant (relay version: $RELAY_VERSION)"
rm -rf "$HERE/config"
mkdir -p "$HERE/config"
( cd "$HERE" && docker compose up -d homeassistant )

echo "--- waiting for Home Assistant"
for i in $(seq 1 90); do
  if curl -sf "$HA_URL/" > /dev/null 2>&1; then break; fi
  if [[ "$i" == "90" ]]; then echo "Home Assistant did not start" >&2; exit 1; fi
  sleep 2
done
pass "Home Assistant is up"

echo "--- onboarding + minting a long-lived token"
# A fresh HA exposes an onboarding API exactly once; this is the supported way
# to create the owner user without touching the UI.
auth_code="$(curl -sf -X POST "$HA_URL/api/onboarding/users" \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"http://127.0.0.1:8123/","name":"E2E","username":"e2e","password":"e2e-password-1234","language":"en"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["auth_code"])')"

access_token="$(curl -sf -X POST "$HA_URL/auth/token" \
  -d "client_id=http://127.0.0.1:8123/&grant_type=authorization_code&code=${auth_code}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"

# Finish onboarding so the core config exists and integrations can load.
for step in core_config analytics integration; do
  curl -sf -X POST "$HA_URL/api/onboarding/$step" \
    -H "Authorization: Bearer ${access_token}" \
    -H 'Content-Type: application/json' \
    -d '{"client_id":"http://127.0.0.1:8123/"}' > /dev/null 2>&1 || true
done

HA_LLAT="$(python3 "$HERE/mint-llat.py" "$HA_URL" "$access_token")"
export HA_LLAT
pass "long-lived token minted"

echo "--- starting the standalone relay container against it"
( cd "$HERE" && docker compose up -d --build relay )
for i in $(seq 1 30); do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$RELAY_URL/health" || true)"
  if [[ "$code" == "401" ]]; then break; fi
  if [[ "$i" == "30" ]]; then echo "relay did not start (last: ${code:-none})" >&2; ( cd "$HERE" && docker compose logs relay ); exit 1; fi
  sleep 2
done
pass "relay rejects unauthenticated requests (401)"

echo "--- asserting the live guarantees"
health="$(curl -sf -H "Authorization: Bearer $RELAY_AUTH_TOKEN" "$RELAY_URL/health")"
echo "      $health"

python3 - "$health" "$RELAY_VERSION" <<'PY' || FAILED=1
import json, sys
health = json.loads(sys.argv[1])["data"]
expected_version = sys.argv[2]
ok = True
if health.get("version") != expected_version:
    print(f"  FAIL: relay reports version {health.get('version')!r}, expected {expected_version!r} (the baked-in version is what the CLI compatibility check reads)", file=sys.stderr)
    ok = False
else:
    print(f"  ok: relay reports its baked-in version ({expected_version})")

# A health probe must NOT open the upstream WebSocket — the connection is
# established lazily by real traffic (docs/choices.md: no lazy-connect through
# health probes, no added latency). So ha_ws_connected is expected to be false
# here, BEFORE any WS call. It is asserted true further down, after real WS use.
if health.get("ha_ws_connected") is True:
    print("  FAIL: a health probe opened the upstream WebSocket — probes must not connect", file=sys.stderr)
    ok = False
else:
    print("  ok: a health probe does not open the upstream WebSocket (no lazy-connect)")
sys.exit(0 if ok else 1)
PY

# REST proxy: real Home Assistant config comes back.
core_body="$(curl -sf -X POST "$RELAY_URL/core" \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/config"}')"
if echo "$core_body" | grep -q '"version"'; then
  pass "REST proxy returns live Home Assistant data"
else
  fail "REST proxy did not return HA config: $core_body"
fi

# WS proxy: real registry data comes back.
ws_body="$(curl -sf -X POST "$RELAY_URL/ws" \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"type":"config/area_registry/list"}')"
if echo "$ws_body" | grep -q '"ok":true'; then
  pass "WS proxy returns live Home Assistant data"
else
  fail "WS proxy failed: $ws_body"
fi

# ...and NOW health must honestly report the live WS connection that the call
# above established. This is the pair that matters: probes do not connect, but
# once there is a real connection, /health says so.
health_after="$(curl -sf -H "Authorization: Bearer $RELAY_AUTH_TOKEN" "$RELAY_URL/health")"
if echo "$health_after" | grep -q '"ha_ws_connected":true'; then
  pass "health reports the live WebSocket connection after real WS traffic"
else
  fail "health should report ha_ws_connected=true after a WS call: $health_after"
fi

# A bare subscription must be rejected — the relay holds no long-lived subscriptions.
bare="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$RELAY_URL/ws" \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"type":"subscribe_events"}')"
if [[ "$bare" == "400" ]]; then
  pass "bare subscription rejected (400)"
else
  fail "bare subscription should be rejected, got HTTP $bare"
fi

# ...but a BOUNDED window is served and unsubscribes itself.
window="$(curl -sf -X POST "$RELAY_URL/ws" \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"message":{"type":"subscribe_events","event_type":"state_changed"},"collect_events":{"max_events":3,"timeout_ms":3000,"on_limit":"return"}}')"
if echo "$window" | grep -q '"events"'; then
  pass "bounded event window is served (envelope v2)"
else
  fail "bounded window failed: $window"
fi

echo
if [[ "$FAILED" == "1" ]]; then
  echo "E2E FAILED" >&2
  ( cd "$HERE" && docker compose logs relay | tail -30 )
  exit 1
fi
echo "E2E PASSED — verified against a real Home Assistant $(date -u +%Y-%m-%d)"
