#!/usr/bin/env bash
set -euo pipefail

# Syncs local repo changes to installed HA NOVA clients.
# KISS: for file-based clients, just re-run install-local-skills.sh.
# Claude Code remains the only special-case cache sync.

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

synced=()
file_clients_synced=0

refresh_file_client() {
  local name="$1"
  local target="$2"

  bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$target"
  echo "[dev:sync] ${name}: refreshed via install-local-skills.sh ${target}"
  synced+=("${name}")
  file_clients_synced=1
}

sync_symlink_client() {
  local name="$1"
  local link_path="$2"
  local target="$3"

  if [[ -L "$link_path" && -e "$link_path" ]]; then
    refresh_file_client "$name" "$target"
    return
  fi

  echo "[dev:sync] ${name}: not installed — skipped"
}

sync_gemini() {
  local context_marker="${HOME}/.gemini/skills/ha-nova/SKILL.md"
  local current_marker="${HOME}/.gemini/skills/ha-nova-read/SKILL.md"
  local legacy_marker="${HOME}/.agents/skills/ha-nova-read/SKILL.md"

  if [[ -f "$context_marker" || -f "$current_marker" || -f "$legacy_marker" ]]; then
    refresh_file_client "Gemini" "gemini"
    return
  fi

  echo "[dev:sync] Gemini: not installed — skipped"
}

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

  # ── Version-resilient cache discovery ──────────────────────────────
  # Claude Code stores plugins at cache/{registry}/{name}/{version}/.
  # After a version bump the old path may be gone while a new one exists.
  # Strategy: try exact path first → fallback to latest versioned dir →
  # if nothing exists, create the correct dir for the current repo version.
  if [[ ! -d "$install_path" ]]; then
    local cache_parent
    cache_parent="$(dirname "$install_path")"   # e.g. cache/ha-nova/ha-nova

    local actual_dir=""
    if [[ -d "$cache_parent" ]]; then
      # Find the latest (or only) versioned subdir
      actual_dir=$(ls -1d "${cache_parent}"/[0-9]* 2>/dev/null | sort -V | tail -1 || true)
    fi

    if [[ -z "$actual_dir" ]]; then
      # No versioned dir at all — create one for the repo version
      local repo_version
      repo_version=$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${REPO_ROOT}/version.json")
      if [[ -z "$repo_version" ]]; then
        echo "[dev:sync] Claude Code: could not read version from version.json — skipped"
        return
      fi
      actual_dir="${cache_parent}/${repo_version}"
      mkdir -p "$actual_dir"
      echo "[dev:sync] Claude Code: created cache dir ${actual_dir}"
    else
      echo "[dev:sync] Claude Code: installPath stale ($install_path), found ${actual_dir}"
    fi

    install_path="$actual_dir"
  fi

  # Sync skills, hooks, plugin manifest, and version
  rsync -a --delete "${REPO_ROOT}/skills/" "${install_path}/skills/"
  rsync -a --delete "${REPO_ROOT}/hooks/" "${install_path}/hooks/"
  rsync -a --delete "${REPO_ROOT}/.claude-plugin/" "${install_path}/.claude-plugin/"
  cp "${REPO_ROOT}/version.json" "${install_path}/version.json"

  # ── Keep installed_plugins.json in sync ────────────────────────────
  # Update installPath and version so Claude Code finds the plugin.
  local repo_version
  repo_version=$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${REPO_ROOT}/version.json")
  # Keep absolute paths — Claude Code uses absolute, not ~-prefixed
  local abs_path="$install_path"

  # Update installPath
  local old_path_pattern
  old_path_pattern=$(sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugins_json" | head -1)
  if [[ -n "$old_path_pattern" && "$old_path_pattern" != "$abs_path" ]]; then
    # Scope replacement to ha-nova block only
    sed -i '' "/"ha-nova@ha-nova"/,/installPath/{s|\"installPath\": \"${old_path_pattern}\"|\"installPath\": \"${abs_path}\"|;}" "$plugins_json"
  fi

  # Update version
  local old_version
  old_version=$(sed -n '/"ha-nova@ha-nova"/,/\"version\"/s/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugins_json" | head -1)
  if [[ -n "$old_version" && "$old_version" != "$repo_version" ]]; then
    # Scope the replacement to the ha-nova block: replace first occurrence of old version after ha-nova@ha-nova
    sed -i '' "/"ha-nova@ha-nova"/,/\"version\"/{s/\"version\": \"${old_version}\"/\"version\": \"${repo_version}\"/;}" "$plugins_json"
  fi

  echo "[dev:sync] Claude Code plugin cache synced (v${repo_version}) → ${install_path}"
  synced+=("Claude Code")
}

# ─── Shared tools fallback ───────────────────────────────────────────
# install-local-skills.sh already refreshes these for file-based clients.
# Keep this fallback for Claude-only setups.
sync_shared_tools() {
  local relay_dst="${HOME}/.config/ha-nova/relay"
  local config_dir="${HOME}/.config/ha-nova"

  if [[ ! -f "$relay_dst" ]]; then
    echo "[dev:sync] Shared tools: not installed — skipped"
    return
  fi

  # Build relay binary from local Go source (dev workflow — no GitHub download)
  if command -v go &>/dev/null && [[ -d "${REPO_ROOT}/cli" ]]; then
    (cd "${REPO_ROOT}/cli" && go build -o "${relay_dst}" .)
    chmod 755 "${relay_dst}"
    echo "[dev:sync] Built and deployed relay CLI from local Go source"
  else
    echo "[dev:sync] Warning: Go not installed or cli/ missing — relay CLI not updated"
  fi

  # Sync version-check, update script + version.json
  local vc_src="${REPO_ROOT}/scripts/version-check.sh"
  if [[ -f "$vc_src" ]]; then
    cp "$vc_src" "${config_dir}/version-check"
    chmod 755 "${config_dir}/version-check"
    cp "${REPO_ROOT}/version.json" "${config_dir}/version.json"
  fi

  local update_src="${REPO_ROOT}/scripts/update.sh"
  if [[ -f "$update_src" ]]; then
    cp "$update_src" "${config_dir}/update"
    chmod 755 "${config_dir}/update"
  fi

  echo "[dev:sync] Shared tools refreshed"
  synced+=("Shared tools")
}

# ─── Guardrail: verify installed_plugins.json integrity ──────────────
# Detects and auto-fixes mismatches between installPath in
# installed_plugins.json and the actual cache directory on disk.
# Prevents broken plugin discovery even after manual path manipulation.
verify_plugin_integrity() {
  local plugins_json="${HOME}/.claude/plugins/installed_plugins.json"
  [[ ! -f "$plugins_json" ]] && return

  local install_path
  install_path=$(
    sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
      "$plugins_json" | head -1
  )
  [[ -z "$install_path" ]] && return

  # Keep raw JSON value for sed matching; expand for filesystem checks
  local raw_install_path="$install_path"
  install_path="${install_path/#\~/$HOME}"

  # Happy path: installPath exists on disk
  if [[ -d "$install_path" ]]; then
    # Verify it actually contains plugin files (hooks/skills)
    if [[ -f "${install_path}/hooks/session-start" && -d "${install_path}/skills" ]]; then
      return  # all good
    fi
    echo "[dev:sync] GUARDRAIL: installPath exists but is missing plugin files: ${install_path}"
    echo "[dev:sync] GUARDRAIL: run 'bash scripts/dev-sync.sh' again or reinstall: claude plugin install ha-nova@ha-nova"
    return
  fi

  # installPath does NOT exist — find the actual versioned dir
  echo "[dev:sync] GUARDRAIL: installPath in installed_plugins.json does not exist: ${install_path}"

  local cache_parent
  cache_parent="$(dirname "$install_path")"  # e.g. cache/ha-nova/ha-nova

  local actual_dir=""
  if [[ -d "$cache_parent" ]]; then
    actual_dir=$(ls -1d "${cache_parent}"/[0-9]* 2>/dev/null | sort -V | tail -1 || true)
  fi

  if [[ -z "$actual_dir" ]]; then
    echo "[dev:sync] GUARDRAIL: no versioned directory found under ${cache_parent}"
    echo "[dev:sync] GUARDRAIL: reinstall required: claude plugin install ha-nova@ha-nova"
    return
  fi

  # Fix installed_plugins.json — match raw JSON value (may contain ~), replace with absolute path
  sed -i '' "/"ha-nova@ha-nova"/,/installPath/{s|\"installPath\": \"${raw_install_path}\"|\"installPath\": \"${actual_dir}\"|;}" "$plugins_json"

  # Also fix the version field to match the directory name
  local dir_version; dir_version=$(basename "$actual_dir")
  local old_version
  old_version=$(sed -n '/"ha-nova@ha-nova"/,/\"version\"/s/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugins_json" | head -1)
  if [[ -n "$old_version" && "$old_version" != "$dir_version" ]]; then
    sed -i '' "/"ha-nova@ha-nova"/,/\"version\"/{s/\"version\": \"${old_version}\"/\"version\": \"${dir_version}\"/;}" "$plugins_json"
  fi

  echo "[dev:sync] GUARDRAIL: FIXED installPath: ${install_path} → ${actual_dir}"
  echo "[dev:sync] GUARDRAIL: plugin discovery restored"
}

# ─── Run ──────────────────────────────────────────────────────────────
sync_symlink_client "Codex" "${HOME}/.agents/skills/ha-nova" "codex"
sync_symlink_client "OpenCode" "${HOME}/.config/opencode/skills/ha-nova" "opencode"
sync_gemini
sync_claude
if [[ "$file_clients_synced" -eq 0 ]]; then
  sync_shared_tools
fi
verify_plugin_integrity

if [[ ${#synced[@]} -eq 0 ]]; then
  echo "[dev:sync] Nothing to sync — no clients detected."
else
  echo "[dev:sync] Done: ${synced[*]}"
fi
