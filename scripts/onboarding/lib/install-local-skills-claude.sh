#!/usr/bin/env bash

claude_plugin_state_runtime_ready() {
  command -v node >/dev/null 2>&1 && node -e '' >/dev/null 2>&1
}

claude_plugins_json_path() {
  printf '%s/.claude/plugins/installed_plugins.json' "${HOME}"
}

claude_known_marketplaces_path() {
  printf '%s/.claude/plugins/known_marketplaces.json' "${HOME}"
}

claude_plugin_record_present() {
  local plugins_json
  plugins_json="$(claude_plugins_json_path)"
  [[ -f "${plugins_json}" ]] || return 1
  grep -Fq 'ha-nova@ha-nova' "${plugins_json}"
}

claude_marketplace_record_present() {
  local known_marketplaces
  known_marketplaces="$(claude_known_marketplaces_path)"
  [[ -f "${known_marketplaces}" ]] || return 1
  grep -Fq '"ha-nova"' "${known_marketplaces}"
}

claude_plugin_state_helper_required() {
  claude_plugin_record_present || claude_marketplace_record_present
}

require_claude_plugin_state_tool() {
  local prefix="${1:-[claude]}"
  local tool_path
  tool_path="$(claude_plugin_state_tool)"
  claude_plugin_state_runtime_ready || {
    echo "${prefix} Node.js not found in PATH (required for local plugin state helper)" >&2
    return 1
  }
  [[ -f "${tool_path}" ]] || {
    echo "${prefix} Missing plugin state tool: ${tool_path}" >&2
    return 1
  }
}

claude_plugin_state_tool() {
  printf '%s/scripts/dev/claude-plugin-state.mjs' "${REPO_ROOT}"
}

claude_plugin_state_lines() {
  local plugins_json="${1:-$(claude_plugins_json_path)}"
  [[ -f "${plugins_json}" ]] || return 1
  node "$(claude_plugin_state_tool)" inspect-installed-plugin "${plugins_json}"
}

claude_plugin_state_value() {
  local field="$1"
  local plugins_json="${2:-${HOME}/.claude/plugins/installed_plugins.json}"
  local value=""

  while IFS='=' read -r key current; do
    if [[ "${key}" == "${field}" ]]; then
      value="${current}"
      break
    fi
  done < <(claude_plugin_state_lines "${plugins_json}" || true)

  printf '%s' "${value}"
}

claude_marketplace_root() {
  printf '%s' "${HOME}/.config/ha-nova/claude-marketplace"
}

claude_plugin_cache_root() {
  printf '%s' "${HOME}/.claude/plugins/cache/ha-nova"
}

stage_claude_marketplace_plugin_root() {
  local marketplace_root
  marketplace_root="$(claude_marketplace_root)"
  local plugin_root="${marketplace_root}/ha-nova"

  mkdir -p "${marketplace_root}"
  rm -rf "${plugin_root}"

  if should_copy_file_client_install; then
    copy_tree_install "${REPO_ROOT}" "${plugin_root}"
    printf '%s' "${plugin_root}"
    return 0
  fi

  if ln -sfn "${REPO_ROOT}" "${plugin_root}"; then
    printf '%s' "${plugin_root}"
    return 0
  fi

  copy_tree_install "${REPO_ROOT}" "${plugin_root}"
  printf '%s' "${plugin_root}"
}

write_claude_marketplace() {
  local marketplace_root
  marketplace_root="$(claude_marketplace_root)"
  local manifest_dir="${marketplace_root}/.claude-plugin"
  local plugin_root
  plugin_root="$(stage_claude_marketplace_plugin_root)"
  local plugin_version
  plugin_version="$(repo_claude_plugin_version)"

  mkdir -p "${manifest_dir}"
  cat > "${manifest_dir}/marketplace.json" <<EOF
{
  "name": "ha-nova",
  "owner": {
    "name": "Markus Leben"
  },
  "plugins": [
    {
      "name": "ha-nova",
      "source": "./ha-nova",
      "version": "${plugin_version}",
      "description": "AI-powered Home Assistant control through LLM skills and a local relay"
    }
  ]
}
EOF

  printf '%s' "${marketplace_root}"
}

