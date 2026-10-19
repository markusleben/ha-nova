#!/usr/bin/env bash
# Rebuild the hosted demo house from scratch. Runs as root on the demo host:
# every full hour and 2 min after boot (ha-nova-demo-reset.timer), on demand
# from guard.sh, from update-ha.sh with a candidate image, and from
# bootstrap.sh. Caddy (edge.yml) serves the "restarting" page until the
# rebuilt house passed every gate.
#
#   reset.sh                        last verified good Home Assistant digest
#   reset.sh --candidate <ref>      a verified candidate (update-ha.sh only)
#
# lock -> hide the house (drop /srv/ready) -> down -> restore the clone ->
# up -> relay (stable public token, fresh HA token) -> egress, DNS, listener,
# relay gates -> dashboard -> connect.json + /srv/ready -> public gate.
set -euo pipefail

# Everything runs inside main: the checkout may rewrite this very file, and
# bash reads scripts lazily. A function is parsed in full before it runs.
main() {
  [[ "$(id -u)" == 0 ]] || { echo "reset.sh: run as root" >&2; exit 1; }
  # shellcheck source=host-lib.sh
  . "$(dirname "${BASH_SOURCE[0]}")/host-lib.sh"
  # DEMO_DOMAIN, DEMO_REF, ABUSE_CONTACT (written by bootstrap.sh)
  # shellcheck source=/dev/null
  . /etc/ha-nova-demo.env
  local candidate=""
  case "${1:-}" in
    --candidate) candidate="${2:-}" ;;
    "") ;;
    *) echo "reset.sh: unknown argument $1" >&2; exit 2 ;;
  esac

  exec 9> "$LOCK"
  flock 9
  # Hidden before anything can fail: no exit below may leave last hour's
  # house exposed (gate_failed also takes it down).
  rm -f "$PUBLIC/ready"
  # Access-log backstop (privacy policy: at most 7 days). Caddy rolls and
  # deletes only while it runs; here any file whose first (oldest) entry is
  # over 6 days old goes, whatever Caddy missed.
  local f ts
  for f in "$STATE"/logs/access*; do
    [[ -f "$f" ]] || continue
    ts="$( (zcat -f -- "$f" 2> /dev/null || true) | head -n 1 | grep -o '"ts":[0-9]*' | head -n 1 | cut -d: -f2 || true)"
    if [[ -n "$ts" ]] && ((ts < $(date +%s) - 6 * 86400)); then rm -f -- "$f"; fi
  done

  TESTBED_HA_IMAGE="${candidate:-$(cat "$STATE/ha-image" 2> /dev/null || true)}"
  [[ "$TESTBED_HA_IMAGE" =~ ^ghcr\.io/home-assistant/home-assistant@sha256:[0-9a-f]{64}$ ]] \
    || gate_failed "no verified Home Assistant digest"
  DEMO_RELAY_IMAGE="$(relay_image "$DEMO_REF")"
  docker image inspect "$DEMO_RELAY_IMAGE" > /dev/null 2>&1 \
    || gate_failed "relay image $DEMO_RELAY_IMAGE missing; re-run bootstrap.sh"
  # down.sh wipes .state with this image: in use by Caddy, so never pruned.
  export TESTBED_OVERLAY="$HOSTED" TESTBED_HA_IMAGE DEMO_RELAY_IMAGE DEMO_DOMAIN ABUSE_CONTACT TESTBED_WIPE_IMAGE="$CADDY_IMAGE"

  RELAY_AUTH_TOKEN="" HA_LLAT="" timeout 300 bash "$TB/down.sh"
  git -C "$REPO" checkout -q --force --detach "$DEMO_REF"
  git -C "$REPO" clean -qffdx
  edge_up
  # Nothing served over TLS but the name points here: Caddy retries ACME now
  # instead of after its backoff (up to 6 h; it stops after 30 days). Here
  # the house is hidden, and up.sh waits for HA through Caddy anyway.
  if ! tls_served "$DEMO_DOMAIN" && dns_points_here "$DEMO_DOMAIN"; then
    echo "reset.sh: $DEMO_DOMAIN points here, no certificate yet; restarting Caddy to retry ACME (docker logs ha-nova-demo-caddy)"
    docker restart ha-nova-demo-caddy > /dev/null
  fi

  # Fresh owner password per reset; the owner is local-only (demo.mjs setup)
  # and only host tooling uses it, through its long-lived token.
  local owner_password owner_llat
  owner_password="$(openssl rand -hex 24)"
  (umask 077 && printf '%s\n' "$owner_password" > /root/ha-nova-demo-owner-password)
  TESTBED_OWNER_PASSWORD="$owner_password" timeout 900 bash "$TB/up.sh"
  owner_llat="$(cat "$TB/.state/ha-llat")"

  # The public relay token is public anyway (connect.json): generated once
  # and kept, so agents survive the hourly rebuild. Never in the repo.
  local token_file="$STATE/relay-token" relay_token
  [[ -s "$token_file" ]] || (umask 077 && openssl rand -hex 32 > "$token_file")
  relay_token="$(cat "$token_file")"
  RELAY_AUTH_TOKEN="$relay_token" HA_LLAT="$(timeout 120 node "$HOSTED/demo.mjs" token "$HA" "$owner_llat")" \
    timeout 300 docker compose -f "$TB/docker-compose.yml" -f "$HOSTED/docker-compose.yml" up -d relay >&2

  local i
  for i in $(seq 1 30); do
    relay_roundtrip "$HA/relay" "$relay_token" 2> /dev/null && break
    [[ "$i" == 30 ]] && gate_failed "relay round trip: $(relay_roundtrip "$HA/relay" "$relay_token" 2>&1)"
    sleep 2
  done
  egress_gate 2> "$STATE/gate.err" || gate_failed "$(cat "$STATE/gate.err")"
  listener_gate 2> "$STATE/gate.err" || gate_failed "$(cat "$STATE/gate.err")"

  timeout 120 node "$HOSTED/demo.mjs" dashboard "$HA" "$owner_llat"
  local version tmp="$PUBLIC/.connect.json.tmp"
  version="$(curl -sf -m 10 -H "Authorization: Bearer $owner_llat" "$HA/api/config" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).version))')"
  printf '{"ha_url":"https://%s","ha_version":"%s","relay_url":"https://%s/relay","relay_token":"%s","login":{"username":"demo","password":"demo"},"next_reset":"%s","abuse":"https://%s/abuse"}\n' \
    "$DEMO_DOMAIN" "$version" "$DEMO_DOMAIN" "$relay_token" "$(date -u -d '+1 hour' +%Y-%m-%dT%H:00:00Z)" "$DEMO_DOMAIN" > "$tmp"
  chmod 0644 "$tmp"
  mv "$tmp" "$PUBLIC/connect.json"
  touch "$PUBLIC/ready"
  # 2 = skipped: nothing is served over TLS yet (no certificate).
  local rc=0
  public_gate "$DEMO_DOMAIN" "$relay_token" 2> "$STATE/gate.err" || rc=$?
  ((rc == 0 || rc == 2)) || gate_failed "$(cat "$STATE/gate.err")"
  echo "reset.sh: demo rebuilt at $(git -C "$REPO" rev-parse --short HEAD), Home Assistant $version ($TESTBED_HA_IMAGE)" >&2
}

# A failed gate takes the house down: Caddy keeps showing the reset page.
gate_failed() {
  rm -f "$PUBLIC/ready"
  logger -p user.err -t ha-nova-demo-reset "gate failed, house taken down: $1"
  echo "reset.sh: gate failed: $1" >&2
  RELAY_AUTH_TOKEN="" HA_LLAT="" timeout 300 bash "$TB/down.sh" || true
  exit 1
}

main "$@"
