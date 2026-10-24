#!/usr/bin/env bash
# Boot the HA NOVA test bed: a throwaway LOCAL Home Assistant "house" with an
# MQTT broker and the standalone relay, all on 127.0.0.1 isolated ports.
#
#   bash scripts/e2e/testbed/up.sh      (or: npm run testbed:up)
#
# Steps: prerequisites -> boot HA + broker -> MQTT discovery -> onboarding ->
# long-lived token -> seed.mjs (MQTT entry, floors/areas/labels, registry) ->
# relay -> summary JSON. State lives in .state/ (gitignored); down.sh wipes it.
#
# Hosted demo (scripts/demo/hosted/reset.sh) only: TESTBED_OVERLAY=<dir> merges
# <dir>/docker-compose.yml, copies <dir>/config/ over the seed config, and boots
# Home Assistant + broker only (no relay, no CLI, no Go) and runs
# `node <dir>/demo.mjs setup` right after onboarding. TESTBED_OWNER_PASSWORD
# replaces the fixed local owner password.
set -euo pipefail

HERE="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
STATE="$HERE/.state"
HA_URL="http://127.0.0.1:18123"
RELAY_URL="http://127.0.0.1:18791"
OVERLAY="${TESTBED_OVERLAY:-}"
OWNER_PASSWORD="${TESTBED_OWNER_PASSWORD:-testbed-password-1234}"
COMPOSE=(docker compose -f "$HERE/docker-compose.yml")
if [[ -n "$OVERLAY" ]]; then COMPOSE+=(-f "$OVERLAY/docker-compose.yml"); fi
start_ts=$(date +%s)

die() { echo "testbed: $*" >&2; exit 1; }
step() { echo "--- $*" >&2; }

# ---- prerequisites (fail loud, no fallbacks) ----------------------------------
command -v docker > /dev/null || die "docker is required (https://docs.docker.com/get-docker/)"
docker compose version > /dev/null 2>&1 || die "docker compose v2 is required ('docker compose', not 'docker-compose')"
docker info > /dev/null 2>&1 || die "the docker daemon is not running"
command -v node > /dev/null || die "node >= 22.12 is required (built-in WebSocket)"
node -e 'process.exit(typeof WebSocket === "function" ? 0 : 1)' || die "node $(node --version) has no built-in WebSocket; use node >= 22.12"
if [[ -z "$OVERLAY" ]]; then
  command -v go > /dev/null || die "go is required to build the CLI (see CONTRIBUTING.md)"
  (cd "$ROOT/cli" && go build -o /dev/null .) || die "the CLI does not build with $(go version); cli/go.mod names the required Go toolchain"
fi
[[ "$OWNER_PASSWORD" =~ ^[A-Za-z0-9._-]{12,}$ ]] || die "TESTBED_OWNER_PASSWORD must be 12+ chars of [A-Za-z0-9._-]"
command -v curl > /dev/null || die "curl is required"
if [[ -n "$("${COMPOSE[@]}" ps -q 2> /dev/null)" ]]; then
  die "the test bed is already running; run down.sh first (npm run testbed:down)"
fi
# The hosted demo publishes nothing from this project (its Caddy, in another
# project, already holds 127.0.0.1:18123).
[[ -n "$OVERLAY" ]] || for port in 18123 18791 11883 18792; do
  if (exec 3<> "/dev/tcp/127.0.0.1/$port") 2> /dev/null; then
    die "port $port is in use on 127.0.0.1; free it (the test bed never uses 8123/8791/1883)"
  fi
done

# ---- boot -----------------------------------------------------------------------
step "preparing runtime config in .state/config"
[[ -e "$STATE" ]] && die ".state exists from an earlier run; run down.sh first"
# A failed boot is left running for inspection; say how to look and clean up.
trap 'code=$?; [[ $code == 0 ]] || echo "testbed: up failed; inspect: docker compose -p ha-nova-testbed logs; clean up: npm run testbed:down" >&2' EXIT
mkdir -p "$STATE/config"
cp -R "$HERE/config/." "$STATE/config/"
if [[ -n "$OVERLAY" ]]; then cp -R "$OVERLAY/config/." "$STATE/config/"; fi
# Automation D uses the built-in motion_light blueprint. HA copies built-ins
# into /config/blueprints only after automations first load, so seed it from
# the pinned image itself (version-matched, no network).
mkdir -p "$STATE/config/blueprints/automation/homeassistant"
"${COMPOSE[@]}" run --rm --no-deps -T --entrypoint cat homeassistant \
  /usr/src/homeassistant/homeassistant/components/automation/blueprints/motion_light.yaml \
  > "$STATE/config/blueprints/automation/homeassistant/motion_light.yaml"

