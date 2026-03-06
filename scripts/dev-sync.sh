#!/usr/bin/env bash
set -euo pipefail

# Syncs local repo skills/hooks to all installed client caches.
# Use after local changes to test without pushing to GitHub.

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

synced=()

# ─── Claude Code plugin cache ────────────────────────────────────────
sync_claude() {
  local plugins_json="${HOME}/.claude/plugins/installed_plugins.json"
  if [[ ! -f "$plugins_json" ]]; then
    echo "[dev:sync] Claude Code: no installed_plugins.json found — skipped"
    return
  fi

  # Extract installPath for ha-nova@ha-nova (no jq dependency)
  local install_path
  install_path=$(
    sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
      "$plugins_json" | head -1
  )

  if [[ -z "$install_path" ]]; then
    echo "[dev:sync] Claude Code: ha-nova plugin not installed — skipped"
    return
  fi

  # Expand ~ if present
  install_path="${install_path/#\~/$HOME}"

  if [[ ! -d "$install_path" ]]; then
    echo "[dev:sync] Claude Code: cache dir missing ($install_path) — skipped"
    return
  fi

  # Sync skills, hooks, plugin manifest, and version
  rsync -a --delete "${REPO_ROOT}/skills/" "${install_path}/skills/"
  rsync -a --delete "${REPO_ROOT}/hooks/" "${install_path}/hooks/"
  rsync -a --delete "${REPO_ROOT}/.claude-plugin/" "${install_path}/.claude-plugin/"
  cp "${REPO_ROOT}/version.json" "${install_path}/version.json"

  local version
  version=$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${REPO_ROOT}/version.json")
  echo "[dev:sync] Claude Code plugin cache synced (v${version}) → ${install_path}"
  synced+=("Claude Code")
}

# ─── Gemini flat copies ──────────────────────────────────────────────
sync_gemini() {
  local gemini_marker="${HOME}/.agents/skills/ha-nova-read/SKILL.md"
  if [[ ! -f "$gemini_marker" ]]; then
    echo "[dev:sync] Gemini: no flat copies found — skipped"
    return
  fi

  bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" gemini
  echo "[dev:sync] Gemini flat copies refreshed"
  synced+=("Gemini")
}

# ─── Relay CLI ────────────────────────────────────────────────────────
sync_relay() {
  local relay_src="${REPO_ROOT}/scripts/relay.sh"
  local relay_dst="${HOME}/.config/ha-nova/relay"

  if [[ ! -f "$relay_dst" ]]; then
    echo "[dev:sync] Relay CLI: not installed — skipped"
    return
  fi

  if [[ ! -f "$relay_src" ]]; then
    echo "[dev:sync] Relay CLI: source missing ($relay_src) — skipped"
    return
  fi

  cp "$relay_src" "$relay_dst"
  chmod 755 "$relay_dst"

  # Sync version-check script + version.json
  local vc_src="${REPO_ROOT}/scripts/version-check.sh"
  if [[ -f "$vc_src" ]]; then
    cp "$vc_src" "${HOME}/.config/ha-nova/version-check"
    chmod 755 "${HOME}/.config/ha-nova/version-check"
    cp "${REPO_ROOT}/version.json" "${HOME}/.config/ha-nova/version.json"
  fi

  echo "[dev:sync] Relay CLI updated"
  synced+=("Relay CLI")
}

# ─── Run ──────────────────────────────────────────────────────────────
sync_claude
sync_gemini
sync_relay

if [[ ${#synced[@]} -eq 0 ]]; then
  echo "[dev:sync] Nothing to sync — no clients detected."
else
  echo "[dev:sync] Done: ${synced[*]}"
fi
