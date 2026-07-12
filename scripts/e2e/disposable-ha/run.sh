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
  # Home Assistant writes the bind mount as root (.storage and friends), so a
  # non-root CI runner cannot remove it directly — a failing cleanup would turn
  # a green evidence run red. Delete it from inside a container, which can.
  if [[ -d "$HERE/config" ]]; then
    docker run --rm -v "$HERE/config:/target" alpine:3 \
      sh -c 'rm -rf /target/* /target/.[!.]* /target/..?* 2>/dev/null || true' > /dev/null 2>&1 || true
    rmdir "$HERE/config" 2>/dev/null || rm -rf "$HERE/config" 2>/dev/null || true
  fi
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
# The integration step needs a redirect_uri as well as a client_id.
curl -sf -X POST "$HA_URL/api/onboarding/core_config" \
  -H "Authorization: Bearer ${access_token}" -H 'Content-Type: application/json' \
  -d '{"client_id":"http://127.0.0.1:8123/"}' > /dev/null
curl -sf -X POST "$HA_URL/api/onboarding/analytics" \
  -H "Authorization: Bearer ${access_token}" -H 'Content-Type: application/json' \
  -d '{"client_id":"http://127.0.0.1:8123/"}' > /dev/null
curl -sf -X POST "$HA_URL/api/onboarding/integration" \
  -H "Authorization: Bearer ${access_token}" -H 'Content-Type: application/json' \
  -d '{"client_id":"http://127.0.0.1:8123/","redirect_uri":"http://127.0.0.1:8123/"}' > /dev/null

# Verify the RESULT rather than trusting the steps: /api/onboarding reports each
# step's done flag. A half-onboarded Home Assistant would make every assertion
# below meaningless, so this is a hard failure, not a warning.
onboarding_state="$(curl -sf "$HA_URL/api/onboarding")"
if echo "$onboarding_state" | python3 -c 'import json,sys; steps=json.load(sys.stdin); sys.exit(0 if all(s["done"] for s in steps) else 1)'; then
  pass "onboarding completed (all steps report done)"
else
  echo "  FAIL: onboarding is incomplete: $onboarding_state" >&2
  exit 1
fi

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

# File access must be OFF by default — the endpoint exists, but refuses. This is
# the guarantee the whole opt-in design rests on, so it is verified live.
files_off="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$RELAY_URL/files" \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' \
  -d '{"action":"list_dir","path":"/config"}')"
if [[ "$files_off" == "403" ]]; then
  pass "file access is off by default (403)"
else
  fail "file access should be disabled by default, got HTTP $files_off"
fi

# The opted-in side of the same guarantee: a second relay mounts the real HA
# config directory with FILE_ACCESS=readwrite and must serve the full
# write -> read-back -> deny -> delete roundtrip the yaml-config skill uses.
echo "--- file access: readwrite roundtrip (second relay on :8792)"
( cd "$HERE" && docker compose up -d --build relay-files > /dev/null 2>&1 )
FILES_URL="http://127.0.0.1:8792"
for i in $(seq 1 30); do
  code="$(curl -s -o /dev/null -w '%{http_code}' "$FILES_URL/health" || true)"
  if [[ "$code" == "401" ]]; then break; fi
  if [[ "$i" == "30" ]]; then echo "relay-files did not start (last: ${code:-none})" >&2; ( cd "$HERE" && docker compose logs relay-files ); exit 1; fi
  sleep 2
done

files_call() {
  curl -s -X POST "$FILES_URL/files" \
    -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' -d "$1"
}
files_code() {
  curl -s -o /dev/null -w '%{http_code}' -X POST "$FILES_URL/files" \
    -H "Authorization: Bearer $RELAY_AUTH_TOKEN" -H 'Content-Type: application/json' -d "$1"
}

listing="$(files_call '{"action":"list_dir","path":"/config"}')"
if echo "$listing" | grep -q 'configuration.yaml'; then
  pass "list_dir sees the real Home Assistant config"
else
  fail "list_dir did not show configuration.yaml: $listing"
fi

wrote="$(files_call '{"action":"write_file","path":"/config/ha_nova/e2e-roundtrip.yaml","content":"e2e_marker: disposable-ha\n"}')"
if echo "$wrote" | grep -q '"ok":true'; then
  pass "write_file creates a file under /config/ha_nova/"
else
  fail "write_file failed: $wrote"
fi

readback="$(files_call '{"action":"read_file","path":"/config/ha_nova/e2e-roundtrip.yaml"}')"
if echo "$readback" | grep -q 'e2e_marker: disposable-ha'; then
  pass "read_file returns the written content verbatim"
else
  fail "read-back did not match: $readback"
fi

secrets_deny="$(files_code '{"action":"write_file","path":"/config/secrets.yaml","content":"x: y\n"}')"
if [[ "$secrets_deny" == "403" ]]; then
  pass "secrets.yaml stays unreachable even with readwrite (403)"
else
  fail "secrets.yaml write should be denied, got HTTP $secrets_deny"
fi

exec_deny="$(files_code '{"action":"write_file","path":"/config/ha_nova/evil.sh","content":"#!/bin/sh\n"}')"
if [[ "$exec_deny" == "403" ]]; then
  pass "non-configuration file types are refused (403)"
else
  fail ".sh write should be denied, got HTTP $exec_deny"
fi

deleted="$(files_call '{"action":"delete_file","path":"/config/ha_nova/e2e-roundtrip.yaml"}')"
gone="$(files_code '{"action":"read_file","path":"/config/ha_nova/e2e-roundtrip.yaml"}')"
if echo "$deleted" | grep -q '"ok":true' && [[ "$gone" == "404" ]]; then
  pass "delete_file removes the file (read-back 404)"
else
  fail "delete roundtrip failed: delete=$deleted read-back=HTTP $gone"
fi

echo
if [[ "$FAILED" == "1" ]]; then
  echo "E2E FAILED" >&2
  ( cd "$HERE" && docker compose logs relay relay-files | tail -40 )
  exit 1
fi
echo "E2E PASSED — verified against a real Home Assistant $(date -u +%Y-%m-%d)"
