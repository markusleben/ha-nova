#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/validation-common.sh
. "${SCRIPT_DIR}/lib/validation-common.sh"
ROOT_DIR="$(validation_repo_root "${BASH_SOURCE[0]}" "../..")"
BUNDLE_SERVER_BASE_URL="${BUNDLE_SERVER_BASE_URL:-http://127.0.0.1:8917}"
MOCK_HA_PORT="${MOCK_HA_PORT:-8123}"
MOCK_RELAY_PORT="${MOCK_RELAY_PORT:-8791}"
RELAY_TOKEN="${RELAY_TOKEN:-test-relay-token}"

TMP_HOME="$(create_temp_dir "${TMPDIR:-/tmp}/ha-nova-macos-setup-all.XXXXXX")"
LOG_PATH="${LOG_PATH:-$(create_temp_file "${TMPDIR:-/tmp}/ha-nova-macos-setup-all.XXXXXX")}"

cleanup() {
  if [[ -n "${MOCK_PID:-}" ]]; then
    kill "${MOCK_PID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_HOME}"
}
trap cleanup EXIT

wait_for_mock_server() {
  local ha_url="http://127.0.0.1:${MOCK_HA_PORT}/"
  local relay_url="http://127.0.0.1:${MOCK_RELAY_PORT}/health"
  local attempts=20

  for ((i = 1; i <= attempts; i++)); do
    if ! kill -0 "${MOCK_PID}" >/dev/null 2>&1; then
      echo "Mock HA/Relay server exited early." >&2
      cat "${TMP_HOME}/mock-server.log" >&2 || true
      exit 1
    fi
    if curl -fsS "${ha_url}" >/dev/null 2>&1 && curl -fsS "${relay_url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done

  echo "Mock HA/Relay server did not become ready." >&2
  cat "${TMP_HOME}/mock-server.log" >&2 || true
  exit 1
}

bundle_url="$(macos_bundle_url "${BUNDLE_SERVER_BASE_URL}")"
bundle_sha_url="$(macos_bundle_sha_url "${bundle_url}")"
MOCK_REPORTED_VERSION="$(require_default_bundle_version_or_explicit_mock "${ROOT_DIR}" "${BUNDLE_SERVER_BASE_URL}" "${bundle_url}")"
require_bundle_assets_ready "${bundle_url}" "${bundle_sha_url}"

python3 "${ROOT_DIR}/scripts/dev/mock-ha-relay.py" \
  --ha-port "${MOCK_HA_PORT}" \
  --relay-port "${MOCK_RELAY_PORT}" \
  --reported-version "${MOCK_REPORTED_VERSION}" \
  >"${TMP_HOME}/mock-server.log" 2>&1 &
MOCK_PID=$!
wait_for_mock_server

export HOME="${TMP_HOME}"
export XDG_CONFIG_HOME="${TMP_HOME}/.config"
export XDG_DATA_HOME="${TMP_HOME}/.local/share"
export HA_NOVA_BUNDLE_URL="${bundle_url}"
export HA_NOVA_BUNDLE_SHA256_URL="${bundle_sha_url}"
export HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1
export HA_NOVA_NO_SETUP=1
export HA_NOVA_NO_BROWSER=1
export HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1
export HA_NOVA_TEST_KEYRING_FILE="${TMP_HOME}/.config/ha-nova/.test-relay-auth-token"
export HA_NOVA_KEYRING_SERVICE="ha-nova.test.$(basename "${TMP_HOME}").setup-all"
runtime_bin="${TMP_HOME}/.local/bin/ha-nova"
token_file="${HA_NOVA_TEST_KEYRING_FILE}"

{
  bash "${ROOT_DIR}/install.sh"
  same_version="$("${runtime_bin}" version)"
  "${runtime_bin}" setup all \
    --host 127.0.0.1 \
    --ha-url "http://127.0.0.1:${MOCK_HA_PORT}" \
    --relay-url "http://127.0.0.1:${MOCK_RELAY_PORT}" \
    --relay-token "${RELAY_TOKEN}" \
    --non-interactive
  "${runtime_bin}" doctor
  "${runtime_bin}" relay version
  "${runtime_bin}" update --version "${same_version}"
  "${runtime_bin}" uninstall --yes
} 2>&1 | tee "${LOG_PATH}"

test ! -e "${TMP_HOME}/.local/share/ha-nova"
test -e "${TMP_HOME}/.config/ha-nova/config.json"
test ! -e "${TMP_HOME}/.config/ha-nova/state.json"
test ! -e "${TMP_HOME}/.cache/ha-nova"
test -e "${token_file}"
test "$(tr -d '\r\n' < "${token_file}")" = "${RELAY_TOKEN}"
test ! -e "${TMP_HOME}/.local/bin/ha-nova"

printf 'MACOS_PRIVATE_RC_SETUP_ALL_OK:%s\n' "${LOG_PATH}"
