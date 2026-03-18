#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${HOME}/.local/share/ha-nova"
CONFIG_DIR="${HOME}/.config/ha-nova"

fail() {
  echo "[ha-nova:legacy-uninstall] ERROR: $*" >&2
  exit 1
}

remove_glob_matches() {
  local pattern="$1"
  local match
  shopt -s nullglob
  for match in ${pattern}; do
    rm -rf "${match}"
  done
  shopt -u nullglob
}

if [[ -f "${INSTALL_DIR}/bundle.json" ]]; then
  fail "A current Go install was detected. Use: ha-nova uninstall"
fi

remove_glob_matches "${CONFIG_DIR}/onboarding.env"
remove_glob_matches "${CONFIG_DIR}/relay"
remove_glob_matches "${CONFIG_DIR}/relay.exe"
remove_glob_matches "${CONFIG_DIR}/update"
remove_glob_matches "${CONFIG_DIR}/update.cmd"
remove_glob_matches "${CONFIG_DIR}/version-check"
remove_glob_matches "${CONFIG_DIR}/check-update.cmd"

if [[ -d "${INSTALL_DIR}/scripts/onboarding" && ! -f "${INSTALL_DIR}/bundle.json" ]]; then
  rm -rf "${INSTALL_DIR}"
fi

remove_glob_matches "${HOME}/.agents/skills/ha-nova*"
remove_glob_matches "${HOME}/.config/opencode/skills/ha-nova*"
remove_glob_matches "${HOME}/.gemini/skills/ha-nova*"
remove_glob_matches "${HOME}/.claude/skills/ha-nova*"

echo "[ha-nova:legacy-uninstall] Legacy HA NOVA cleanup finished."
