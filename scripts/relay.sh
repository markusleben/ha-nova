#!/usr/bin/env bash
set -euo pipefail
# HA NOVA Relay CLI - thin curl wrapper
# Usage: ha-nova-relay <endpoint> [curl-args...]
#   ha-nova-relay ws -d '{"type":"get_states"}'
#   ha-nova-relay core -d '{"method":"GET","path":"/api/states"}'
#   ha-nova-relay health

source ~/.config/ha-nova/onboarding.env 2>/dev/null \
  || { echo "error: missing ~/.config/ha-nova/onboarding.env" >&2; exit 1; }

RELAY_AUTH_TOKEN="$(security find-generic-password \
  -a "$USER" -s "ha-nova.relay-auth-token" -w 2>/dev/null)" \
  || { echo "error: missing Keychain token ha-nova.relay-auth-token" >&2; exit 1; }

ENDPOINT="${1:?Usage: ha-nova-relay <endpoint> [curl-args...]}"
shift

exec curl -sS --connect-timeout 5 --max-time 15 \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/$ENDPOINT" "$@"
