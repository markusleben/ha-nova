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
CLIENT="${1:-}"

if [[ -z "${CLIENT}" ]]; then
  echo "Usage: $0 <claude|codex|opencode|gemini>" >&2
  exit 1
fi

TMP_HOME="$(create_temp_dir "${TMPDIR:-/tmp}/ha-nova-macos-client.XXXXXX")"
LOG_PATH="${LOG_PATH:-$(create_temp_file "${TMPDIR:-/tmp}/ha-nova-macos-${CLIENT}.XXXXXX")}"

claude_marketplace_points_to_root() {
  local known_marketplaces="$1"
  local expected_root="$2"
  python3 - "${known_marketplaces}" "${expected_root}" <<'PY'
import json
import os
import sys

path = sys.argv[1]
expected = os.path.normpath(sys.argv[2])

with open(path, "r", encoding="utf-8") as handle:
    data = json.load(handle)

entry = data.get("ha-nova")
if not isinstance(entry, dict):
    raise SystemExit(1)

candidates = []
source = entry.get("source")
if isinstance(source, str):
    candidates.append(source)
elif isinstance(source, dict):
    source_path = source.get("path")
    if isinstance(source_path, str):
        candidates.append(source_path)

install_location = entry.get("installLocation")
if isinstance(install_location, str):
    candidates.append(install_location)

for candidate in candidates:
    if os.path.normpath(candidate) == expected:
        raise SystemExit(0)

raise SystemExit(1)
PY
}

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
export HA_NOVA_KEYRING_SERVICE="ha-nova.test.$(basename "${TMP_HOME}").${CLIENT}"
runtime_bin="${TMP_HOME}/.local/bin/ha-nova"

{
  bash "${ROOT_DIR}/install.sh"
  "${runtime_bin}" setup "${CLIENT}" \
    --host 127.0.0.1 \
    --ha-url "http://127.0.0.1:${MOCK_HA_PORT}" \
    --relay-url "http://127.0.0.1:${MOCK_RELAY_PORT}" \
    --relay-token "${RELAY_TOKEN}" \
    --non-interactive
} 2>&1 | tee "${LOG_PATH}"

case "${CLIENT}" in
  codex)
    test -f "${TMP_HOME}/.agents/skills/ha-nova/ha-nova/SKILL.md"
    ;;
  opencode)
    test -f "${TMP_HOME}/.config/opencode/skills/ha-nova/ha-nova/SKILL.md"
    ;;
  gemini)
    test -f "${TMP_HOME}/.gemini/skills/ha-nova/SKILL.md"
    test -f "${TMP_HOME}/.gemini/skills/ha-nova-review/SKILL.md"
    ;;
  claude)
    command -v claude >/dev/null 2>&1
    grep -Fq '"ha-nova@ha-nova"' "${TMP_HOME}/.claude/plugins/installed_plugins.json"
    claude_marketplace_points_to_root "${TMP_HOME}/.claude/plugins/known_marketplaces.json" "${TMP_HOME}/.config/ha-nova/claude-marketplace"
    ;;
  *)
    echo "Unsupported client: ${CLIENT}" >&2
    exit 1
    ;;
esac

"${runtime_bin}" uninstall --yes >/dev/null
test ! -e "${TMP_HOME}/.local/bin/ha-nova"
test ! -e "${TMP_HOME}/.local/share/ha-nova"
test -e "${TMP_HOME}/.config/ha-nova/config.json"
test ! -e "${TMP_HOME}/.config/ha-nova/state.json"
test ! -e "${TMP_HOME}/.cache/ha-nova"
if [[ "${CLIENT}" == "claude" ]]; then
  if [[ -f "${TMP_HOME}/.claude/plugins/installed_plugins.json" ]]; then
    ! grep -Fq '"ha-nova@ha-nova"' "${TMP_HOME}/.claude/plugins/installed_plugins.json"
  fi
fi

printf 'MACOS_PRIVATE_RC_CLIENT_OK:%s:%s\n' "${CLIENT}" "${LOG_PATH}"
