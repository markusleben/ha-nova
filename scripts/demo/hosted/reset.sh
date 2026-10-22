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
# lock -> hide the house (drop /srv/ready) -> keep the chat-app sign-ins ->
# down -> restore the clone -> seed the chat-app entry -> up -> restore the
# sign-ins -> relay (stable public token, fresh HA token) + chat bridge ->
# egress, DNS, listener, relay, chat gates -> dashboard -> connect.json +
# /srv/ready -> public gate.
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
  # Access-log backstop (privacy page: no entry older than 6 days). Caddy
  # rolls and deletes only while it runs; here any file whose first (oldest)
  # entry is over 5 days old goes (a day of slack for late or skipped
  # resets), whatever Caddy missed. Unreadable first entry (corrupt, cut
  # off): the file's mtime instead. The open access.log is emptied, not
  # unlinked, so Caddy's next writes stay readable.
  local f ts
  for f in "$STATE"/logs/access*; do
    [[ -f "$f" && -s "$f" ]] || continue
    ts="$( (zcat -f -- "$f" 2> /dev/null || true) | head -n 1 | grep -o '"ts":[0-9]*' | head -n 1 | cut -d: -f2 || true)"
    [[ -n "$ts" ]] || ts="$(stat -c %Y -- "$f")"
    if ((ts < $(date +%s) - 5 * 86400)); then
      if [[ "$f" == */access.log ]]; then : > "$f"; else rm -f -- "$f"; fi
    fi
  done

  TESTBED_HA_IMAGE="${candidate:-$(cat "$STATE/ha-image" 2> /dev/null || true)}"
  [[ "$TESTBED_HA_IMAGE" =~ ^ghcr\.io/home-assistant/home-assistant@sha256:[0-9a-f]{64}$ ]] \
    || gate_failed "no verified Home Assistant digest"
  DEMO_RELAY_IMAGE="$(relay_image "$DEMO_REF")"
  docker image inspect "$DEMO_RELAY_IMAGE" > /dev/null 2>&1 \
    || gate_failed "relay image $DEMO_RELAY_IMAGE missing; re-run bootstrap.sh"
  DEMO_BRIDGE_IMAGE="$(bridge_image "$DEMO_REF")"
  docker image inspect "$DEMO_BRIDGE_IMAGE" > /dev/null 2>&1 \
    || gate_failed "chat bridge image $DEMO_BRIDGE_IMAGE missing; re-run bootstrap.sh"
  DEMO_EXTERNAL_URL="https://$DEMO_DOMAIN"
  # down.sh wipes .state with this image: in use by Caddy, so never pruned.
  export TESTBED_OVERLAY="$HOSTED" TESTBED_HA_IMAGE DEMO_RELAY_IMAGE DEMO_BRIDGE_IMAGE DEMO_EXTERNAL_URL DEMO_DOMAIN ABUSE_CONTACT TESTBED_WIPE_IMAGE="$CADDY_IMAGE"

  # Chat-app sign-ins (Claude, ChatGPT) outlive the wipe: HA flushes its auth
  # store on stop, chat.mjs keeps the demo user's used in the last 24 h
  # (spec decision 2). Never a reason to stop: the worst case is today's
  # reconnect.
  timeout 90 docker stop -t 60 ha-nova-testbed-ha > /dev/null 2>&1 || true
  timeout 60 node "$HOSTED/chat.mjs" keep "$TB/.state" "$KEPT" \
    || logger -p user.warning -t ha-nova-demo-reset "chat-app sign-ins not kept"
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

  # Add to Claude: the ha_nova entry (bridge address, a fresh secret) is in
  # Home Assistant's store before its first boot, as the NOVA App's discovery
  # would leave it; up.sh copies config/ over (git clean removes it again).
  local bridge_secret
  bridge_secret="$(openssl rand -hex 32)"
  mkdir -p "$HOSTED/config/.storage"
  (umask 077 && node "$HOSTED/chat.mjs" entry "$bridge_secret" > "$HOSTED/config/.storage/core.config_entries")

  # Fresh owner password per reset; the owner is local-only (demo.mjs setup)
  # and only host tooling uses it, through its long-lived token.
  local owner_password owner_llat i
  owner_password="$(openssl rand -hex 24)"
  (umask 077 && printf '%s\n' "$owner_password" > /root/ha-nova-demo-owner-password)
  TESTBED_OWNER_PASSWORD="$owner_password" timeout 900 bash "$TB/up.sh"
  owner_llat="$(cat "$TB/.state/ha-llat")"
  # The kept sign-ins go into the new auth store while HA is stopped, bound
  # to this build's demo user. Only when there are any: a restart costs time.
  if [[ -s "$KEPT" ]]; then
    local kept=0
    timeout 90 docker stop -t 60 ha-nova-testbed-ha > /dev/null || gate_failed "could not stop Home Assistant to restore chat-app sign-ins"
    kept="$(timeout 60 node "$HOSTED/chat.mjs" restore "$KEPT" "$TB/.state")" \
      || logger -p user.warning -t ha-nova-demo-reset "chat-app sign-ins not restored"
    docker start ha-nova-testbed-ha > /dev/null || gate_failed "could not start Home Assistant after restoring chat-app sign-ins"
    for i in $(seq 1 90); do
      curl -sf -m 5 -o /dev/null -H "Authorization: Bearer $owner_llat" "$HA/api/" && break
      [[ "$i" == 90 ]] && gate_failed "Home Assistant did not come back after restoring ${kept:-0} chat-app sign-in(s)"
      sleep 2
    done
    echo "reset.sh: ${kept:-0} chat-app sign-in(s) restored" >&2
  fi

  # The public relay token is public anyway (connect.json): generated once
  # and kept, so agents survive the hourly rebuild. Never in the repo.
  local token_file="$STATE/relay-token" relay_token
  [[ -s "$token_file" ]] || (umask 077 && openssl rand -hex 32 > "$token_file")
  relay_token="$(cat "$token_file")"
  # The chat bridge's files: only the demo user of this build may use it.
  node "$HOSTED/chat.mjs" files "$TB/.state/bridge" "$bridge_secret" "$relay_token" "$DEMO_EXTERNAL_URL" \
    || gate_failed "chat bridge files"
  chown -R 1000:1000 "$TB/.state/bridge"
  RELAY_AUTH_TOKEN="$relay_token" HA_LLAT="$(timeout 120 node "$HOSTED/demo.mjs" token "$HA" "$owner_llat")" \
    timeout 300 docker compose -f "$TB/docker-compose.yml" -f "$HOSTED/docker-compose.yml" up -d relay bridge >&2

  for i in $(seq 1 30); do
    relay_roundtrip "$HA/relay" "$relay_token" 2> /dev/null && break
    [[ "$i" == 30 ]] && gate_failed "relay round trip: $(relay_roundtrip "$HA/relay" "$relay_token" 2>&1)"
    sleep 2
  done
  for i in $(seq 1 10); do
    timeout 45 node "$HOSTED/chat.mjs" gate "$HA" "$owner_llat" "$DEMO_EXTERNAL_URL" 2> "$STATE/gate.err" && break
    [[ "$i" == 10 ]] && gate_failed "chat gate: $(cat "$STATE/gate.err")"
    sleep 2
  done
  egress_gate 2> "$STATE/gate.err" || gate_failed "$(cat "$STATE/gate.err")"
  listener_gate 2> "$STATE/gate.err" || gate_failed "$(cat "$STATE/gate.err")"

  timeout 120 node "$HOSTED/demo.mjs" dashboard "$HA" "$owner_llat" 2> "$STATE/gate.err" \
    || gate_failed "demo.mjs dashboard: $(cat "$STATE/gate.err")"
  # A warning (the simulated devices did not answer) is no reason to stay down.
  if [[ -s "$STATE/gate.err" ]]; then logger -p user.warning -t ha-nova-demo-reset "$(cat "$STATE/gate.err")"; fi
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
