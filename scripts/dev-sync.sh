#!/usr/bin/env bash
set -euo pipefail

# Syncs local repo changes to installed HA NOVA clients.
# KISS: for file-based clients, just re-run install-local-skills.sh.
# Claude Code remains the only special-case cache sync.

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=onboarding/lib/install-local-skills-common.sh
. "${REPO_ROOT}/scripts/onboarding/lib/install-local-skills-common.sh"
# shellcheck source=onboarding/lib/install-local-skills-claude.sh
. "${REPO_ROOT}/scripts/onboarding/lib/install-local-skills-claude.sh"

CLAUDE_PLUGIN_STATE_TOOL="$(claude_plugin_state_tool)"

synced=()
file_clients_synced=0

print_claude_repair_hint() {
  echo "[dev:sync] GUARDRAIL: rerun 'npm run dev:sync'; if Claude still stays detached, run 'ha-nova setup claude'."
}

require_repo_invariants() {
  [[ -d "${REPO_ROOT}/skills" ]] || {
    echo "[dev:sync] ERROR: missing repo skills directory: ${REPO_ROOT}/skills" >&2
    exit 1
  }
  [[ -f "${REPO_ROOT}/version.json" ]] || {
    echo "[dev:sync] ERROR: missing repo version file: ${REPO_ROOT}/version.json" >&2
    exit 1
  }
  [[ -x "${REPO_ROOT}/scripts/onboarding/bin/ha-nova" ]] || {
    echo "[dev:sync] ERROR: missing repo helper runtime shim: ${REPO_ROOT}/scripts/onboarding/bin/ha-nova" >&2
    exit 1
  }
}

state_lists_client() {
  local client="$1"
  local state_file="${HOME}/.config/ha-nova/state.json"
  [[ ! -f "$state_file" ]] && return 1
  sed -n '/"installed_clients"[[:space:]]*:/,/\]/{p;}' "$state_file" | grep -Fq "\"${client}\""
}

file_client_install_present() {
  local install_root="$1"

  if [[ -L "${install_root}" && -e "${install_root}" ]]; then
    return 0
  fi

  [[ -d "${install_root}" && -f "${install_root}/ha-nova/SKILL.md" ]]
}

refresh_file_client() {
  local name="$1"
  local target="$2"

  bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$target"
  echo "[dev:sync] ${name}: refreshed via install-local-skills.sh ${target}"
  synced+=("${name}")
  file_clients_synced=1
}

