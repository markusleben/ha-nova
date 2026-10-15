# shellcheck shell=bash
# shellcheck disable=SC2034 # constants for the scripts that source this file
# Shared by reset.sh, guard.sh, update-ha.sh, bootstrap.sh on the demo host
# (sourced; bash on Ubuntu 24.04). Gates print why they fail and return 1.

REPO=/opt/ha-nova
TB="$REPO/scripts/e2e/testbed"
HOSTED="$REPO/scripts/demo/hosted"
STATE=/var/lib/ha-nova-demo
PUBLIC="$STATE/public"
HA=http://127.0.0.1:18123
# One lock for everything that rebuilds or inspects the house: reset.sh holds
# it for the whole rebuild, guard.sh skips while it is held, update-ha.sh
# passes its candidate straight to reset.sh (nothing to steal).
LOCK=/run/ha-nova-demo.lock
# The relay image: built once per ref by bootstrap.sh from a digest-pinned
# Node base, never pulled or rebuilt by a reset, labelled so prunes keep it.
NODE_IMAGE=node:22.23.3-alpine3.24@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402
CADDY_IMAGE=caddy:2.11.4@sha256:0c994536bddb66445885237f1a5dcc1916bccea922661c76b4e9fc24061f9b52
KEEP_LABEL=ha-nova-demo.keep=1

relay_image() { echo "ha-nova-demo/relay:${1:0:12}"; } # <ref>

# relay_roundtrip <base> <token>: /core GET /api/ and /ws get_states work, and
# /health then reports a live Home Assistant connection.
relay_roundtrip() {
  local base="$1" token="$2" auth core ws health
  auth=(-H "Authorization: Bearer $token" -H "content-type: application/json")
  core="$(curl -sf -m 15 "${auth[@]}" -d '{"method":"GET","path":"/api/"}' "$base/core")" \
    || { echo "relay /core failed" >&2; return 1; }
  ws="$(curl -sf -m 15 "${auth[@]}" -d '{"type":"get_states"}' "$base/ws")" \
    || { echo "relay /ws failed" >&2; return 1; }
  health="$(curl -sf -m 10 -H "Authorization: Bearer $token" "$base/health")" \
    || { echo "relay /health failed" >&2; return 1; }
  # shellcheck disable=SC2016 # JavaScript, not shell
  node -e '
    const [core, ws, health] = process.argv.slice(1).map((s) => JSON.parse(s));
    const problems = [];
    if (core.data?.status !== 200) problems.push(`/core status ${core.data?.status}`);
    if (!Array.isArray(ws.data) || ws.data.length < 100) problems.push("/ws get_states too short");
    if (health.data?.ha_ws_connected !== true) problems.push(`not connected (${health.data?.ha_ws_disconnect_reason})`);
    if (problems.length) { console.error(`relay: ${problems.join(", ")}`); process.exit(1); }' "$core" "$ws" "$health"
}

# egress_gate: Home Assistant and the relay can neither resolve external
# names (Linux dockerd forwards them from internal networks; the compose DNS
# sink must stop that) nor reach an external address; service names resolve.
# Probes run in parallel, 4 s each; a probe that times out has failed to get
# out, which is the pass.
egress_gate() {
  local dir pids=() i=0 failed=0
  dir="$(mktemp -d)"
  probe() { if timeout 4 "$@" > /dev/null 2>&1; then echo "$*" > "$dir/escaped.$i"; fi; }
  probe docker exec ha-nova-testbed-ha getent hosts example.com & pids+=($!); i=$((i + 1))
  probe docker exec ha-nova-testbed-ha curl -s -m 3 -o /dev/null http://1.1.1.1 & pids+=($!); i=$((i + 1))
  probe docker exec ha-nova-testbed-relay node -e 'require("dns").lookup("example.com",e=>process.exit(e?1:0))' & pids+=($!); i=$((i + 1))
  probe docker exec ha-nova-testbed-relay node -e 'fetch("http://1.1.1.1").then(()=>process.exit(0),()=>process.exit(1))' & pids+=($!)
  wait "${pids[@]}" || true
  if compgen -G "$dir/escaped.*" > /dev/null; then
    echo "egress gate: got out with: $(cat "$dir"/escaped.*)" >&2
    failed=1
  fi
  rm -rf "$dir"
  if ! timeout 4 docker exec ha-nova-testbed-ha getent hosts mosquitto > /dev/null; then
    echo "egress gate: service names no longer resolve on the house network" >&2
    failed=1
  fi
  return "$failed"
}

# listener_gate: nothing beyond loopback except 22, 80, 443 (TCP).
listener_gate() {
  local bad
  bad="$(ss -H -ltn | awk '{print $4}' | grep -v -E '^(127\.|\[::1\]|\[::ffff:127\.)' | grep -v -E ':(22|80|443)$' || true)"
  [[ -z "$bad" ]] || { echo "listener gate: unexpected TCP listeners: $bad" >&2; return 1; }
}

# public_gate <domain> <relay_token> [first-boot]: the public site as a
# visitor sees it, through Caddy's TLS on this host. On bootstrap's first run
# no certificate may exist yet (DNS not pointed): then it is skipped, loudly.
public_gate() {
  local domain="$1" token="$2" first="${3:-}" url
  url=(--resolve "$domain:443:127.0.0.1" -sf -m 15)
  if ! curl "${url[@]}" -o /dev/null "https://$domain/start"; then
    if [[ "$first" == first-boot ]]; then
      echo "public gate: SKIPPED, no TLS for $domain yet (point DNS here; the next reset checks)" >&2
      return 0
    fi
    echo "public gate: https://$domain unreachable" >&2
    return 1
  fi
  curl "${url[@]}" "https://$domain/" | grep -q "HA NOVA demo house" || { echo "public gate: landing page" >&2; return 1; }
  curl "${url[@]}" "https://$domain/connect.json" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>process.exit(JSON.parse(s).relay_token?0:1))' \
    || { echo "public gate: connect.json" >&2; return 1; }
  curl "${url[@]}" -o /dev/null -H "Authorization: Bearer $token" "https://$domain/relay/health" || { echo "public gate: relay" >&2; return 1; }
  curl "${url[@]}" -o /dev/null "https://$domain/auth/authorize?response_type=code&client_id=https://$domain/&redirect_uri=https://$domain/" \
    || { echo "public gate: login page" >&2; return 1; }
}

# edge_up [--force]: (re)create Caddy when its files changed (or forced),
# after validating the Caddyfile with the pinned image.
edge_up() {
  local hash
  hash="$(cat "$HOSTED/edge.yml" "$HOSTED/Caddyfile" "$HOSTED"/site/* | sha256sum | cut -d' ' -f1)"
  if [[ "${1:-}" != --force && "$hash" == "$(cat "$STATE/edge.hash" 2> /dev/null || true)" ]]; then return 0; fi
  docker run --rm -e "DEMO_DOMAIN=$DEMO_DOMAIN" -e "ABUSE_CONTACT=$ABUSE_CONTACT" \
    -v "$HOSTED/Caddyfile:/etc/caddy/Caddyfile:ro" "$CADDY_IMAGE" \
    caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile > /dev/null 2>&1 \
    || { echo "edge: Caddyfile does not validate" >&2; return 1; }
  DEMO_DOMAIN="$DEMO_DOMAIN" ABUSE_CONTACT="$ABUSE_CONTACT" \
    docker compose -f "$HOSTED/edge.yml" up -d --force-recreate >&2
  echo "$hash" > "$STATE/edge.hash"
}
