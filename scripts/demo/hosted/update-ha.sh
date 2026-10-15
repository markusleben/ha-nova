#!/usr/bin/env bash
# Keep the hosted demo on the latest stable Home Assistant, by verified digest
# only. Runs as root on the demo host: nightly (ha-nova-demo-update.timer,
# 03:10 UTC) and once from bootstrap.sh.
#
#   update-ha.sh              latest stable: resolve, verify, candidate boot, smoke, promote
#   update-ha.sh --pin <ver>  verify <ver> and make it the good digest WITHOUT a candidate
#                             boot (bootstrap: the test bed's tested pin)
#
# Verified before anything runs: the version string, the digest of
# ghcr.io/home-assistant/home-assistant:<ver>, and its Sigstore signature,
# keyless, made by Home Assistant's own release workflow for exactly that tag
# (identity + issuer pinned, transparency log checked; never skipped). On any
# failure the last good digest stays in place and the reason is logged loudly.
set -euo pipefail

REPO=/opt/ha-nova
HOSTED="$REPO/scripts/demo/hosted"
STATE=/var/lib/ha-nova-demo
IMAGE=ghcr.io/home-assistant/home-assistant
ISSUER=https://token.actions.githubusercontent.com
HA=http://127.0.0.1:18123

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

reset_wait() {
  # A reset already running would swallow our start request (systemd joins it).
  while [[ "$(systemctl show -p ActiveState --value ha-nova-demo-reset.service)" == activating ]]; do sleep 5; done
  systemctl start ha-nova-demo-reset.service
}

[[ "$(id -u)" == 0 ]] || fail "run as root"
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
  exit 0
fi

log "candidate Home Assistant $version ($ref); Caddy serves the reset page meanwhile"
install -d -m 0700 /run/ha-nova-demo
(umask 077 && printf '%s\n' "$ref" > /run/ha-nova-demo/ha-image.candidate)
ok=0
if reset_wait; then
  running="$(docker inspect ha-nova-testbed-ha --format '{{.Config.Image}}' 2> /dev/null || true)"
  if [[ "$running" == "$ref" ]] \
    && got="$(timeout 120 node "$HOSTED/demo.mjs" smoke "$HA" "$(cat "$REPO/scripts/e2e/testbed/.state/ha-llat")")" \
    && [[ "$got" == "$version" ]]; then
    ok=1
  fi
fi
if [[ "$ok" == 1 ]]; then
  persist "$ref" "$version"
  log "promoted Home Assistant $version ($ref)"
  exit 0
fi

rm -f /run/ha-nova-demo/ha-image.candidate
logger -p user.err -t ha-nova-demo-update "candidate Home Assistant $version failed; rolling back to $current"
reset_wait || true
fail "candidate Home Assistant $version ($ref) failed its boot or smoke test; kept $current"
