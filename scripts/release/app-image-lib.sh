# shellcheck shell=bash
# Shared facts about the prebuilt NOVA Relay App images (sourced, not run).
# Used by .github/workflows/app-image.yml and scripts/release/verify-app-image.sh
# so the image name, arch list, and build-input fingerprint have ONE definition.

APP_IMAGE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# Supervisor substitutes {arch}; nova/config.yaml `image:` must equal this.
APP_IMAGE_TEMPLATE="ghcr.io/markusleben/{arch}-ha-nova-relay"

app_image_ref() { # <arch> <version>
  printf '%s:%s\n' "${APP_IMAGE_TEMPLATE//\{arch\}/$1}" "$2"
}

app_config_version() { # [<git-rev>] — working tree when omitted
  app_config_at "${1:-}" | sed -nE 's/^version:[[:space:]]*"?([^"]+)"?[[:space:]]*$/\1/p' | head -1
}

app_config_image() { # [<git-rev>]
  app_config_at "${1:-}" | sed -nE 's/^image:[[:space:]]*"?([^"]+)"?[[:space:]]*$/\1/p' | head -1
}

app_config_at() {
  if [[ -n "${1:-}" ]]; then
    git -C "$APP_IMAGE_ROOT" show "$1:nova/config.yaml"
  else
    cat "$APP_IMAGE_ROOT/nova/config.yaml"
  fi
}

# "<arch> <base-image>" per line, from nova/build.yaml build_from.
app_build_from() {
  awk '
    /^build_from:/ { s = 1; next }
    /^[^[:space:]]/ { s = 0 }
    s && /^  [a-z0-9]+:/ { k = $1; sub(":", "", k); v = $2; gsub(/"/, "", v); print k, v }
  ' "$APP_IMAGE_ROOT/nova/build.yaml"
}

# Supervisor arch -> OCI platform (Supervisor's MAP_ARCH). Unknown = error.
app_arch_platform() {
  case "$1" in
    amd64) echo linux/amd64 ;;
    aarch64) echo linux/arm64 ;;
    *) echo "unsupported App arch: $1" >&2; return 1 ;;
  esac
}

# Fingerprint of everything the App image is built from. Squash merges keep
# file contents, so a PR head and its merge commit share the fingerprint.
app_image_inputs() { # <git-rev>
  git -C "$APP_IMAGE_ROOT" ls-tree -r "$1" -- \
    nova/Dockerfile nova/build.yaml nova/run nova/package.json \
    nova/package-lock.json nova/tsconfig.json nova/version.json nova/src \
    | git -C "$APP_IMAGE_ROOT" hash-object --stdin
}

# True while <version> may still be (re)pushed: newer than <git-ref>'s App
# version AND never present in <git-ref>'s nova/config.yaml history. A version
# that ever reached main is frozen forever (users and backup restores pull it).
app_version_publishable() { # <version> <git-ref>
  local ref_version
  ref_version="$(app_config_version "$2")"
  [[ "$1" != "$ref_version" && "$(printf '%s\n%s\n' "$1" "$ref_version" | sort -V | tail -1)" == "$1" ]] || return 1
  [[ -z "$(git -C "$APP_IMAGE_ROOT" log --format=%H -G "^version:[[:space:]]*\"?${1//./\\.}\"?[[:space:]]*$" "$2" -- nova/config.yaml)" ]]
}
