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
CURRENT_PLATFORM_ID="$(detect_platform_id)"

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

sync_hermes() {
  if [[ "${CURRENT_PLATFORM_ID}" == "windows" ]]; then
    echo "[dev:sync] Hermes Agent: native Windows sync not supported — use the WSL2/Linux HA NOVA install instead"
    return
  fi

  sync_file_client "Hermes Agent" "${HOME}/.hermes/skills/ha-nova" "hermes"
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

  # Claude re-stages the plugin FROM the marketplace source on restart — update
  # it too, so a restart keeps dev skills instead of clobbering them.
  # Dev marketplace root nests under `ha-nova/`; a release snapshot has `skills/`
  # directly. Handle both, and never write outside HOME (keeps test sandboxes safe).
  local mkt_src mkt_parent
  mkt_src="$(claude_marketplace_source_dir)"
  mkt_parent=""
  if [[ -n "${mkt_src}" && "${mkt_src}" == "${HOME}"/* ]]; then
    if [[ -d "${mkt_src}/ha-nova/skills" ]]; then
      mkt_parent="${mkt_src}/ha-nova"
    elif [[ -d "${mkt_src}/skills" ]]; then
      mkt_parent="${mkt_src}"
    fi
  fi
  # If the marketplace source resolves into the repo (dev-mode symlink), it is
  # already always-live — rsyncing/stamping would write back into the repo. Skip.
  if [[ -n "${mkt_parent}" ]]; then
    local mkt_real
    mkt_real="$(cd "${mkt_parent}" 2>/dev/null && pwd -P || true)"
    if [[ -z "${mkt_real}" || "${mkt_real}" == "${REPO_ROOT}" || "${mkt_real}" == "${REPO_ROOT}/"* ]]; then
      echo "[dev:sync] Claude marketplace source is the live repo — already current, skip"
      mkt_parent=""
    fi
  fi
  if [[ -n "${mkt_parent}" ]]; then
    rsync -a --delete "${REPO_ROOT}/skills/" "${mkt_parent}/skills/"
    cp "${REPO_ROOT}/version.json" "${mkt_parent}/version.json" 2>/dev/null || true
    echo "[dev:sync] Claude marketplace source synced → ${mkt_parent}"
  fi

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
    # The dev build channel (ha-nova version self-report) is stamped onto the
    # runtime binary by sync_cli_runtime — the single owner of the stamp. This
    # shared-tools build stays plain (and, in a repo-dev install, relay_dst is a
    # wrapper script this rarely-taken branch would otherwise clobber).
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

# ldflags that stamp a local dev build so `ha-nova version` self-reports DEV.
# Released builds never pass these, so they print only the bare version — the
# published install stays untouched. The signal lives in the shared CLI, not in
# skill files, so it works for every client (symlinked Codex/OpenCode or copied
# Claude/Gemini) and can never pollute the committed skill source.
dev_build_ldflags() {
  local sha stamp
  sha="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo local)"
  stamp="$(date '+%Y-%m-%dT%H:%M' 2>/dev/null || echo unknown)"
  printf -- '-X main.BuildChannel=dev -X main.BuildStamp=%s-%s' "${stamp}" "${sha}"
}

# The directory Claude stages the ha-nova plugin FROM (its marketplace source).
# Updating it keeps dev skills alive across a Claude restart instead of clobbered.
claude_marketplace_source_dir() {
  local km="${HOME}/.claude/plugins/known_marketplaces.json"
  [[ -f "${km}" ]] || return 0
  command -v node >/dev/null 2>&1 || return 0
  node -e '
    try {
      const fs = require("fs");
      const j = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
      const m = j["ha-nova"];
      const p = m ? ((m.source && typeof m.source === "object" && m.source.path) || m.installLocation || "") : "";
      if (p) process.stdout.write(p);
    } catch (e) {}
  ' "${km}" 2>/dev/null || true
}

# True when $1 resolves to a path inside the repo working tree. The repo ships a
# tracked helper shim (scripts/onboarding/bin/ha-nova); if a dev puts that dir on
# PATH, `command -v ha-nova` resolves to it. Building the Go runtime onto it would
# clobber a tracked file, so dev_runtime_target rejects any in-repo candidate.
path_within_repo() {
  local candidate="$1" repo_real cand_dir cand_real
  repo_real="$(cd "${REPO_ROOT}" 2>/dev/null && pwd -P)" || return 1
  cand_dir="$(cd "$(dirname "${candidate}")" 2>/dev/null && pwd -P)" || return 1
  cand_real="${cand_dir}/$(basename "${candidate}")"
  [[ "${cand_real}" == "${repo_real}" || "${cand_real}" == "${repo_real}/"* ]]
}

# Resolve the real binary that the installed `ha-nova` actually runs, so the CLI
# rebuild lands on the path every client's skill calls — not a side path.
dev_runtime_target() {
  local p resolved
  p="$(command -v ha-nova 2>/dev/null || true)"
  if [[ -n "${p}" ]]; then
    if [[ -L "${p}" ]]; then
      resolved="$(readlink "${p}")"
      [[ "${resolved}" == /* ]] || resolved="$(cd "$(dirname "${p}")" && pwd)/${resolved}"
    else
      resolved="${p}"
    fi
    # Never hand back an in-repo path: a tracked helper shim on PATH would be
    # overwritten by `go build -o`. Fall through to the real install root so the
    # rebuild lands on the runtime clients actually call.
    if ! path_within_repo "${resolved}"; then
      printf '%s\n' "${resolved}"
      return 0
    fi
  fi
  if [[ -e "${HOME}/.local/share/ha-nova/ha-nova" ]]; then
    printf '%s\n' "${HOME}/.local/share/ha-nova/ha-nova"
    return 0
  fi
  return 1
}

# Rebuild the local Go CLI onto the runtime binary the installed clients call,
# so skills and CLI stay in lockstep for live dev testing. This is a local dev
# build (unstamped version); restore the released CLI with `ha-nova update`.
sync_cli_runtime() {
  if ! command -v go >/dev/null 2>&1 || [[ ! -f "${REPO_ROOT}/cli/main.go" ]]; then
    echo "[dev:sync] CLI: Go toolchain or cli/ missing — CLI not rebuilt (new ha-nova subcommands stay unavailable)"
    return
  fi

  local target
  if ! target="$(dev_runtime_target)"; then
    echo "[dev:sync] CLI: no installed ha-nova runtime found — run 'ha-nova setup' first, then re-sync"
    return
  fi

  # Only build onto a runtime under the current HOME — never a foreign install or
  # a test sandbox whose mocked PATH resolves ha-nova to the developer's real one.
  case "${target}" in
    "${HOME}"/*) ;;
    *)
      echo "[dev:sync] CLI: runtime ${target} is outside HOME — skipping CLI build"
      return
      ;;
  esac

  if (cd "${REPO_ROOT}/cli" && go build -ldflags "$(dev_build_ldflags)" -o "${target}" .); then
    chmod 755 "${target}" 2>/dev/null || true
    echo "[dev:sync] CLI: built local Go source → ${target}"
    echo "[dev:sync] CLI: local dev build active — 'ha-nova version' now reports DEV; restore the release with 'ha-nova update'"
    synced+=("CLI")
  else
    echo "[dev:sync] CLI: go build failed — fix the error above, then re-sync" >&2
  fi
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
sync_hermes
sync_claude
sync_cli_runtime
if [[ "${file_clients_synced}" -eq 0 ]]; then
  sync_shared_tools
fi
verify_plugin_integrity

if [[ ${#synced[@]} -eq 0 ]]; then
  echo "[dev:sync] Nothing to sync — no clients detected."
else
  echo "[dev:sync] Done: ${synced[*]}"
fi
