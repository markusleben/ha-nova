#!/usr/bin/env bash
set -euo pipefail

SKILL_NAMES=(
  "ha-nova"
  "ha-nova-write"
  "ha-nova-read"
  "ha-nova-entity-discovery"
  "ha-nova-onboarding"
  "ha-nova-service-call"
)

SKILL_DIRS=(
  "${HOME}/.agents/skills"
  "${HOME}/.claude/skills"
  "${HOME}/.config/opencode/skills"
)

log() { echo "[ha-nova] $*"; }

removed=0

remove_path() {
  local path="$1"
  if [[ -e "$path" ]]; then
    rm -rf "$path"
    log "Removed: $path"
    removed=$((removed + 1))
  fi
}

remove_known_config() {
  local config_dir="${HOME}/.config/ha-nova"
  [[ -d "$config_dir" ]] || return 0
  remove_path "${config_dir}/relay"
  remove_path "${config_dir}/onboarding.env"
  remove_path "${config_dir}/doctor-cache.env"
  # Remove dir only if empty (preserves user's custom files)
  rmdir "$config_dir" 2>/dev/null && log "Removed: ${config_dir}" && removed=$((removed + 1)) || true
}

echo ""
echo "  HA NOVA Uninstall"
echo "  ─────────────────"
echo ""
echo "  This will remove:"
echo "    • Skills from all AI client directories"
echo "    • Relay CLI (~/.config/ha-nova/relay)"
echo "    • Config (~/.config/ha-nova/)"
echo "    • Keychain entry (ha-nova.relay-auth-token)"
echo ""

if [[ "${1:-}" == "--yes" || "${1:-}" == "-y" ]]; then
  confirm="y"
else
  printf "  Continue? [y/N]: "
  read -r confirm
fi

if [[ "${confirm:-n}" != "y" && "${confirm:-n}" != "Y" ]]; then
  echo "  Cancelled."
  exit 0
fi

echo ""

# ── Remove skills from all client directories ──
for skills_dir in "${SKILL_DIRS[@]}"; do
  for skill in "${SKILL_NAMES[@]}"; do
    remove_path "${skills_dir}/${skill}"
  done
done

# ── Remove known config files (preserves any custom files) ──
remove_known_config

# ── Remove Keychain entry ──
if security find-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1; then
  security delete-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1 || true
  log "Removed Keychain entry: ha-nova.relay-auth-token"
  removed=$((removed + 1))
fi

echo ""
if [[ "$removed" -gt 0 ]]; then
  log "Done. Removed ${removed} items."
else
  log "Nothing to remove — HA NOVA was not installed."
fi
echo ""
echo "  To also remove the npm package:"
echo "    npm uninstall -g ha-nova"
echo ""
