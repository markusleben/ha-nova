#!/usr/bin/env bash
# Self-update for HA NOVA skills — all supported clients.
# Installed to ~/.config/ha-nova/update during onboarding.
# Runnable from any directory.
#
# Client archetypes:
#   native    — client has own plugin system (delegate update)
#   symlink   — git pull in source clone (symlink auto-resolves)
#   flat-copy — git pull + re-copy files
set -euo pipefail

# ─── Helpers ──────────────────────────────────────────────────────────

log()  { echo "[ha-nova:update] $*"; }
warn() { echo "[ha-nova:update] WARNING: $*" >&2; }
die()  { echo "[ha-nova:update] ERROR: $*" >&2; exit 1; }

extract_json_string() {
  local key="$1" json="$2"
  echo "$json" | grep -o "\"${key}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" 2>/dev/null | head -1 \
    | sed 's/.*"\([^"]*\)"$/\1/'
}

# Portable readlink -f (stock macOS lacks it)
resolve_link() {
  local path="$1"
  while [[ -L "$path" ]]; do
    local dir; dir="$(cd -P "$(dirname "$path")" && pwd)"
    path="$(readlink "$path")"
    [[ "$path" != /* ]] && path="${dir}/${path}"
  done
  cd -P "$(dirname "$path")" && echo "$(pwd)/$(basename "$path")"
}

CONFIG_DIR="${HOME}/.config/ha-nova"
DETECTED_CLIENTS=()
UPDATED_CLIENTS=()
CLONE_ROOT=""
NEW_VERSION=""
CURRENT_VERSION=""

rewrite_flat_markdown() {
  local skill_name="$1" source_dir="$2" src="$3" dest="$4"
  local content

  content="$(cat "${src}")"

  for companion in "${source_dir}"/*.md; do
    local companion_name
    companion_name="$(basename "${companion}")"
    [[ "${companion_name}" == "SKILL.md" ]] && continue
    local same_skill_ref same_skill_local
    printf -v same_skill_ref '`skills/%s/%s`' "${skill_name}" "${companion_name}"
    printf -v same_skill_local '`%s`' "${companion_name}"
    content="${content//${same_skill_ref}/${same_skill_local}}"
  done

  content="$(
    printf '%s' "${content}" | HA_NOVA_ROOT="${CLONE_ROOT}" perl -0pe '
      s{`docs/reference/([^`]+)`}{sprintf("`%s/docs/reference/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
      s{`skills/([^`]+)`}{sprintf("`%s/skills/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
    '
  )"

  printf '%s' "${content}" > "${dest}"
}

copy_flat_skill_markdown() {
  local skill_name="$1" source_dir="$2" dest_dir="$3"

  mkdir -p "${dest_dir}"
  find "${dest_dir}" -maxdepth 1 -type f -name '*.md' -exec rm -f {} +

  if [[ -f "${source_dir}/SKILL.md" ]]; then
    rewrite_flat_markdown "${skill_name}" "${source_dir}" "${source_dir}/SKILL.md" "${dest_dir}/SKILL.md"
  fi

  for companion in "${source_dir}"/*.md; do
    local companion_name
    companion_name="$(basename "${companion}")"
    [[ "${companion_name}" == "SKILL.md" ]] && continue
    rewrite_flat_markdown "${skill_name}" "${source_dir}" "${companion}" "${dest_dir}/${companion_name}"
  done
}

# ─── Phase 1: Detect installed clients ────────────────────────────────

detect_clients() {
  # Claude Code — native plugin system
  local pj="${HOME}/.claude/plugins/installed_plugins.json"
  if [[ -f "$pj" ]] && grep -q '"ha-nova@ha-nova"' "$pj" 2>/dev/null; then
    DETECTED_CLIENTS+=("claude")
  fi

  # Codex — symlink
  if [[ -L "${HOME}/.agents/skills/ha-nova" ]]; then
    DETECTED_CLIENTS+=("codex")
  fi

  # OpenCode — symlink
  if [[ -L "${HOME}/.config/opencode/skills/ha-nova" ]]; then
    DETECTED_CLIENTS+=("opencode")
  fi

  # Gemini — flat copies (marker: read sub-skill exists).
  # Keep the legacy shared-agents root as a migration fallback so older
  # installs can still self-update into ~/.gemini/skills.
  if [[ -f "${HOME}/.gemini/skills/ha-nova-read/SKILL.md" ]] || \
     [[ -f "${HOME}/.agents/skills/ha-nova-read/SKILL.md" ]]; then
    DETECTED_CLIENTS+=("gemini")
  fi

  if [[ ${#DETECTED_CLIENTS[@]} -eq 0 ]]; then
    die "No HA NOVA client installations detected. Run setup first."
  fi

  log "Detected clients: ${DETECTED_CLIENTS[*]}"
}

# ─── Phase 2: Find source git clone ──────────────────────────────────
# Priority: symlink targets first (user's actual checkout),
# then Claude Code plugin cache as last resort.

find_source_clone() {
  # 1. Codex symlink → parent of skills dir
  local link="${HOME}/.agents/skills/ha-nova"
  if [[ -L "$link" ]]; then
    local target; target=$(resolve_link "$link" 2>/dev/null || true)
    local repo; repo="$(dirname "$target")"
    if [[ -d "${repo}/.git" ]]; then
      echo "$repo"; return 0
    fi
  fi

  # 2. OpenCode symlink → parent of skills dir
  link="${HOME}/.config/opencode/skills/ha-nova"
  if [[ -L "$link" ]]; then
    local target; target=$(resolve_link "$link" 2>/dev/null || true)
    local repo; repo="$(dirname "$target")"
    if [[ -d "${repo}/.git" ]]; then
      echo "$repo"; return 0
    fi
  fi

  # 3. Installer-managed local clone
  local repo="${HOME}/.local/share/ha-nova"
  if [[ -d "${repo}/.git" ]]; then
    echo "$repo"; return 0
  fi

  # 4. Claude Code plugin cache (git clone root)
  local pj="${HOME}/.claude/plugins/installed_plugins.json"
  if [[ -f "$pj" ]]; then
    local install_path
    install_path=$(
      sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
        "$pj" | head -1
    )
    install_path="${install_path/#\~/$HOME}"
    if [[ -n "$install_path" ]]; then
      # installPath = cache/ha-nova/ha-nova/{version}/ → clone root = 2 levels up
      local clone; clone="$(dirname "$(dirname "$install_path")")"
      if [[ -d "${clone}/.git" ]]; then
        echo "$clone"; return 0
      fi
    fi
  fi

  return 1
}

# ─── Phase 3: Update source (git pull) ───────────────────────────────

update_source() {
  CLONE_ROOT="$1"

  CURRENT_VERSION="unknown"
  if [[ -f "${CLONE_ROOT}/version.json" ]]; then
    CURRENT_VERSION=$(extract_json_string "skill_version" "$(cat "${CLONE_ROOT}/version.json")")
  fi

  echo "Checking for updates..."
  echo "Current version: v${CURRENT_VERSION}"

  git -C "$CLONE_ROOT" fetch origin main --quiet 2>/dev/null || \
    die "Could not reach GitHub. Check your internet connection."

  local local_sha remote_sha
  local_sha=$(git -C "$CLONE_ROOT" rev-parse HEAD 2>/dev/null)
  remote_sha=$(git -C "$CLONE_ROOT" rev-parse origin/main 2>/dev/null)

  if [[ "$local_sha" == "$remote_sha" ]]; then
    echo "Already up to date (v${CURRENT_VERSION})."
    return 1  # signal: no changes pulled
  fi

  git -C "$CLONE_ROOT" pull --ff-only origin main --quiet 2>/dev/null || \
    die "git pull failed (local changes or diverged branch). Try: cd ${CLONE_ROOT} && git status"

  NEW_VERSION="unknown"
  if [[ -f "${CLONE_ROOT}/version.json" ]]; then
    NEW_VERSION=$(extract_json_string "skill_version" "$(cat "${CLONE_ROOT}/version.json")")
  fi

  echo "New version: v${NEW_VERSION}"
}

# ─── Phase 4: Per-client update functions ─────────────────────────────

# Archetype: native — delegate to client's own plugin manager
update_claude() {
  if command -v claude &>/dev/null; then
    log "Updating Claude Code plugin..."
    if claude plugin update ha-nova@ha-nova 2>/dev/null; then
      cleanup_claude_cache_orphans
      UPDATED_CLIENTS+=("Claude Code")
    else
      warn "claude plugin update failed — try: claude plugin install ha-nova@ha-nova"
    fi
  else
    warn "claude CLI not available — Claude Code plugin will update on next 'claude plugin update'"
  fi
}

# Remove renamed/deleted skills from Claude plugin cache.
# claude plugin update pulls new files but doesn't delete orphans.
cleanup_claude_cache_orphans() {
  local pj="${HOME}/.claude/plugins/installed_plugins.json"
  [[ -f "$pj" ]] || return 0

  local install_path
  install_path=$(
    sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
      "$pj" | head -1
  )
  install_path="${install_path/#\~/$HOME}"
  [[ -d "${install_path}/skills" ]] || return 0

  # Resolve authoritative source for valid skill names.
  # Prefer CLONE_ROOT (fresh git pull). Fallback: the plugin cache's own
  # git clone (dirname of install_path = cache/ha-nova/ha-nova/), which
  # claude plugin update just refreshed. Never use find_source_clone()
  # here — it could pick a stale ~/.local/share/ha-nova clone.
  local source_skills=""
  if [[ -n "${CLONE_ROOT}" && -d "${CLONE_ROOT}/skills" ]]; then
    source_skills="${CLONE_ROOT}/skills"
  else
    local cache_clone
    cache_clone="$(dirname "$install_path")"
    [[ -d "${cache_clone}/skills" ]] && source_skills="${cache_clone}/skills"
  fi
  [[ -d "$source_skills" ]] || return 0

  # Build valid list (portable — no associative arrays, works on macOS Bash 3.2)
  local valid_skills="ha-nova"
  for skill_dir in "${source_skills}"/*/SKILL.md; do
    valid_skills="${valid_skills}"$'\n'"$(basename "$(dirname "$skill_dir")")"
  done

  for existing in "${install_path}/skills"/ha-nova*/; do
    [[ ! -d "$existing" ]] && continue
    local name; name="$(basename "$existing")"
    if ! printf '%s\n' "$valid_skills" | grep -qx "$name"; then
      rm -rf "$existing"
      log "Removed orphaned Claude cache skill: ${name}"
    fi
  done
}

# Archetype: symlink — git pull already updated the target, just verify
update_symlink_client() {
  local name="$1" link_path="$2"
  local target; target=$(resolve_link "$link_path" 2>/dev/null || true)
  if [[ -d "$target" ]]; then
    UPDATED_CLIENTS+=("$name")
  else
    warn "$name symlink broken: $link_path → $target"
  fi
}

# Archetype: flat-copy — re-copy from updated source
update_gemini() {
  local source_skills="${CLONE_ROOT}/skills"
  local skills_dir="${HOME}/.gemini/skills"

  # Auto-cleanup: remove any ha-nova* dir that doesn't match a current source skill.
  # Portable — no associative arrays, works on macOS Bash 3.2.
  local valid_skills="ha-nova"
  for skill_dir in "${source_skills}"/*/SKILL.md; do
    valid_skills="${valid_skills}"$'\n'"$(basename "$(dirname "$skill_dir")")"
  done
  for existing in "${skills_dir}"/ha-nova*/; do
    [[ ! -d "$existing" ]] && continue
    local name; name="$(basename "$existing")"
    if ! printf '%s\n' "$valid_skills" | grep -qx "$name"; then
      rm -rf "$existing"
      log "Removed orphaned Gemini skill: ${name}"
    fi
  done

  # Context skill
  local context_dir="${skills_dir}/ha-nova"
  copy_flat_skill_markdown "ha-nova" "${source_skills}/ha-nova" "${context_dir}"

  # Sub-skills + companion docs
  for skill_md in "${source_skills}"/*/SKILL.md; do
    local skill_name; skill_name=$(basename "$(dirname "$skill_md")")
    [[ "$skill_name" == "ha-nova" ]] && continue

    local dest_dir="${skills_dir}/${skill_name}"
    copy_flat_skill_markdown "${skill_name}" "${source_skills}/${skill_name}" "${dest_dir}"
  done

  UPDATED_CLIENTS+=("Gemini")
}

# ─── Phase 5: Update shared tools ────────────────────────────────────

update_shared_tools() {
  local src="$1"
  mkdir -p "$CONFIG_DIR"

  # Download updated relay binary
  local os_name arch_name version download_url
  os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch_name="$(uname -m)"
  case "$arch_name" in
    x86_64)        arch_name="amd64" ;;
    aarch64|arm64) arch_name="arm64" ;;
  esac
  version="$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${src}/version.json" | head -1)"
  if [[ -z "$version" ]]; then
    log "Warning: could not determine version from version.json — relay CLI not updated."
  else
    # Go binary releases are tagged alongside skill_version (same GitHub release).
    download_url="https://github.com/markusleben/ha-nova/releases/download/v${version}/relay-${os_name}-${arch_name}"
    if curl -fsSL "${download_url}" -o "${CONFIG_DIR}/relay"; then
      chmod 755 "${CONFIG_DIR}/relay"
    else
      log "Warning: could not download updated relay binary."
    fi
  fi

  # Atomic self-update: copy to temp then move (script may be running from CONFIG_DIR/update)
  if [[ -f "${src}/scripts/update.sh" ]]; then
    local tmp; tmp=$(mktemp "${CONFIG_DIR}/update.XXXXXX")
    cp "${src}/scripts/update.sh" "$tmp"
    chmod 755 "$tmp"
    mv -f "$tmp" "${CONFIG_DIR}/update"
  fi
  [[ -f "${src}/scripts/version-check.sh" ]] && cp "${src}/scripts/version-check.sh" "${CONFIG_DIR}/version-check" && chmod 755 "${CONFIG_DIR}/version-check"
  [[ -f "${src}/version.json" ]]            && cp "${src}/version.json" "${CONFIG_DIR}/version.json"

  # Clear update cache so next session shows fresh state
  rm -f "${HOME}/.cache/ha-nova/latest-version.json"
}

# ─── Main ─────────────────────────────────────────────────────────────

main() {
  detect_clients

  # Determine if we need a git pull (anything besides native-only Claude)
  local needs_pull=false
  for client in "${DETECTED_CLIENTS[@]}"; do
    [[ "$client" != "claude" ]] && needs_pull=true
  done
  # Claude without CLI also needs pull (fallback path)
  if [[ " ${DETECTED_CLIENTS[*]} " == *" claude "* ]] && ! command -v claude &>/dev/null; then
    needs_pull=true
  fi

  local clone_root=""
  local source_updated=false
  if $needs_pull; then
    clone_root=$(find_source_clone) || die "No HA NOVA git clone found. Re-install with the setup script."
    if update_source "$clone_root"; then
      source_updated=true
    fi
  fi

  # Per-client updates
  for client in "${DETECTED_CLIENTS[@]}"; do
    case "$client" in
      claude)   update_claude ;;
      codex)    update_symlink_client "Codex" "${HOME}/.agents/skills/ha-nova" ;;
      opencode) update_symlink_client "OpenCode" "${HOME}/.config/opencode/skills/ha-nova" ;;
      gemini)   update_gemini ;;
    esac
  done

  # Shared tools (need a source — either from pull or Claude plugin cache)
  if [[ -z "$clone_root" ]]; then
    clone_root=$(find_source_clone) || true
  fi
  if [[ -n "$clone_root" ]]; then
    update_shared_tools "$clone_root"
  fi

  # Report
  echo ""
  if ! $source_updated && [[ ${#UPDATED_CLIENTS[@]} -eq 0 ]]; then
    echo "Everything is up to date."
  else
    if [[ -n "$NEW_VERSION" && "$NEW_VERSION" != "unknown" ]]; then
      echo "Updated: v${CURRENT_VERSION} → v${NEW_VERSION}"
    fi
    if [[ ${#UPDATED_CLIENTS[@]} -gt 0 ]]; then
      echo "Clients updated: ${UPDATED_CLIENTS[*]}"
    fi
    echo "Please start a new session to use the updated skills."
  fi
}

main "$@"
