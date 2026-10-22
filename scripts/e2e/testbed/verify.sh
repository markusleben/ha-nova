#!/usr/bin/env bash
# Smoke assertions against a running test bed (npm run testbed:up first).
# Uses the CLI built from this checkout with the isolated HOME (cli.sh). Only
# read verbs and plans run here: nothing is applied, Home Assistant is not
# changed. Prints PASS/FAIL per assertion; exits non-zero on any FAIL.
#
#   bash scripts/e2e/testbed/verify.sh      (or: npm run testbed:verify)
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE="$HERE/.state"
CLI="$HERE/cli.sh"
[[ -f "$STATE/relay-token" ]] || { echo "testbed: not running; npm run testbed:up first" >&2; exit 1; }
start_ts=$(date +%s)
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
PASSED=0
FAILED=0

# expect <name> <exit> <js predicate over d (parsed stdout)> -- <cli args…>
expect() {
  local name="$1" want_exit="$2" predicate="$3"
  shift 4
  local out code
  out="$(bash "$CLI" "$@" 2> "$TMP/stderr")"
  code=$?
  if [[ "$code" == "$want_exit" ]] && node -e '
    const d = JSON.parse(process.argv[1]);
    process.exit(new Function("d", `return (${process.argv[2]});`)(d) ? 0 : 1);' "$out" "$predicate" 2> /dev/null; then
    echo "PASS  $name"
    PASSED=$((PASSED + 1))
  else
    echo "FAIL  $name (exit $code, want $want_exit)"
    echo "      ha-nova $*"
    echo "      stdout: ${out:0:600}"
    [[ -s "$TMP/stderr" ]] && echo "      stderr: $(head -c 400 "$TMP/stderr")"
    FAILED=$((FAILED + 1))
  fi
}
reasons='d.reasons.map(r => r.code)'

echo "--- resolution"
expect 'find "Leselampe" resolves light.bed_light by alias' 0 \
  'd.result.matches[0].entity_id === "light.bed_light" && d.result.matches[0].by === "alias"' -- find "Leselampe"
expect 'find "Haustür" resolves lock.entrance_door by alias' 0 \
  'd.result.matches[0].entity_id === "lock.entrance_door"' -- find "Haustür"
expect 'registry-disabled switch.ac is never a target' 5 \
  'd.error.code === "ENTITY_DISABLED"' -- plan call switch.turn_on switch.ac

echo "--- tiering (plan call)"
expect 'helper write reaching lock.unlock via automation A is typed' 3 \
  "d.tier === 'typed' && $reasons.includes('ACCESS_LOCK') && d.consumers.some(c => c.automation === 'automation.a_guest_mode_unlocks_entrance_door' && c.class === 'typed' && c.match === (d.deltas[0].before === 'off' ? 'will' : 'may'))" \
  -- plan call input_boolean.turn_on input_boolean.guest_mode
expect 'living-room light off is ordinary; consumer B (fires on "on") is not listed' 0 \
  "d.tier === 'ordinary' && !(d.consumers || []).some(c => c.automation === 'automation.b_living_room_light_follows')" \
  -- plan call light.turn_off light.living_room_rgbww_lights
expect 'MQTT garage cover open is typed (ACCESS_COVER)' 3 \
  "d.tier === 'typed' && $reasons.includes('ACCESS_COVER')" -- plan call cover.open_cover cover.testbed_garage
expect 'MQTT gate cover open is typed (ACCESS_COVER, device_class gate)' 3 \
  "d.tier === 'typed' && $reasons.includes('ACCESS_COVER') && d.say.includes('opens a gate')" -- plan call cover.open_cover cover.driveway_gate
expect 'device_class override (kitchen_window -> shutter) makes open ordinary' 0 \
  "d.tier === 'ordinary'" -- plan call cover.open_cover cover.kitchen_window
expect 'registry-less demo lock unlock is typed with a four-digit one-time code' 3 \
  "d.tier === 'typed' && $reasons.includes('ACCESS_LOCK') && $reasons.includes('UNREGISTERED_ACTUATOR') && /^[0-9]{4}$/.test(d.plan.code) && d.say.endsWith('\n\nTo go ahead, type this code:  \n**' + d.plan.code + '**')" -- plan call lock.unlock lock.front_door
expect '"Plug factory reset" press is typed (DESTRUCTIVE_BUTTON)' 3 \
  "d.tier === 'typed' && $reasons.includes('DESTRUCTIVE_BUTTON')" -- plan call button.press button.plug_factory_reset
expect 'restart-class button (demo, non-user-authored) press is preview' 3 \
  "d.tier === 'preview' && $reasons.includes('DISRUPTIVE')" -- plan call button.press button.modem_restart
expect 'MQTT "Router restart" press is typed (user-authored button outranks DISRUPTIVE)' 3 \
  "d.tier === 'typed' && $reasons.includes('DISRUPTIVE') && $reasons.includes('USER_AUTHORED_BUTTON')" \
  -- plan call button.press button.router_restart
expect 'command_line switch is typed (UNREAD_COMMAND)' 3 \
  "d.tier === 'typed' && $reasons.includes('UNREAD_COMMAND')" -- plan call switch.turn_on switch.garden_pump
expect 'light acted on by a package automation without id: its loaded config is read (ordinary, no YAML_NO_ID)' 0 \
  "d.tier === 'ordinary' && !(d.reasons || []).some(r => r.code === 'YAML_NO_ID')" -- plan call light.turn_on light.ceiling_lights