RELAY_VERSION="$(grep -E '^version:' "$ROOT/nova/config.yaml" | head -1 | sed -E 's/^version:[[:space:]]*"?([^"]+)"?/\1/')"
RELAY_AUTH_TOKEN="testbed-relay-$(node -e 'console.log(require("node:crypto").randomBytes(16).toString("hex"))')"
[[ -n "$RELAY_VERSION" && -n "$RELAY_AUTH_TOKEN" ]] || die "could not derive the relay version or token"
export RELAY_VERSION RELAY_AUTH_TOKEN HA_LLAT=""

step "starting Home Assistant + MQTT broker"
"${COMPOSE[@]}" up -d homeassistant mosquitto >&2

step "publishing retained MQTT discovery (7 fake devices)"
pub() { docker exec ha-nova-testbed-mqtt mosquitto_pub -h 127.0.0.1 -r -t "$1" -m "$2"; }
for i in $(seq 1 30); do
  docker exec ha-nova-testbed-mqtt mosquitto_pub -h 127.0.0.1 -t testbed/ping -m up 2> /dev/null && break
  [[ "$i" == 30 ]] && die "MQTT broker did not start"
  sleep 1
done
dev() { printf '{"identifiers":["%s"],"name":"%s","manufacturer":"NOVA Testbed","model":"%s"}' "$1" "$2" "$3"; }
LOCK_DEV="$(dev testbed_entrance_lock 'Entrance Door' 'Fake Lock')"
GARAGE_DEV="$(dev testbed_garage_opener 'Garage Opener' 'Fake Opener')"
PLUG_DEV="$(dev testbed_smart_plug 'Smart Plug' 'Fake Plug')"
CLIMATE_DEV="$(dev testbed_climate 'Climate Sensor' 'Fake Thermometer')"
ROUTER_DEV="$(dev testbed_router 'Router' 'Fake Router')"
GATE_DEV="$(dev testbed_driveway_gate 'Driveway Gate' 'Fake Gate Opener')"
pub homeassistant/lock/testbed_entrance_lock/config \
  "{\"name\":\"Lock\",\"unique_id\":\"testbed_entrance_lock\",\"default_entity_id\":\"lock.entrance_door\",\"command_topic\":\"testbed/entrance_lock/set\",\"state_topic\":\"testbed/entrance_lock/state\",\"device\":$LOCK_DEV}"
pub homeassistant/cover/testbed_garage/config \
  "{\"name\":\"Garage\",\"unique_id\":\"testbed_garage_cover\",\"default_entity_id\":\"cover.testbed_garage\",\"device_class\":\"garage\",\"command_topic\":\"testbed/garage/set\",\"state_topic\":\"testbed/garage/state\",\"device\":$GARAGE_DEV}"
pub homeassistant/switch/testbed_smart_plug/config \
  "{\"name\":\"Plug\",\"unique_id\":\"testbed_smart_plug_switch\",\"default_entity_id\":\"switch.smart_plug\",\"command_topic\":\"testbed/plug/set\",\"state_topic\":\"testbed/plug/state\",\"device\":$PLUG_DEV}"
pub homeassistant/button/testbed_plug_factory_reset/config \
  "{\"name\":\"Plug factory reset\",\"unique_id\":\"testbed_plug_factory_reset\",\"default_entity_id\":\"button.plug_factory_reset\",\"command_topic\":\"testbed/plug/factory_reset\",\"device\":$PLUG_DEV}"
pub homeassistant/sensor/testbed_temperature/config \
  "{\"name\":\"Temperature\",\"unique_id\":\"testbed_temperature\",\"default_entity_id\":\"sensor.testbed_temperature\",\"device_class\":\"temperature\",\"unit_of_measurement\":\"°C\",\"state_topic\":\"testbed/climate/temperature\",\"device\":$CLIMATE_DEV}"
pub homeassistant/button/testbed_router_restart/config \
  "{\"name\":\"Router restart\",\"unique_id\":\"testbed_router_restart\",\"default_entity_id\":\"button.router_restart\",\"device_class\":\"restart\",\"command_topic\":\"testbed/router/restart\",\"device\":$ROUTER_DEV}"
# name null: the entity takes the device name ("Driveway Gate").
pub homeassistant/cover/testbed_driveway_gate/config \
  "{\"name\":null,\"unique_id\":\"testbed_driveway_gate\",\"default_entity_id\":\"cover.driveway_gate\",\"device_class\":\"gate\",\"command_topic\":\"testbed/gate/set\",\"state_topic\":\"testbed/gate/state\",\"device\":$GATE_DEV}"
pub testbed/entrance_lock/state LOCKED
pub testbed/garage/state closed
pub testbed/gate/state closed
pub testbed/plug/state OFF
pub testbed/climate/temperature 21.5

step "waiting for Home Assistant on $HA_URL"
for i in $(seq 1 120); do
  curl -sf "$HA_URL/manifest.json" > /dev/null 2>&1 && break
  [[ "$i" == 120 ]] && { "${COMPOSE[@]}" logs homeassistant | tail -40 >&2; die "Home Assistant did not start"; }
  sleep 2
done

# ---- onboarding + token (same API path as scripts/e2e/disposable-ha/run.sh) ---
step "onboarding the owner user"
json() { node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s)[process.argv[1]]))' "$1"; }
auth_code="$(curl -sf -X POST "$HA_URL/api/onboarding/users" -H 'Content-Type: application/json' \
  -d '{"client_id":"http://127.0.0.1:18123/","name":"Testbed Owner","username":"testbed","password":"'"$OWNER_PASSWORD"'","language":"en"}' | json auth_code)"
