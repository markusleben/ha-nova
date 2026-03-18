#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUNDLE_SERVER_BASE_URL="${BUNDLE_SERVER_BASE_URL:-http://127.0.0.1:8917}"
MOCK_HA_PORT="${MOCK_HA_PORT:-8123}"
MOCK_RELAY_PORT="${MOCK_RELAY_PORT:-8791}"
RELAY_TOKEN="${RELAY_TOKEN:-test-relay-token}"
CLIENT="${1:-}"

if [[ -z "${CLIENT}" ]]; then
  echo "Usage: $0 <claude|codex|opencode|gemini>" >&2
  exit 1
fi

TMP_HOME="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-macos-client.XXXXXX")"
LOG_PATH="${LOG_PATH:-${TMP_HOME}/ha-nova-macos-${CLIENT}.log}"

detect_bundle_name() {
  case "$(uname -m)" in
    x86_64) printf '%s\n' "ha-nova-installer-bundle-macos-amd64" ;;
    arm64) printf '%s\n' "ha-nova-installer-bundle-macos-arm64" ;;
    *) echo "Unsupported macOS arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

bundle_reported_version() {
  local bundle_path="$1"
  local version_member
  version_member="$(tar -tzf "${bundle_path}" 2>/dev/null | grep -E '(^|/)version\.json$' | head -1 || true)"
  [[ -n "${version_member}" ]] || return 1
  tar -xOf "${bundle_path}" "${version_member}" 2>/dev/null | node -e 'const fs=require("fs"); const data=JSON.parse(fs.readFileSync(0,"utf8")); console.log(data.skill_version || data.version || "");'
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

bundle_name="$(detect_bundle_name)"
bundle_url="${HA_NOVA_BUNDLE_URL:-${BUNDLE_SERVER_BASE_URL}/${bundle_name}.tar.gz}"
bundle_sha_url="${HA_NOVA_BUNDLE_SHA256_URL:-${bundle_url}.sha256}"
local_bundle_path="${ROOT_DIR}/dist/install-bundles/${bundle_name}.tar.gz"
if [[ -z "${MOCK_REPORTED_VERSION}" ]]; then
  if [[ "${BUNDLE_SERVER_BASE_URL}" != "http://127.0.0.1:8917" || (-n "${HA_NOVA_BUNDLE_URL:-}" && "${bundle_url}" != "${BUNDLE_SERVER_BASE_URL}/${bundle_name}.tar.gz") ]]; then
    echo "Set MOCK_REPORTED_VERSION explicitly when overriding the bundle source." >&2
    exit 1
  fi
  MOCK_REPORTED_VERSION="$(bundle_reported_version "${local_bundle_path}")"
fi

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
    grep -Fq "\"${TMP_HOME}/.local/share/ha-nova\"" "${TMP_HOME}/.claude/plugins/known_marketplaces.json"
    ;;
  *)
    echo "Unsupported client: ${CLIENT}" >&2
    exit 1
    ;;
esac

"${runtime_bin}" uninstall --yes >/dev/null
test ! -e "${TMP_HOME}/.local/share/ha-nova"
test ! -e "${TMP_HOME}/.config/ha-nova"
if [[ "${CLIENT}" == "claude" ]]; then
  if [[ -f "${TMP_HOME}/.claude/plugins/installed_plugins.json" ]]; then
    ! grep -Fq '"ha-nova@ha-nova"' "${TMP_HOME}/.claude/plugins/installed_plugins.json"
  fi
fi

printf 'MACOS_PRIVATE_RC_CLIENT_OK:%s:%s\n' "${CLIENT}" "${LOG_PATH}"
