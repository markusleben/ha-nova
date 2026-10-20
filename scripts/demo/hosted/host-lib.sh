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
# passes its candidate straight to reset.sh (nothing to steal) and holds it
# for its own gates.
LOCK=/run/ha-nova-demo.lock
# Set while the public gate was skipped (nothing served over TLS); guard.sh
# starts a reset, and with it the gate, as soon as TLS is served.
PENDING="$STATE/public-gate-pending"
# The relay and chat-bridge images: built once per ref by bootstrap.sh from
# digest-pinned bases (Go only for the bridge's build stage), never pulled or
# rebuilt by a reset, labelled so prunes keep them.
NODE_IMAGE=node:22.23.3-alpine3.24@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402
GO_IMAGE=golang:1.26.5@sha256:705e964a93a2fd2e75c7d59bb7d781b57e30f12293ffde5175c69229e18fb678
CADDY_IMAGE=caddy:2.11.4@sha256:0c994536bddb66445885237f1a5dcc1916bccea922661c76b4e9fc24061f9b52
KEEP_LABEL=ha-nova-demo.keep=1

relay_image() { echo "ha-nova-demo/relay:${1:0:12}"; }   # <ref>
bridge_image() { echo "ha-nova-demo/bridge:${1:0:12}"; } # <ref>

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

# egress_gate: Home Assistant, the relay, and the chat bridge can neither resolve external
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
  probe docker exec ha-nova-testbed-relay node -e 'fetch("http://1.1.1.1").then(()=>process.exit(0),()=>process.exit(1))' & pids+=($!); i=$((i + 1))
  probe docker exec ha-nova-testbed-bridge node -e 'require("dns").lookup("example.com",e=>process.exit(e?1:0))' & pids+=($!); i=$((i + 1))
  probe docker exec ha-nova-testbed-bridge node -e 'fetch("http://1.1.1.1").then(()=>process.exit(0),()=>process.exit(1))' & pids+=($!)
  wait "${pids[@]}" || true
  if compgen -G "$dir/escaped.*" > /dev/null; then
    echo "egress gate: got out with: $(cat "$dir"/escaped.*)" >&2
    failed=1
  fi
  rm -rf "$dir"
  # Positive controls: the same tools must work on house targets, or a
  # missing tool or container would read as "blocked".
  if ! timeout 4 docker exec ha-nova-testbed-ha getent hosts mosquitto > /dev/null; then
    echo "egress gate: service names no longer resolve on the house network" >&2
    failed=1
  fi
  if ! timeout 4 docker exec ha-nova-testbed-ha curl -sf -m 3 -o /dev/null http://127.0.0.1:8123/manifest.json; then
    echo "egress gate: the curl probe does not work inside Home Assistant" >&2
    failed=1
  fi
  local c
  for c in relay bridge; do
    if ! timeout 4 docker exec "ha-nova-testbed-$c" node -e 'fetch("http://homeassistant:8123/manifest.json").then(r=>process.exit(r.ok?0:1),()=>process.exit(1))'; then
      echo "egress gate: the fetch/DNS probe does not work inside the $c" >&2
      failed=1
    fi
  done
  return "$failed"
}

# listener_gate: nothing beyond loopback except 22, 80, 443 (TCP).
listener_gate() {
  local bad
  bad="$(ss -H -ltn | awk '{print $4}' | grep -v -E '^(127\.|\[::1\]|\[::ffff:127\.)' | grep -v -E ':(22|80|443)$' || true)"
  [[ -z "$bad" ]] || { echo "listener gate: unexpected TCP listeners: $bad" >&2; return 1; }
}

# tls_served <domain>: Caddy completes a TLS handshake for the name on this
# host and answers, whatever the certificate (expired, internal, untrusted).
tls_served() { curl -sk -m 15 --resolve "$1:443:127.0.0.1" -o /dev/null "https://$1/start"; }

# dns_points_here <domain>: the name resolves to this instance's public IPv4
# (IMDSv2; the hop limit of 1 keeps containers out, not the host).
dns_points_here() {
  local token ip
  token="$(curl -sf -m 3 -X PUT -H 'X-aws-ec2-metadata-token-ttl-seconds: 60' http://169.254.169.254/latest/api/token)" || return 1
  ip="$(curl -sf -m 3 -H "X-aws-ec2-metadata-token: $token" http://169.254.169.254/latest/meta-data/public-ipv4)" || return 1
  [[ -n "$ip" && " $(getent ahostsv4 "$1" | awk '{print $1}' | tr '\n' ' ') " == *" $ip "* ]]
}