claude_plugin_installed() {
  [[ "$(claude_plugin_state_value "installed")" == "1" ]]
}

read_claude_marketplace_source() {
  local known_marketplaces
  known_marketplaces="$(claude_known_marketplaces_path)"
  [[ -f "${known_marketplaces}" ]] || return 0
  node "$(claude_plugin_state_tool)" read-known-marketplace-source "${known_marketplaces}"
}

remove_claude_plugin_record() {
  local plugins_json
  plugins_json="$(claude_plugins_json_path)"
  [[ -f "${plugins_json}" ]] || return 0
  claude_plugin_state_runtime_ready || return 0
  node "$(claude_plugin_state_tool)" remove-plugin-record "${plugins_json}"
}

reset_local_claude_plugin_state() {
  if claude_plugin_installed; then
    local output=""
    if ! output="$(claude plugin remove ha-nova@ha-nova 2>&1)"; then
      if ! printf '%s' "${output}" | grep -Eiq 'not found|not installed'; then
        echo "[claude] Plugin remove failed: ha-nova@ha-nova" >&2
        return 1
      fi
    fi
  fi

  remove_claude_plugin_record
  rm -rf "$(claude_plugin_cache_root)"
}

restore_local_claude_state() {
  local previous_source="$1"
  local previous_plugin_installed="$2"

  claude plugin marketplace remove ha-nova >/dev/null 2>&1 || true
  if [[ "${previous_plugin_installed}" == "1" && -z "${previous_source}" ]]; then
    return 1
  fi
  if [[ -n "${previous_source}" ]]; then
    claude plugin marketplace add "${previous_source}" >/dev/null 2>&1 || return 1
  fi

  if [[ "${previous_plugin_installed}" == "1" ]]; then
    claude plugin install ha-nova@ha-nova >/dev/null 2>&1 || return 1
  else
    claude plugin remove ha-nova@ha-nova >/dev/null 2>&1 || true
    remove_claude_plugin_record
    rm -rf "$(claude_plugin_cache_root)"
  fi

  return 0
}

fail_after_claude_restore_attempt() {
  local message="$1"
  local previous_source="$2"
  local previous_plugin_installed="$3"

  if ! restore_local_claude_state "${previous_source}" "${previous_plugin_installed}"; then
    die "${message}; previous Claude state could not be restored automatically"
  fi

  die "${message}"
}

install_claude_plugin() {
  local previous_source=""
  local previous_plugin_installed="0"

  if claude_plugin_state_helper_required; then
    require_claude_plugin_state_tool "[claude]" || exit 1
    previous_source="$(read_claude_marketplace_source)"
    if claude_plugin_installed; then
      previous_plugin_installed="1"
    fi
  fi

  local marketplace_root
  marketplace_root="$(write_claude_marketplace)"

  claude plugin marketplace remove ha-nova >/dev/null 2>&1 || true

  if claude plugin marketplace add "${marketplace_root}" 2>/dev/null; then
    log "[claude] Marketplace registered: ${marketplace_root}"
  else
    fail_after_claude_restore_attempt "[claude] Marketplace registration failed: ${marketplace_root}" "${previous_source}" "${previous_plugin_installed}"
  fi

  if ! reset_local_claude_plugin_state; then
    fail_after_claude_restore_attempt "[claude] Plugin reset failed: ha-nova@ha-nova" "${previous_source}" "${previous_plugin_installed}"
  fi

  if claude plugin install ha-nova@ha-nova 2>/dev/null; then
    log "[claude] Plugin installed: ha-nova@ha-nova"
  else
    fail_after_claude_restore_attempt "[claude] Plugin install failed: ha-nova@ha-nova" "${previous_source}" "${previous_plugin_installed}"
  fi
}
