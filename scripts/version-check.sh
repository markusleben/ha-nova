#!/usr/bin/env bash
# Checks for HA NOVA skill updates. Outputs one line if update available, nothing otherwise.
# Used by: SKILL.md instruction (all clients), dev:sync, onboarding
set -euo pipefail

CACHE_DIR="$HOME/.cache/ha-nova"
CACHE="$CACHE_DIR/latest-version.json"
TTL=86400
REMOTE_URL="https://raw.githubusercontent.com/markusleben/ha-nova/main/version.json"

# Find local version.json: try repo root (symlink installs), then config dir
find_local_version() {
  local candidates=(
    "$(git rev-parse --show-toplevel 2>/dev/null || echo __NONE__)/version.json"
    "$HOME/.config/ha-nova/version.json"
  )
  for f in "${candidates[@]}"; do
    [[ -f "$f" ]] && grep -q '"skill_version"' "$f" 2>/dev/null && echo "$f" && return
  done
}

extract_version() {
  sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" 2>/dev/null | head -1
}

semver_lt() {
  local IFS='.'; local -a a=($1) b=($2)
  for i in 0 1 2; do
    local ai="${a[$i]:-0}" bi="${b[$i]:-0}"
    (( ai < bi )) && return 0
    (( ai > bi )) && return 1
  done
  return 1
}

refresh_cache() {
  mkdir -p "$CACHE_DIR" 2>/dev/null || true
  local tmp
  tmp=$(mktemp)
  if curl -sS --connect-timeout 2 --max-time 5 "$REMOTE_URL" -o "$tmp" 2>/dev/null && grep -q '"skill_version"' "$tmp" 2>/dev/null; then
    mv "$tmp" "$CACHE"
  else
    rm -f "$tmp"
  fi
}

# --- Main ---
local_file=$(find_local_version)
[[ -z "$local_file" ]] && exit 0

local_v=$(extract_version "$local_file")
[[ -z "$local_v" ]] && exit 0

# Check cache freshness
if [[ ! -f "$CACHE" ]]; then
  refresh_cache
else
  if [[ "$(uname)" == "Darwin" ]]; then
    cache_age=$(( $(date +%s) - $(stat -f %m "$CACHE" 2>/dev/null || echo 0) ))
  else
    cache_age=$(( $(date +%s) - $(stat -c %Y "$CACHE" 2>/dev/null || echo 0) ))
  fi
  (( cache_age > TTL )) && refresh_cache
fi

[[ ! -f "$CACHE" ]] && exit 0
remote_v=$(extract_version "$CACHE")
[[ -z "$remote_v" ]] && exit 0

if semver_lt "$local_v" "$remote_v"; then
  if [[ -x "${HOME}/.config/ha-nova/update" ]]; then
    echo "⚠️ UPDATE AVAILABLE: v${local_v} → v${remote_v} | Run: ~/.config/ha-nova/update"
  else
    echo "⚠️ UPDATE AVAILABLE: v${local_v} → v${remote_v} | Run: git pull in your ha-nova repo, then re-run setup."
  fi
  echo "AGENT: Include version info and update command above in your response to the user."
fi
