#!/usr/bin/env bash
# Rebuild the hosted demo house from scratch. Runs as root on the demo host:
# every full hour (ha-nova-demo-reset.timer), on demand from guard.sh, and
# once from bootstrap.sh. Caddy (edge.yml) keeps serving the "resetting" page.
#
# down (containers, volumes, .state) -> restore the clone to the pinned ref ->
# up with this overlay -> relay with the demo user's token -> dashboard ->
# publish connect.json.
set -euo pipefail

# Everything runs inside main: the checkout may rewrite this very file, and
# bash reads scripts lazily. A function is parsed in full before it runs.
main() {
  [[ "$(id -u)" == 0 ]] || { echo "reset.sh: run as root" >&2; exit 1; }
  # DEMO_DOMAIN, DEMO_REF, ABUSE_CONTACT (written by bootstrap.sh)
  # shellcheck source=/dev/null
  . /etc/ha-nova-demo.env
  local repo=/opt/ha-nova
  local tb="$repo/scripts/e2e/testbed"
  local hosted="$repo/scripts/demo/hosted"
  local public=/var/lib/ha-nova-demo/public
  local ha=http://127.0.0.1:18123
  export TESTBED_OVERLAY="$hosted"

  RELAY_AUTH_TOKEN="" HA_LLAT="" bash "$tb/down.sh"
  git -C "$repo" checkout -q --force --detach "$DEMO_REF"
  git -C "$repo" clean -qffdx

  # Fresh owner password per reset; the owner is local-only (demo.mjs setup)
  # and only host tooling uses it, through its long-lived token.
  local owner_password
  owner_password="$(openssl rand -hex 24)"
  (umask 077 && printf '%s\n' "$owner_password" > /root/ha-nova-demo-owner-password)
  TESTBED_OWNER_PASSWORD="$owner_password" bash "$tb/up.sh"
  local owner_llat
  owner_llat="$(cat "$tb/.state/ha-llat")"

  # The public relay: a new token every reset, never in the repo.
  local relay_token
  relay_token="$(openssl rand -hex 32)"
  RELAY_AUTH_TOKEN="$relay_token" HA_LLAT="$(node "$hosted/demo.mjs" token "$ha" "$owner_llat")" \
    RELAY_VERSION="$(sed -nE 's/^version:[[:space:]]*"?([^"]+)"?/\1/p' "$repo/nova/config.yaml" | head -1)" \
    docker compose -f "$tb/docker-compose.yml" -f "$hosted/docker-compose.yml" up -d --build relay >&2
  local i
  for i in $(seq 1 60); do
    curl -sf -o /dev/null -H "Authorization: Bearer $relay_token" "$ha/relay/health" && break
    [[ "$i" == 60 ]] && { echo "reset.sh: relay not healthy" >&2; exit 1; }
    sleep 2
  done

  node "$hosted/demo.mjs" dashboard "$ha" "$owner_llat"

  local tmp="$public/.connect.json.tmp"
  printf '{"ha_url":"https://%s","relay_url":"https://%s/relay","relay_token":"%s","login":{"username":"demo","password":"demo"},"next_reset":"%s","abuse":"https://%s/abuse"}\n' \
    "$DEMO_DOMAIN" "$DEMO_DOMAIN" "$relay_token" "$(date -u -d '+1 hour' +%Y-%m-%dT%H:00:00Z)" "$DEMO_DOMAIN" > "$tmp"
  chmod 0644 "$tmp"
  mv "$tmp" "$public/connect.json"
  echo "reset.sh: demo rebuilt at $(git -C "$repo" rev-parse --short HEAD)" >&2
}

main "$@"
