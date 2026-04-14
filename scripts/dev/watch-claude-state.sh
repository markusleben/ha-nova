#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
STATE_TOOL="${REPO_ROOT}/scripts/dev/claude-plugin-state.mjs"
HOME_ROOT="${1:-${HOME}}"
CACHE_ROOT_DEFAULT="${HOME_ROOT}/Library/Caches/ha-nova/claude-drift-audit"
if [[ ! -d "${HOME_ROOT}/Library/Caches" ]]; then
  CACHE_ROOT_DEFAULT="${HOME_ROOT}/.cache/ha-nova/claude-drift-audit"
fi
OUT_DIR="${HA_NOVA_CLAUDE_AUDIT_DIR:-${CACHE_ROOT_DEFAULT}}"
EVENT_LOG="${OUT_DIR}/events.jsonl"
LATEST_JSON="${OUT_DIR}/latest.json"
CLAUDE_DIR="${HOME_ROOT}/.claude"
PLUGIN_DIR="${CLAUDE_DIR}/plugins"
INSTALLED_PLUGINS_JSON="${PLUGIN_DIR}/installed_plugins.json"
KNOWN_MARKETPLACES_JSON="${PLUGIN_DIR}/known_marketplaces.json"
SETTINGS_JSON="${CLAUDE_DIR}/settings.json"
SETTINGS_LOCAL_JSON="${CLAUDE_DIR}/settings.local.json"

require_cmd() {
  local name="$1"
  shift
  local candidate

  if candidate="$(command -v "${name}" 2>/dev/null)"; then
    printf '%s\n' "${candidate}"
    return 0
  fi

  for candidate in "$@"; do
    if [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  echo "[claude-audit] Missing command: ${name}" >&2
  exit 1
}

NODE_BIN="$(require_cmd node /opt/homebrew/bin/node /usr/local/bin/node)"
FSWATCH_BIN="$(require_cmd fswatch /opt/homebrew/bin/fswatch /usr/local/bin/fswatch)"
[[ -f "${STATE_TOOL}" ]] || {
  echo "[claude-audit] Missing helper: ${STATE_TOOL}" >&2
  exit 1
}

mkdir -p "${OUT_DIR}"

WATCH_PATHS=(
  "${PLUGIN_DIR}"
  "${INSTALLED_PLUGINS_JSON}"
  "${KNOWN_MARKETPLACES_JSON}"
  "${SETTINGS_JSON}"
  "${SETTINGS_LOCAL_JSON}"
)

normalize_trigger_path() {
  local changed_path="$1"
  case "${changed_path}" in
    "${INSTALLED_PLUGINS_JSON}") echo "${INSTALLED_PLUGINS_JSON}" ;;
    "${KNOWN_MARKETPLACES_JSON}") echo "${KNOWN_MARKETPLACES_JSON}" ;;
    "${SETTINGS_JSON}") echo "${SETTINGS_JSON}" ;;
    "${SETTINGS_LOCAL_JSON}") echo "${SETTINGS_LOCAL_JSON}" ;;
    "${PLUGIN_DIR}/installed_plugins.json") echo "${INSTALLED_PLUGINS_JSON}" ;;
    "${PLUGIN_DIR}/known_marketplaces.json") echo "${KNOWN_MARKETPLACES_JSON}" ;;
    *) return 1 ;;
  esac
}

capture_event() {
  local trigger_path="$1"
  if ! "${NODE_BIN}" "${STATE_TOOL}" write-watch-event "${HOME_ROOT}" "${trigger_path}" "${EVENT_LOG}" "${LATEST_JSON}"; then
    echo "[claude-audit] helper failed for: ${trigger_path}" >&2
    return 1
  fi
}

capture_event "__startup__"
echo "[claude-audit] Watching Claude registry state"
echo "[claude-audit] Output dir: ${OUT_DIR}"

"${FSWATCH_BIN}" -0 "${WATCH_PATHS[@]}" | while IFS= read -r -d '' changed_path; do
  if ! trigger_path="$(normalize_trigger_path "${changed_path}")"; then
    continue
  fi
  if capture_event "${trigger_path}"; then
    echo "[claude-audit] captured: ${trigger_path}"
  fi
done
