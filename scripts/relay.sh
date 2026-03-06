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

if [[ "$ENDPOINT" == "health" ]]; then
  # Fetch health first — capture to check success before adding notices
  health_json=$(curl -sS --connect-timeout 5 --max-time 15 \
    -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    "$RELAY_BASE_URL/health" "$@" 2>/dev/null) || true

  # If health failed, output nothing (callers check for empty stdout)
  [[ -z "$health_json" ]] && exit 1

  # Skill version check BEFORE JSON so it's visible even when output is collapsed
  if [[ -x "${HOME}/.config/ha-nova/version-check" ]]; then
    "${HOME}/.config/ha-nova/version-check" 2>/dev/null || true
  fi
  echo "$health_json"

  # Relay compat check: compare relay version against min_relay_version
  relay_v=$(echo "$health_json" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  local_vj=""
  for f in "$(git rev-parse --show-toplevel 2>/dev/null || echo __NONE__)/version.json" "${HOME}/.config/ha-nova/version.json"; do
    [[ -f "$f" ]] && local_vj="$f" && break
  done
  if [[ -n "$relay_v" && -n "$local_vj" ]]; then
    min_v=$(sed -n 's/.*"min_relay_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$local_vj" 2>/dev/null | head -1)
    if [[ -n "$min_v" ]]; then
      # semver compare: is relay_v < min_v?
      IFS='.' read -r ra1 ra2 ra3 <<< "$relay_v"
      IFS='.' read -r mn1 mn2 mn3 <<< "$min_v"
      relay_below=false
      for i in 1 2 3; do
        eval "a=\$ra$i; b=\$mn$i"
        a="${a:-0}"; b="${b:-0}"
        (( a < b )) && relay_below=true && break
        (( a > b )) && break
      done
      if $relay_below; then
        echo "⚠️ RELAY OUTDATED: v${relay_v} is below minimum v${min_v} — Inform the user: update the NOVA Relay App in Home Assistant."
      fi
    fi
  fi
  exit 0
fi

exec curl -sS --connect-timeout 5 --max-time 15 \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/$ENDPOINT" "$@"
