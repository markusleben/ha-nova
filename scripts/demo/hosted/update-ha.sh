#!/usr/bin/env bash
# Keep the hosted demo on the latest stable Home Assistant, by verified digest
# only. Runs as root on the demo host: nightly (ha-nova-demo-update.timer,
# 03:10 UTC) and once from bootstrap.sh.
#
#   update-ha.sh              latest stable: resolve, verify, candidate boot, gates, promote
#   update-ha.sh --pin <ver>  verify <ver> and make it the good digest WITHOUT a candidate
#                             boot (bootstrap: the test bed's tested pin; manual rollback,
#                             the only way to go to an older version)
#
# Verified before anything runs: the version string, the digest of
# ghcr.io/home-assistant/home-assistant:<ver>, and its Sigstore signature,
# keyless, made by Home Assistant's own release workflow for exactly that tag
# (identity + issuer pinned, transparency log checked; never skipped). On any
# failure the last good digest stays in place and the reason is logged loudly.
set -euo pipefail

# shellcheck source=host-lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/host-lib.sh"
IMAGE=ghcr.io/home-assistant/home-assistant
ISSUER=https://token.actions.githubusercontent.com

fail() {
  logger -p user.err -t ha-nova-demo-update "$*"
  echo "update-ha: FAILED: $*" >&2
  exit 1
}
log() { echo "update-ha: $*" >&2; }

# verify <version> -> prints the verified ghcr.io/...@sha256 reference
verify() {
  local version="$1" digest
  [[ "$version" =~ ^20[0-9]{2}\.[0-9]{1,2}\.[0-9]+$ ]] || fail "not a stable version: '$version'"
  digest="$(docker buildx imagetools inspect "$IMAGE:$version" --format '{{json .Manifest.Digest}}' | tr -d '"')"
  [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "could not resolve $IMAGE:$version"
  cosign verify "$IMAGE@$digest" \
    --certificate-identity "https://github.com/home-assistant/core/.github/workflows/builder.yml@refs/tags/$version" \
    --certificate-oidc-issuer "$ISSUER" > /dev/null \
    || fail "signature check failed for $IMAGE:$version ($digest)"
  echo "$IMAGE@$digest"
}

persist() {
  (umask 077 && printf '%s\n' "$1" > "$STATE/ha-image.tmp" && printf '%s\n' "$2" > "$STATE/ha-version.tmp")
  mv "$STATE/ha-image.tmp" "$STATE/ha-image"
  mv "$STATE/ha-version.tmp" "$STATE/ha-version"
}

# Old images would fill the disk over months (and the guard resets at 80 %).
# Prune only while Home Assistant runs the good digest: an image in use is
# never pruned, and the relay image carries the keep label.
prune() {
  local good running
  good="$(cat "$STATE/ha-image" 2> /dev/null || true)"
  running="$(docker inspect ha-nova-testbed-ha --format '{{.Config.Image}}' 2> /dev/null || true)"
  if [[ -n "$good" && "$running" == "$good" ]]; then
    docker image prune -af --filter until=48h --filter "label!=$KEEP_LABEL" > /dev/null || true
  else
    log "skipping image prune: Home Assistant is not running the good digest"
  fi
}

# gates <ref> <version>: the candidate runs, the house passes smoke (every
# guard invariant included), the relay round-trips, the public site works.
gates() {
  local ref="$1" version="$2" running got
  running="$(docker inspect ha-nova-testbed-ha --format '{{.Config.Image}}' 2> /dev/null || true)"
  [[ "$running" == "$ref" ]] || { log "running image $running, expected $ref"; return 1; }
  got="$(timeout 120 node "$HOSTED/demo.mjs" smoke "$HA" "$(cat "$TB/.state/ha-llat")")" || return 1
  [[ "$got" == "$version" ]] || { log "smoke reports $got, expected $version"; return 1; }
  relay_roundtrip "$HA/relay" "$(cat "$STATE/relay-token")" || return 1
  public_gate "$DEMO_DOMAIN" "$(cat "$STATE/relay-token")"
}

[[ "$(id -u)" == 0 ]] || fail "run as root"
# shellcheck source=/dev/null
. /etc/ha-nova-demo.env
if [[ "${1:-}" == --pin ]]; then
  ref="$(verify "${2:-}")"
  persist "$ref" "$2"
  log "pinned Home Assistant $2 ($ref)"
  exit 0
fi

version="$(curl -fsS -m 30 https://version.home-assistant.io/stable.json \
  | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).homeassistant.default))')" \
  || fail "could not read version.home-assistant.io/stable.json"
ref="$(verify "$version")"
current="$(cat "$STATE/ha-image" 2> /dev/null || true)"
if [[ "$ref" == "$current" ]]; then
  log "already on Home Assistant $version"
  prune
  exit 0
fi
current_version="$(cat "$STATE/ha-version" 2> /dev/null || true)"
if [[ -n "$current_version" && "$version" != "$current_version" \
  && "$(printf '%s\n%s\n' "$version" "$current_version" | sort -V | head -1)" == "$version" ]]; then
  fail "stable.json offers $version, older than the running $current_version; refusing a downgrade (use --pin)"
fi

# The candidate goes to reset.sh as an argument: a timer reset waiting on the
# lock can never pick it up, and Caddy serves the reset page meanwhile.
log "candidate Home Assistant $version ($ref)"
if bash "$HOSTED/reset.sh" --candidate "$ref" && gates "$ref" "$version"; then
  persist "$ref" "$version"
  log "promoted Home Assistant $version ($ref)"
  [[ -z "$current" ]] || docker image rm "$current" > /dev/null 2>&1 || true
  prune
  exit 0
fi

logger -p user.err -t ha-nova-demo-update "candidate Home Assistant $version failed; rolling back to $current"
bash "$HOSTED/reset.sh" || true
docker image rm "$ref" > /dev/null 2>&1 || true
prune
fail "candidate Home Assistant $version ($ref) failed its boot or gates; kept $current"
