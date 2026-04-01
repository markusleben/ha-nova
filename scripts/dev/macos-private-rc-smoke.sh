#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/validation-common.sh
. "${SCRIPT_DIR}/lib/validation-common.sh"
ROOT_DIR="$(validation_repo_root "${BASH_SOURCE[0]}" "../..")"
BUNDLE_SERVER_BASE_URL="${BUNDLE_SERVER_BASE_URL:-http://127.0.0.1:8917}"
TMP_ROOT="$(create_temp_dir "${TMPDIR:-/tmp}/ha-nova-macos-smoke.XXXXXX")"

cleanup() {
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

bundle_url="$(macos_bundle_url "${BUNDLE_SERVER_BASE_URL}")"
bundle_sha_url="$(macos_bundle_sha_url "${bundle_url}")"
require_bundle_assets_ready "${bundle_url}" "${bundle_sha_url}"

run_lane() {
  local lane="$1"
  local uninstall_args=("${@:2}")
  local tmp_home runtime_bin token_file

  tmp_home="${TMP_ROOT}/${lane}"
  runtime_bin="${tmp_home}/.local/bin/ha-nova"
  token_file="${tmp_home}/.config/ha-nova/.test-relay-auth-token"

  mkdir -p "${tmp_home}"
  export HOME="${tmp_home}"
  export XDG_CONFIG_HOME="${tmp_home}/.config"
  export XDG_DATA_HOME="${tmp_home}/.local/share"
  export HA_NOVA_BUNDLE_URL="${bundle_url}"
  export HA_NOVA_BUNDLE_SHA256_URL="${bundle_sha_url}"
  export HA_NOVA_NO_SETUP=1
  export HA_NOVA_NO_BROWSER=1
  export HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1
  export HA_NOVA_TEST_KEYRING_FILE="${token_file}"

  bash "${ROOT_DIR}/install.sh"
  "${runtime_bin}" version

  mkdir -p "${tmp_home}/.config/ha-nova" "${tmp_home}/.cache/ha-nova"
  printf '{"ha_url":"http://127.0.0.1:8123","relay_base_url":"http://127.0.0.1:8791"}\n' > "${tmp_home}/.config/ha-nova/config.json"
  printf '{"schema_version":1,"install_source":"bundle"}\n' > "${tmp_home}/.config/ha-nova/state.json"
  printf 'test-relay-token\n' > "${token_file}"
  printf '{}\n' > "${tmp_home}/.cache/ha-nova/latest-release.json"

  "${runtime_bin}" uninstall "${uninstall_args[@]}"

  test ! -e "${tmp_home}/.local/bin/ha-nova"
  test ! -e "${tmp_home}/.local/share/ha-nova"
  test ! -e "${tmp_home}/.config/ha-nova/state.json"
  test ! -e "${tmp_home}/.cache/ha-nova"

  if [[ "${lane}" == "purge" ]]; then
    test ! -e "${tmp_home}/.config/ha-nova/config.json"
    test ! -e "${token_file}"
    return 0
  fi

  test -e "${tmp_home}/.config/ha-nova/config.json"
  test -e "${token_file}"
}

run_lane standard --yes
run_lane purge --yes --purge

printf 'MACOS_PRIVATE_RC_SMOKE_OK\n'