expect 'template light is typed (TEMPLATE_ACTION_UNREAD)' 3 \
  "d.tier === 'typed' && $reasons.includes('TEMPLATE_ACTION_UNREAD')" -- plan call light.turn_on light.template_desk_lamp
expect 'script with delayed garage open is typed via stored member' 3 \
  "d.tier === 'typed' && $reasons.includes('ACCESS_COVER') && $reasons.includes('STORED_MEMBERS')" \
  -- plan call script.turn_on script.open_garage_after_delay
expect 'scene with a group member containing a lock is typed' 3 \
  "d.tier === 'typed' && $reasons.includes('ACCESS_LOCK')" -- plan call scene.turn_on scene.door_scene
expect 'area target with one light (kuche) is ordinary like naming it' 0 \
  "d.tier === 'ordinary' && JSON.stringify(d.action.entity_ids) === '[\"light.kitchen_lights\"]'" \
  -- plan call light.turn_off --area kuche
expect 'generic homeassistant.* over the same area stays preview' 3 \
  "d.tier === 'preview' && $reasons.includes('EXPANDED_TARGET')" -- plan call homeassistant.turn_off --area kuche

echo "--- config writes (plan write; nothing is applied)"
cat > "$TMP/create.json" << 'EOF'
{"alias":"Testbed draft kitchen evening","triggers":[{"trigger":"time","at":"19:00:00"}],"actions":[{"action":"light.turn_on","target":{"entity_id":"light.kitchen_lights"}}],"mode":"single"}
EOF
cat > "$TMP/unlock.json" << 'EOF'
{"alias":"Testbed draft unlock","triggers":[{"trigger":"time","at":"19:00:00"}],"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.entrance_door"}}],"mode":"single"}
EOF
cat > "$TMP/package.json" << 'EOF'
{"alias":"Package hall light evening","triggers":[{"trigger":"sun","event":"sunset"}],"actions":[{"action":"light.turn_on","target":{"entity_id":"light.entrance_color_white_lights"}}],"mode":"single"}
EOF
cat > "$TMP/ordered.json" << 'EOF'
{"alias":"F Ordered variables","triggers":[{"trigger":"state","entity_id":"input_select.house_mode"}],"variables":{"base":30,"brightness_pct":"{{ base * 2 }}"},"actions":[{"action":"light.turn_on","target":{"entity_id":"light.ceiling_lights"},"data":{"brightness_pct":"{{ brightness_pct }}"}}],"mode":"single"}
EOF
expect 'plan write --create (ordinary draft) is preview' 3 \
  "d.tier === 'preview' && $reasons.includes('CONFIG_WRITE') && d.config.op === 'write'" \
  -- plan write --domain automation --create --file "$TMP/create.json"
expect 'plan write of a draft that unlocks a door is typed with a code' 3 \
  "d.tier === 'typed' && d.plan.confirm === 'code' && $reasons.includes('ACCESS_LOCK')" \
  -- plan write --domain automation --create --file "$TMP/unlock.json"
expect 'plan write update keeps ordered variables (base before brightness_pct)' 3 \
  "Object.keys(d.config.body.variables).join() === 'base,brightness_pct' && d.diff.length > 0" \
  -- plan write --domain automation --id testbed_f_ordered_variables --file "$TMP/ordered.json"
expect 'get --config on a package automation returns its loaded config (managed: yaml)' 0 \
  "d.result.managed === 'yaml' && d.result.config.alias === 'Package hall light evening'" -- get automation.package_hall_light_evening --config
expect 'plan write over a package automation is refused (CONFIG_NOT_UI_MANAGED)' 4 \
  "d.error.code === 'CONFIG_NOT_UI_MANAGED'" \
  -- plan write --domain automation --id package_hall_light_evening --file "$TMP/package.json"

echo "--- reads"
expect 'history of a light is readable' 0 "d.status === 'ok'" -- history light.kitchen_lights --since 1h

echo "--- webhook-read protection (MCP ha_read never reaches /api/webhook)"
mcp_read() {
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"testbed-verify","version":"1"}}}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"ha_read\",\"arguments\":{\"endpoint\":\"core\",\"path\":\"$1\"}}}" \
    | bash "$CLI" mcp 2> /dev/null | tail -1
}
for path in /api/webhook/testbed-kitchen-light /api/states/%2e%2e/webhook/testbed-kitchen-light \
  /api/states/x/../../webhook/testbed-kitchen-light /api/%77ebhook/testbed-kitchen-light; do
  if mcp_read "$path" | node -e 'let s="";process.stdin.on("data",c=>s+=c).on("end",()=>{
      const text = JSON.parse(s).result.content[0].text; process.exit(/"error"/.test(text) ? 0 : 1)})'; then
    echo "PASS  ha_read refuses $path"
    PASSED=$((PASSED + 1))
  else
    echo "FAIL  ha_read did not refuse $path"
    FAILED=$((FAILED + 1))
  fi
done
expect 'webhook automation E never fired (last_triggered null)' 0 \
  'd.result.state.attributes.last_triggered === null' -- get automation.e_webhook_turns_on_kitchen_light

echo "--- known gap (reported, not asserted)"
gap="$(bash "$CLI" plan call button.press button.gate_buzzer 2> /dev/null)"
echo "INFO  template button without unique_id whose press unlocks lock.kitchen_door -> tier: $(
  node -e 'const d=JSON.parse(process.argv[1]); console.log(d.tier, d.reasons.map(r=>r.code).join(","))' "$gap")"

echo
echo "$PASSED passed, $FAILED failed in $(($(date +%s) - start_ts))s"
[[ "$FAILED" == 0 ]]
