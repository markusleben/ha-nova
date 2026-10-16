#!/usr/bin/env bash
# Every minute on the demo host (ha-nova-demo-guard.timer). Every visitor is
# an admin, so anything can be broken; this notices and rebuilds. Cheap
# repairs (the public password, notices, the banner dashboard) happen in
# demo.mjs guard; everything else triggers a full reset.sh run.
set -euo pipefail

# shellcheck source=host-lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/host-lib.sh"
# DEMO_DOMAIN (written by bootstrap.sh)
# shellcheck source=/dev/null
. /etc/ha-nova-demo.env
RELAY_STRIKES=/run/ha-nova-demo-relay-strikes

reset() {
  echo "guard: $1; starting a full reset" >&2
  logger -p user.warning -t ha-nova-demo-guard "$1; full reset"
  rm -f "$RELAY_STRIKES"
  systemctl start --no-block ha-nova-demo-reset.service
  exit 0
}

# A rebuild in progress (reset.sh holds the lock) owns the house.
exec 9> "$LOCK"
flock -n 9 || exit 0

disk="$(df --output=pcent / | tail -1 | tr -dc 0-9)"
((disk <= 80)) || reset "disk at ${disk}%"
swap="$(free | awk '/^Swap:/ { print ($2 > 0) ? int($3 * 100 / $2) : 0 }')"
((swap <= 80)) || reset "swap at ${swap}%"

[[ -s "$TB/.state/ha-llat" && -s "$STATE/relay-token" && -e "$PUBLIC/ready" ]] || reset "no demo state"
# The last reset skipped the public gate (nothing served over TLS); now
# something is: run the gate through a reset.
if [[ -e "$PENDING" ]] && tls_served "$DEMO_DOMAIN"; then reset "TLS for $DEMO_DOMAIN is up, public gate pending"; fi
timeout 30 node "$HOSTED/demo.mjs" guard "$HA" "$(cat "$TB/.state/ha-llat")" || reset "house check failed"

# The relay: a revoked HA token (auth) resets at once; anything else
# (network, never connected, a failed round trip) after 3 minutes in a row.
token="$(cat "$STATE/relay-token")"
if relay_roundtrip "$HA/relay" "$token" 2> /dev/null; then
  rm -f "$RELAY_STRIKES"
  exit 0
fi
reason="$(curl -sf -m 10 -H "Authorization: Bearer $token" "$HA/relay/health" \
  | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).data.ha_ws_disconnect_reason ?? ""))' || true)"
[[ "$reason" != auth ]] || reset "relay lost its Home Assistant token"
strikes=$(($(cat "$RELAY_STRIKES" 2> /dev/null || echo 0) + 1))
echo "$strikes" > "$RELAY_STRIKES"
((strikes < 3)) || reset "relay unhealthy for $strikes minutes (${reason:-no answer})"
