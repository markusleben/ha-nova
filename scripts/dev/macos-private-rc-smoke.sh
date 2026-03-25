#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUNDLE_SERVER_BASE_URL="${BUNDLE_SERVER_BASE_URL:-http://127.0.0.1:8917}"

normalize_path() {
  python3 - "$1" <<'PY'
import os
import sys

print(os.path.normpath(sys.argv[1]))
PY
}

TMP_HOME="$(normalize_path "$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-macos-smoke.XXXXXX")")"

detect_bundle_name() {
  case "$(uname -m)" in
    x86_64) printf '%s\n' "ha-nova-installer-bundle-macos-amd64" ;;
    arm64) printf '%s\n' "ha-nova-installer-bundle-macos-arm64" ;;
    *) echo "Unsupported macOS arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

cleanup() {
  rm -rf "${TMP_HOME}"
}
trap cleanup EXIT

bundle_name="$(detect_bundle_name)"
bundle_url="${HA_NOVA_BUNDLE_URL:-${BUNDLE_SERVER_BASE_URL}/${bundle_name}.tar.gz}"
bundle_sha_url="${HA_NOVA_BUNDLE_SHA256_URL:-${bundle_url}.sha256}"

export HOME="${TMP_HOME}"
export XDG_CONFIG_HOME="${TMP_HOME}/.config"
export XDG_DATA_HOME="${TMP_HOME}/.local/share"
export HA_NOVA_BUNDLE_URL="${bundle_url}"
export HA_NOVA_BUNDLE_SHA256_URL="${bundle_sha_url}"
export HA_NOVA_NO_SETUP=1
export HA_NOVA_NO_BROWSER=1
export HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1
export HA_NOVA_TEST_KEYRING_FILE="${TMP_HOME}/.config/ha-nova/.test-relay-auth-token"

bash "${ROOT_DIR}/install.sh"
"${TMP_HOME}/.local/bin/ha-nova" version
"${TMP_HOME}/.local/bin/ha-nova" uninstall --yes

test ! -e "${TMP_HOME}/.local/share/ha-nova"
test ! -e "${TMP_HOME}/.config/ha-nova/config.json"
test ! -e "${TMP_HOME}/.config/ha-nova/state.json"
test ! -e "${TMP_HOME}/.cache/ha-nova"

printf 'MACOS_PRIVATE_RC_SMOKE_OK:%s\n' "${TMP_HOME}"
