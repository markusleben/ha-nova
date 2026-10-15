#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-app-image.sh <app-version> [<inputs-fingerprint>]

Fails unless ghcr.io/markusleben/{arch}-ha-nova-relay:<app-version> exists and
is ANONYMOUSLY pullable for every arch in nova/build.yaml (what Supervisor
needs), with the right platform and the io.hass.* labels Supervisor reads.
With <inputs-fingerprint> (scripts/release/app-image-lib.sh app_image_inputs),
each image must also carry that exact build-input fingerprint.
EOF
}

fail() {
  echo "[verify-app-image] ERROR: $*" >&2
  exit 1
}

[[ "$#" -eq 1 || "$#" -eq 2 ]] || { usage; exit 2; }
version="$1"
inputs="${2:-}"
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
  || fail "App version must be canonical X.Y.Z"
[[ -z "$inputs" || "$inputs" =~ ^[0-9a-f]{40}$ ]] || fail "inputs fingerprint must be 40 hex characters"

# shellcheck source=app-image-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/app-image-lib.sh"
command -v curl >/dev/null || fail "curl is required"
command -v jq >/dev/null || fail "jq is required"

config_archs="$(awk '/^arch:/ { s = 1; next } /^[^[:space:]]/ { s = 0 } s && /^  - / { print $2 }' \
  "$APP_IMAGE_ROOT/nova/config.yaml" | sort)"
build_archs="$(app_build_from | awk '{ print $1 }' | sort)"
[[ -n "$build_archs" ]] || fail "nova/build.yaml has no build_from archs"
[[ "$config_archs" == "$build_archs" ]] \
  || fail "nova/config.yaml arch list ($(echo $config_archs)) != nova/build.yaml build_from ($(echo $build_archs))"
configured_image="$(app_config_image)"
[[ -z "$configured_image" || "$configured_image" == "$APP_IMAGE_TEMPLATE" ]] \
  || fail "nova/config.yaml image: is '${configured_image}', expected '${APP_IMAGE_TEMPLATE}'"

accept="application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json"

for arch in $build_archs; do
  ref="$(app_image_ref "$arch" "$version")"
  repo="${ref#ghcr.io/}"; repo="${repo%:*}"
  platform="$(app_arch_platform "$arch")"
  # Anonymous token on purpose: Supervisor pulls without credentials, so a
  # private package must fail here, not on every user's device.
  token="$(curl -fsS "https://ghcr.io/token?scope=repository:${repo}:pull" | jq -r '.token // empty')" \
    || fail "${ref}: missing or not public (anonymous token refused)"
  manifest="$(curl -fsS -H "Authorization: Bearer ${token}" -H "Accept: ${accept}" \
    "https://ghcr.io/v2/${repo}/manifests/${version}")" \
    || fail "${ref}: missing or not public (anonymous manifest fetch failed)"
  if jq -e '.manifests' >/dev/null <<<"$manifest"; then
    digest="$(jq -r --arg os "${platform%%/*}" --arg a "${platform#*/}" \
      '[.manifests[] | select(.platform.os == $os and .platform.architecture == $a)][0].digest // empty' <<<"$manifest")"
    [[ -n "$digest" ]] || fail "${ref}: index has no ${platform} manifest"
    manifest="$(curl -fsS -H "Authorization: Bearer ${token}" -H "Accept: ${accept}" \
      "https://ghcr.io/v2/${repo}/manifests/${digest}")" || fail "${ref}: platform manifest fetch failed"
  fi
  config_digest="$(jq -r '.config.digest // empty' <<<"$manifest")"
  [[ -n "$config_digest" ]] || fail "${ref}: manifest has no config digest"
  config="$(curl -fsSL -H "Authorization: Bearer ${token}" "https://ghcr.io/v2/${repo}/blobs/${config_digest}")" \
    || fail "${ref}: config blob fetch failed"

  jq -e --arg os "${platform%%/*}" --arg a "${platform#*/}" --arg v "$version" --arg arch "$arch" --arg inputs "$inputs" '
    .os == $os and .architecture == $a
    and .config.Labels["io.hass.version"] == $v
    and .config.Labels["io.hass.arch"] == $arch
    and .config.Labels["io.hass.type"] == "app"
    and .config.Labels["org.opencontainers.image.source"] == "https://github.com/markusleben/ha-nova"
    and ($inputs == "" or .config.Labels["io.ha-nova.inputs"] == $inputs)
  ' >/dev/null <<<"$config" || fail "${ref}: platform/labels mismatch (want ${platform}, io.hass.version=${version}, io.hass.arch=${arch}${inputs:+, io.ha-nova.inputs=${inputs}}); got $(jq -c '{os, architecture, labels: .config.Labels}' <<<"$config")"
  echo "[verify-app-image] OK: ${ref} (${platform})"
done
