#!/usr/bin/env bash
# Every minute on the demo host (ha-nova-demo-guard.timer). Every visitor is
# an admin, so anything can be broken; this notices and rebuilds. Cheap
# repairs (the public password, failed-login notices with visitor IPs) happen
# in demo.mjs guard; everything else triggers a full reset.sh run.
set -euo pipefail

TB=/opt/ha-nova/scripts/e2e/testbed
HOSTED=/opt/ha-nova/scripts/demo/hosted
HA=http://127.0.0.1:18123

reset() {
  echo "guard: $1; starting a full reset" >&2
  systemctl start --no-block ha-nova-demo-reset.service
  exit 0
}

# A reset in progress owns the house.
[[ "$(systemctl show -p ActiveState --value ha-nova-demo-reset.service)" == activating ]] && exit 0

disk="$(df --output=pcent / | tail -1 | tr -dc 0-9)"
((disk <= 80)) || reset "disk at ${disk}%"
swap="$(free | awk '/^Swap:/ { print ($2 > 0) ? int($3 * 100 / $2) : 0 }')"
((swap <= 80)) || reset "swap at ${swap}%"

[[ -s "$TB/.state/ha-llat" && -s /var/lib/ha-nova-demo/public/connect.json ]] || reset "no demo state"
node "$HOSTED/demo.mjs" guard "$HA" "$(cat "$TB/.state/ha-llat")" || reset "house check failed"
relay_token="$(node -e 'console.log(require(process.argv[1]).relay_token)' /var/lib/ha-nova-demo/public/connect.json)"
curl -sf -o /dev/null -m 10 -H "Authorization: Bearer $relay_token" "$HA/relay/health" || reset "relay unhealthy"
