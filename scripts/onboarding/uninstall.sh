#!/usr/bin/env bash
set -euo pipefail

SKILL_DIRS=(
  "${HOME}/.agents/skills"
  "${HOME}/.claude/skills"
  "${HOME}/.config/opencode/skills"
)

# Flat skill directories (legacy + Gemini installs)
FLAT_SKILLS=(
  "ha-nova-read"
  "ha-nova-write"
  "ha-nova-helper"
  "ha-nova-review"
  "ha-nova-entity-discovery"
  "ha-nova-onboarding"
  "ha-nova-service-call"
  "ha-nova-guide"
)

log() { echo "[ha-nova] $*"; }

removed=0

remove_path() {
  local path="$1"
  if [[ -L "$path" ]]; then
    rm -f "$path"
    log "Removed symlink: $path"
    removed=$((removed + 1))
  elif [[ -e "$path" ]]; then
    rm -rf "$path"
    log "Removed: $path"
    removed=$((removed + 1))
  fi
}

remove_known_config() {
  local config_dir="${HOME}/.config/ha-nova"
  [[ -d "$config_dir" ]] || return 0
  remove_path "${config_dir}/relay"
  remove_path "${config_dir}/update"
  remove_path "${config_dir}/version-check"
  remove_path "${config_dir}/version.json"
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
echo "    • Claude Code plugin cache"
echo "    • Relay CLI (~/.config/ha-nova/relay)"
echo "    • Config (~/.config/ha-nova/)"
echo "    • Cache (~/.cache/ha-nova/)"
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

# ── Probe relay BEFORE deleting config/token ──
relay_still_running="0"
relay_base_url=""
config_file="${HOME}/.config/ha-nova/onboarding.env"
if [[ -f "$config_file" ]]; then
  relay_base_url="$(grep -E '^RELAY_BASE_URL=' "$config_file" 2>/dev/null | head -1 | sed "s/^RELAY_BASE_URL=//" | tr -d "'" | tr -d '"' || true)"
fi
relay_token="$(security find-generic-password -s "ha-nova.relay-auth-token" -w 2>/dev/null || true)"
if [[ -n "$relay_base_url" && -n "$relay_token" ]]; then
  http_code="$(curl -sS --connect-timeout 2 --max-time 4 \
    -H "Authorization: Bearer ${relay_token}" \
    -o /dev/null -w "%{http_code}" \
    "${relay_base_url%/}/health" 2>/dev/null || true)"
  if [[ "$http_code" == "200" ]]; then
    relay_still_running="1"
  fi
fi

# ── Remove skills from all client directories ──
for skills_dir in "${SKILL_DIRS[@]}"; do
  # Remove ha-nova/ (symlink or directory)
  remove_path "${skills_dir}/ha-nova"

  # Clean up flat skill directories (legacy + Gemini)
  for flat_skill in "${FLAT_SKILLS[@]}"; do
    remove_path "${skills_dir}/${flat_skill}"
  done
done

# ── Remove known config files (preserves any custom files) ──
remove_known_config

# ── Remove update cache ──
remove_path "${HOME}/.cache/ha-nova"

# ── Remove Claude Code plugin ──
if command -v claude &>/dev/null; then
  claude plugin remove ha-nova@ha-nova 2>/dev/null && log "Removed Claude Code plugin" && removed=$((removed + 1)) || true
fi
# Remove cache as fallback
remove_path "${HOME}/.claude/plugins/cache/ha-nova"

# ── Remove Keychain entry ──
if security find-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1; then
  security delete-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1 || true
  log "Removed Keychain entry: ha-nova.relay-auth-token"
  removed=$((removed + 1))
fi

echo ""
if [[ "$removed" -gt 0 ]]; then
  log "Done. Removed ${removed} items."
  if [[ "$relay_still_running" == "1" ]]; then
    echo ""
    echo "  Note: The NOVA Relay app is still running in Home Assistant."
    echo "  To remove it: Settings > Apps > NOVA Relay > Uninstall"
  fi
else
  log "Nothing to remove — HA NOVA was not installed."
fi
echo ""