# public_gate <domain> <relay_token>: the public site as a visitor sees it,
# through Caddy's TLS on this host (verified), blocked paths included.
# Returns 2, the gate skipped, only while nothing at all is served over TLS
# (no certificate yet: DNS not pointed here); noted on stdout (callers
# capture stderr for failures only), and $PENDING makes guard.sh start a
# reset as soon as TLS is served. Anything served goes through the gate.
public_gate() {
  local domain="$1" token="$2" url pats pat code
  if ! tls_served "$domain"; then
    echo "public gate: SKIPPED, nothing served over TLS for $domain yet (no certificate: A record to this host? docker logs ha-nova-demo-caddy)"
    touch "$PENDING"
    return 2
  fi
  rm -f "$PENDING"
  url=(--resolve "$domain:443:127.0.0.1" -sf -m 15)
  curl "${url[@]}" -o /dev/null "https://$domain/start" || { echo "public gate: https://$domain does not verify or answer" >&2; return 1; }
  # The landing page lives on hanova.app; HA's sign-in return (/ with a
  # query) must still reach HA.
  for pat in / /start; do
    code="$(curl "${url[@]}" -o /dev/null -w '%{http_code} %{redirect_url}' "https://$domain$pat" || true)"
    [[ "$code" == "302 https://hanova.app/demo/" ]] || { echo "public gate: $pat answered '$code', not the landing redirect" >&2; return 1; }
  done
  code="$(curl "${url[@]}" -o /dev/null -w '%{http_code} %{redirect_url}' "https://$domain/?auth_callback=1" || true)"
  [[ "$code" == "200 " ]] || { echo "public gate: /?auth_callback=1 answered '$code', not Home Assistant" >&2; return 1; }
  curl "${url[@]}" "https://$domain/connect.json" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>process.exit(JSON.parse(s).relay_token?0:1))' \
    || { echo "public gate: connect.json" >&2; return 1; }
  # grep -c reads to the end: grep -q could exit early and fail curl (pipefail).
  curl "${url[@]}" -o /dev/null -D - "https://$domain/connect.json" | tr -d '\r' | grep -ci '^access-control-allow-origin: https://hanova\.app$' > /dev/null \
    || { echo "public gate: connect.json lacks the hanova.app CORS header" >&2; return 1; }
  curl "${url[@]}" -o /dev/null "https://$domain/_demo/icon.svg" || { echo "public gate: restarting page star" >&2; return 1; }
  curl "${url[@]}" -o /dev/null -H "Authorization: Bearer $token" "https://$domain/relay/health" || { echo "public gate: relay" >&2; return 1; }
  curl "${url[@]}" -o /dev/null "https://$domain/auth/authorize?response_type=code&client_id=https://$domain/&redirect_uri=https://$domain/" \
    || { echo "public gate: login page" >&2; return 1; }
  # Add to Claude: the connector answers 401 pointing at this origin's
  # resource metadata, which names this origin as its sign-in, whose metadata
  # offers what Claude's published identity (CIMD) needs.
  curl --resolve "$domain:443:127.0.0.1" -s -m 15 -o /dev/null -D - -X POST "https://$domain/api/ha_nova/mcp" | tr -d '\r' \
    | grep -ci "^www-authenticate: Bearer resource_metadata=\"https://$domain/.well-known/oauth-protected-resource\"$" > /dev/null \
    || { echo "public gate: /api/ha_nova/mcp does not ask for sign-in with this origin's resource metadata" >&2; return 1; }
  { curl "${url[@]}" "https://$domain/.well-known/oauth-protected-resource" && echo && curl "${url[@]}" "https://$domain/.well-known/oauth-authorization-server"; } \
    | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const [r,a]=s.trim().split("\n").map(JSON.parse),o=process.argv[1];
        process.exit(r.resource===o&&r.authorization_servers?.[0]===o&&a.authorization_endpoint===o+"/auth/authorize"&&a.token_endpoint===o+"/auth/token"
          &&a.client_id_metadata_document_supported===true&&a.token_endpoint_auth_methods_supported?.includes("none")&&a.code_challenge_methods_supported?.includes("S256")?0:1)})' "https://$domain" \
    || { echo "public gate: OAuth metadata is not what Claude's sign-in needs" >&2; return 1; }
  # Every @blocked pattern of the Caddyfile, read from it (read -a: no globbing).
  read -ra pats <<< "$(sed -n 's/^[[:space:]]*@blocked path //p' "$HOSTED/Caddyfile")"
  ((${#pats[@]} > 0)) || { echo "public gate: no @blocked patterns in the Caddyfile" >&2; return 1; }
  for pat in "${pats[@]}"; do
    code="$(curl --resolve "$domain:443:127.0.0.1" -s -m 15 -o /dev/null -w '%{http_code}' "https://$domain${pat//\*/x}" || true)"
    [[ "$code" == 403 ]] || { echo "public gate: ${pat//\*/x} answered $code, not 403" >&2; return 1; }
  done
  echo "public gate: passed"
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