sync_file_client() {
  local name="$1"
  local link_path="$2"
  local target="$3"

  if file_client_install_present "$link_path"; then
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

sync_claude() {
  local plugins_json="${HOME}/.claude/plugins/installed_plugins.json"
  local state_expects_claude=0
  local helper_required=0
  if state_lists_client "claude"; then
    state_expects_claude=1
  fi
  if claude_plugin_state_helper_required; then
    helper_required=1
  fi

  if [[ "${state_expects_claude}" -eq 0 && "${helper_required}" -eq 0 ]]; then
    echo "[dev:sync] Claude Code: ha-nova plugin not installed — skipped"
    return
  fi

  if [[ "${state_expects_claude}" -eq 1 && "${helper_required}" -eq 0 ]]; then
    echo "[dev:sync] Claude Code: configured in state.json but plugin record is missing — reinstalling"
    refresh_file_client "Claude Code" "claude"
    return
  fi

  require_claude_plugin_state_tool "[dev:sync] ERROR:" || exit 1

  if [[ ! -f "${plugins_json}" ]]; then
    if [[ "${state_expects_claude}" -eq 1 ]]; then
      echo "[dev:sync] Claude Code: configured in state.json but plugin record is missing — reinstalling"
      refresh_file_client "Claude Code" "claude"
      return
    fi
    echo "[dev:sync] Claude Code: ha-nova plugin not installed — skipped"
    return
  fi

  local install_path
  install_path="$(claude_plugin_state_value "install_path" "${plugins_json}")"

  if [[ -z "${install_path}" ]]; then
    if [[ "${state_expects_claude}" -eq 1 ]]; then
      echo "[dev:sync] Claude Code: configured in state.json but plugin installPath is missing — reinstalling"
      refresh_file_client "Claude Code" "claude"
      return
    fi
    echo "[dev:sync] Claude Code: ha-nova plugin not installed — skipped"
    return
  fi

  install_path="${install_path/#\~/$HOME}"

  if [[ ! -d "${install_path}" ]]; then
    local cache_parent actual_dir repo_version
    cache_parent="$(dirname "${install_path}")"
    actual_dir=""

    if [[ -d "${cache_parent}" ]]; then
      actual_dir="$(ls -1d "${cache_parent}"/[0-9]* 2>/dev/null | sort -V | tail -1 || true)"
    fi

    if [[ -z "${actual_dir}" ]]; then
      repo_version="$(repo_skill_version)"
      if [[ -z "${repo_version}" ]]; then
        echo "[dev:sync] Claude Code: could not read version from version.json — skipped"
        return
      fi
      actual_dir="${cache_parent}/${repo_version}"
      mkdir -p "${actual_dir}"
      echo "[dev:sync] Claude Code: created cache dir ${actual_dir}"
    else
      echo "[dev:sync] Claude Code: installPath stale (${install_path}), found ${actual_dir}"
    fi

    install_path="${actual_dir}"
  fi

  rsync -a --delete "${REPO_ROOT}/skills/" "${install_path}/skills/"
  rsync -a --delete "${REPO_ROOT}/hooks/" "${install_path}/hooks/"
  rsync -a --delete "${REPO_ROOT}/.claude-plugin/" "${install_path}/.claude-plugin/"
  cp "${REPO_ROOT}/version.json" "${install_path}/version.json"

  local repo_version
  repo_version="$(repo_skill_version)"
  node "${CLAUDE_PLUGIN_STATE_TOOL}" repair-plugin-record "${plugins_json}" "${install_path}" "${repo_version}"

  echo "[dev:sync] Claude Code plugin cache synced (v${repo_version}) → ${install_path}"
  synced+=("Claude Code")
}

sync_shared_tools() {
  local relay_dst="${HOME}/.config/ha-nova/relay"
  local config_dir="${HOME}/.config/ha-nova"

  if [[ ! -f "${relay_dst}" ]]; then
    echo "[dev:sync] Shared tools: not installed — skipped"
    return
  fi

  if command -v go &>/dev/null && [[ -d "${REPO_ROOT}/cli" ]]; then
    (cd "${REPO_ROOT}/cli" && go build -o "${relay_dst}" .)
    chmod 755 "${relay_dst}"
    echo "[dev:sync] Built and deployed relay CLI from local Go source"
  else
    echo "[dev:sync] Warning: Go not installed or cli/ missing — relay CLI not updated"
  fi

  write_repo_cli_wrapper "${config_dir}/version-check" "check-update" "--quiet"
  cp "${REPO_ROOT}/version.json" "${config_dir}/version.json"

  echo "[dev:sync] Shared tools refreshed"
  synced+=("Shared tools")
}

verify_plugin_integrity() {
  local plugins_json="${HOME}/.claude/plugins/installed_plugins.json"
  [[ -f "${plugins_json}" ]] || return 0
  claude_plugin_record_present || return 0

  require_claude_plugin_state_tool "[dev:sync] ERROR:" || exit 1

  local raw_install_path
  raw_install_path="$(claude_plugin_state_value "install_path" "${plugins_json}")"
  [[ -z "${raw_install_path}" ]] && return 0

  local install_path="${raw_install_path/#\~/$HOME}"
  if [[ -d "${install_path}" ]]; then
    if [[ -f "${install_path}/hooks/session-start" && -d "${install_path}/skills" ]]; then
      return 0
    fi
    echo "[dev:sync] GUARDRAIL: installPath exists but is missing plugin files: ${install_path}"
    print_claude_repair_hint
    return 0
  fi

  echo "[dev:sync] GUARDRAIL: installPath in installed_plugins.json does not exist: ${install_path}"

  local cache_parent actual_dir
  cache_parent="$(dirname "${install_path}")"
  actual_dir=""
  if [[ -d "${cache_parent}" ]]; then
    actual_dir="$(ls -1d "${cache_parent}"/[0-9]* 2>/dev/null | sort -V | tail -1 || true)"
  fi

  if [[ -z "${actual_dir}" ]]; then
    echo "[dev:sync] GUARDRAIL: no versioned directory found under ${cache_parent}"
    print_claude_repair_hint
    return 0
  fi

  local dir_version
  dir_version="$(basename "${actual_dir}")"
  node "${CLAUDE_PLUGIN_STATE_TOOL}" repair-plugin-record "${plugins_json}" "${actual_dir}" "${dir_version}"

  echo "[dev:sync] GUARDRAIL: FIXED installPath: ${install_path} → ${actual_dir}"
  echo "[dev:sync] GUARDRAIL: plugin discovery restored"
  return 0
}

require_repo_invariants

sync_file_client "Codex" "${HOME}/.agents/skills/ha-nova" "codex"
sync_file_client "OpenCode" "${HOME}/.config/opencode/skills/ha-nova" "opencode"
sync_gemini
sync_claude
if [[ "${file_clients_synced}" -eq 0 ]]; then
  sync_shared_tools
fi
verify_plugin_integrity

if [[ ${#synced[@]} -eq 0 ]]; then
  echo "[dev:sync] Nothing to sync — no clients detected."
else
  echo "[dev:sync] Done: ${synced[*]}"
fi