access_token="$(curl -sf -X POST "$HA_URL/auth/token" \
  -d "client_id=http://127.0.0.1:18123/&grant_type=authorization_code&code=${auth_code}" | json access_token)"
for onboarding_step in core_config analytics integration; do
  curl -sf -X POST "$HA_URL/api/onboarding/$onboarding_step" \
    -H "Authorization: Bearer ${access_token}" -H 'Content-Type: application/json' \
    -d '{"client_id":"http://127.0.0.1:18123/","redirect_uri":"http://127.0.0.1:18123/"}' > /dev/null
done
curl -sf "$HA_URL/api/onboarding" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>process.exit(JSON.parse(s).every(x=>x.done)?0:1))' \
  || die "onboarding incomplete"

HA_LLAT="$(node "$HERE/seed.mjs" mint "$HA_URL" "$access_token")"
export HA_LLAT
umask 077
printf '%s\n' "$HA_LLAT" > "$STATE/ha-llat"
printf '%s\n' "$RELAY_AUTH_TOKEN" > "$STATE/relay-token"

if [[ -n "$OVERLAY" ]]; then
  # First thing with an admin token: the demo's own users and http checks
  # before anything else touches the house.
  step "hosted demo: overlay setup (demo.mjs setup)"
  node "$OVERLAY/demo.mjs" setup "$HA_URL" "$HA_LLAT"
fi

step "seeding the house (MQTT entry, floors, areas, labels, registry)"
node "$HERE/seed.mjs" house "$HA_URL" "$HA_LLAT" > "$STATE/registry.json"

if [[ -n "$OVERLAY" ]]; then
  node "$HERE/seed.mjs" summary "$HA_URL" "$HA_LLAT"
  echo "testbed: overlay boot done (Home Assistant + broker; no relay, no CLI)" >&2
  exit 0
fi

# ---- relay ----------------------------------------------------------------------
step "building + starting the relay ($RELAY_VERSION)"
"${COMPOSE[@]}" up -d --build relay >&2
for i in $(seq 1 60); do
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $RELAY_AUTH_TOKEN" "$RELAY_URL/health" || true)"
  [[ "$code" == 200 ]] && break
  [[ "$i" == 60 ]] && { "${COMPOSE[@]}" logs relay | tail -40 >&2; die "relay not healthy (last HTTP ${code:-none})"; }
  sleep 2
done

# ---- CLI home (temp HOME inside .state; never the real ~/.config/ha-nova) ------
step "preparing the isolated CLI home"
bash "$HERE/cli.sh" version >&2

node "$HERE/seed.mjs" summary "$HA_URL" "$HA_LLAT" > "$STATE/summary.json"
node -e '
const s = require(process.argv[1]); const r = require(process.argv[2]);
console.log(JSON.stringify({ ...s, area_ids: r.areas, relay_url: process.argv[3],
  relay_token_file: process.argv[4], ha_llat_file: process.argv[5], seconds: +process.argv[6] }, null, 2));' \
  "$STATE/summary.json" "$STATE/registry.json" "$RELAY_URL" "$STATE/relay-token" "$STATE/ha-llat" "$(($(date +%s) - start_ts))"

cat >&2 << EOF

Test bed is up. Use the CLI against it (isolated HOME in .state/home):
  npm run testbed:cli -- find "Leselampe"
  bash scripts/e2e/testbed/cli.sh plan call light.turn_on light.living_room_rgbww_lights
Shell env for your own commands (agent scenarios):
  eval "\$(bash scripts/e2e/testbed/cli.sh --env)"
Assertions: npm run testbed:verify     Tear down: npm run testbed:down
EOF
