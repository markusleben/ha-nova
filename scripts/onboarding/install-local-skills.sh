#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[install-local-skills] $*"
}

die() {
  echo "[install-local-skills] $*" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/onboarding/install-local-skills.sh codex
  bash scripts/onboarding/install-local-skills.sh claude
  bash scripts/onboarding/install-local-skills.sh opencode
  bash scripts/onboarding/install-local-skills.sh gemini
  bash scripts/onboarding/install-local-skills.sh all

Targets:
  codex    -> link/copy ~/.agents/skills/ha-nova -> repo skills
  claude   -> skipped (use Claude Code plugin system)
  opencode -> link/copy ~/.config/opencode/skills/ha-nova -> repo skills
  gemini   -> flat copy ~/.gemini/skills/ha-nova-*/SKILL.md (+ local companion .md files)
  all      -> install for codex + claude + opencode + gemini
USAGE
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
SOURCE_SKILLS_DIR="${REPO_ROOT}/skills"

detect_platform_id() {
  local platform_source="${HA_NOVA_PLATFORM_OVERRIDE:-$(uname -s)}"

  case "$platform_source" in
    macos|Darwin)
      printf 'macos'
      ;;
    windows|MINGW*|MSYS*|CYGWIN*)
      printf 'windows'
      ;;
    Linux)
      printf 'linux'
      ;;
    *)
      printf '%s' "$platform_source" | tr '[:upper:]' '[:lower:]'
      ;;
  esac
}

CURRENT_PLATFORM_ID="$(detect_platform_id)"

should_copy_file_client_install() {
  [[ "${HA_NOVA_FORCE_COPY_INSTALL:-0}" == "1" || "${CURRENT_PLATFORM_ID}" == "windows" ]]
}

normalize_release_arch() {
  local arch_name="${1:-$(uname -m)}"

  case "$arch_name" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64)
      if [[ "${CURRENT_PLATFORM_ID}" == "windows" ]]; then
        printf 'amd64'
      else
        printf 'arm64'
      fi
      ;;
    i386|i686) printf '386' ;;
    *) printf '%s' "$arch_name" ;;
  esac
}

normalize_release_os() {
  case "${CURRENT_PLATFORM_ID}" in
    macos) printf 'darwin' ;;
    windows) printf 'windows' ;;
    linux) printf 'linux' ;;
    *) printf '%s' "${CURRENT_PLATFORM_ID}" ;;
  esac
}

relay_binary_name() {
  if [[ "${CURRENT_PLATFORM_ID}" == "windows" ]]; then
    printf 'relay.exe'
    return
  fi

  printf 'relay'
}

bundled_relay_path() {
  local relay_name
  relay_name="$(relay_binary_name)"

  for candidate in \
    "${HA_NOVA_BUNDLED_RELAY:-}" \
    "${REPO_ROOT}/bin/${relay_name}" \
    "${REPO_ROOT}/bundle/bin/${relay_name}"
  do
    if [[ -n "$candidate" && -f "$candidate" ]]; then
      printf '%s' "$candidate"
      return 0
    fi
  done

  return 1
}

copy_tree_install() {
  local source_dir="$1"
  local target_dir="$2"

  rm -rf "${target_dir}"
  cp -R "${source_dir}" "${target_dir}"
}

write_repo_cli_wrapper() {
  local target_path="$1"
  local subcommand="$2"
  local extra_args="${3:-}"

  cat > "${target_path}" <<EOF
#!/usr/bin/env bash
set -euo pipefail
exec "${REPO_ROOT}/scripts/onboarding/bin/ha-nova" ${subcommand}${extra_args:+ ${extra_args}} "\$@"
EOF
  chmod 755 "${target_path}"
}

# Legacy flat skill directories to clean up
LEGACY_FLAT_SKILLS=(
  "ha-nova-write"
  "ha-nova-read"
  "ha-nova-helper"
  "ha-nova-entity-discovery"
  "ha-nova-onboarding"
  "ha-nova-service-call"
  "ha-nova-review"
  "ha-nova-guide"
  "ha-nova-fallback"
)

# Sub-skills that get flat-copied for Gemini (auto-discovered from skills/)
GEMINI_SUB_SKILLS=()
for _skill_dir in "${SOURCE_SKILLS_DIR}"/*/SKILL.md; do
  _skill_name="$(basename "$(dirname "$_skill_dir")")"
  [[ "$_skill_name" == "ha-nova" ]] && continue
  GEMINI_SUB_SKILLS+=("$_skill_name")
done
unset _skill_dir _skill_name

rewrite_flat_markdown() {
  local skill_name="$1"
  local source_dir="$2"
  local src="$3"
  local dest="$4"
  local content

  content="$(cat "${src}")"

  # Same-skill companions should stay local after flat copy.
  for companion in "${source_dir}"/*.md; do
    local companion_name
    companion_name="$(basename "${companion}")"
    [[ "${companion_name}" == "SKILL.md" ]] && continue
    local same_skill_ref same_skill_local
    printf -v same_skill_ref '`skills/%s/%s`' "${skill_name}" "${companion_name}"
    printf -v same_skill_local '`%s`' "${companion_name}"
    content="${content//${same_skill_ref}/${same_skill_local}}"
  done

  # Shared docs and cross-skill references resolve back to the source clone.
  content="$(
    printf '%s' "${content}" | HA_NOVA_ROOT="${REPO_ROOT}" perl -0pe '
      s{`docs/reference/([^`]+)`}{sprintf("`%s/docs/reference/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
      s{`skills/([^`]+)`}{sprintf("`%s/skills/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
    '
  )"

  printf '%s' "${content}" > "${dest}"
}

copy_flat_skill_markdown() {
  local skill_name="$1"
  local source_dir="${SOURCE_SKILLS_DIR}/${skill_name}"
  local dest_dir="$2"

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

cleanup_legacy() {
  local user_skills_dir="$1"
  local target="$2"

  # Legacy flat skill directories
  for legacy_skill in "${LEGACY_FLAT_SKILLS[@]}"; do
    local legacy_path="${user_skills_dir}/${legacy_skill}"
    if [[ -e "${legacy_path}" || -L "${legacy_path}" ]]; then
      rm -rf "${legacy_path}"
      log "[${target}] Cleaned up legacy flat skill: ${legacy_path}"
    fi
  done

  # Legacy nested copy (from pre-symlink era)
  local nested_path="${user_skills_dir}/ha-nova"
  if [[ -d "${nested_path}" && ! -L "${nested_path}" ]]; then
    rm -rf "${nested_path}"
    log "[${target}] Cleaned up legacy nested copy: ${nested_path}"
  fi
}

cleanup_legacy_flat_only() {
  local user_skills_dir="$1"
  local target="$2"

  for legacy_skill in "${LEGACY_FLAT_SKILLS[@]}"; do
    local legacy_path="${user_skills_dir}/${legacy_skill}"
    if [[ -e "${legacy_path}" || -L "${legacy_path}" ]]; then
      rm -rf "${legacy_path}"
      log "[${target}] Cleaned up legacy flat skill: ${legacy_path}"
    fi
  done
}

# Migration: remove un-prefixed Gemini dirs left by OLD update.sh that ran
# after the skill-rename (source dirs changed from ha-nova-read/ to read/).
# OLD update.sh copied to ~/.gemini/skills/read/ instead of ha-nova-read/.
# The ha-nova* orphan glob never catches these — explicit cleanup needed.
cleanup_gemini_unprefixed() {
  local skills_dir="$1"

  for skill_dir in "${SOURCE_SKILLS_DIR}"/*/SKILL.md; do
    local src_name
    src_name="$(basename "$(dirname "$skill_dir")")"
    [[ "$src_name" == "ha-nova" ]] && continue
    local bare_dir="${skills_dir}/${src_name}"
    if [[ -d "$bare_dir" && -f "${bare_dir}/SKILL.md" ]] && \
       grep -q 'ha-nova' "${bare_dir}/SKILL.md" 2>/dev/null; then
      rm -rf "$bare_dir"
      log "[gemini] Removed un-prefixed migration artifact: ${src_name}"
    fi
  done
}

# Auto-detect and remove orphaned ha-nova* dirs in Gemini's flat-copy tree.
# Works like rsync --delete: anything in the target that doesn't exist in source gets removed.
cleanup_gemini_orphans() {
  local skills_dir="$1"

  # First: clean up un-prefixed dirs from OLD update.sh transition
  cleanup_gemini_unprefixed "$skills_dir"

  # Build valid list with ha-nova- prefix (Gemini target names).
  # Source dirs are short (read/, write/), Gemini dirs are ha-nova-read/, ha-nova-write/.
  local valid_skills="ha-nova"
  for skill_dir in "${SOURCE_SKILLS_DIR}"/*/SKILL.md; do
    local src_name
    src_name="$(basename "$(dirname "$skill_dir")")"
    if [[ "$src_name" == "ha-nova" ]]; then
      continue  # context skill — already in valid_skills
    fi
    valid_skills="${valid_skills}"$'\n'"ha-nova-${src_name}"
  done

  # Scan target for ha-nova* dirs and remove orphans
  for existing in "${skills_dir}"/ha-nova*/; do
    [[ ! -d "$existing" ]] && continue
    local name
    name="$(basename "$existing")"
    if ! printf '%s\n' "$valid_skills" | grep -qx "$name"; then
      rm -rf "$existing"
      log "[gemini] Removed orphaned skill: ${name}"
    fi
  done
}

install_symlink() {
  local target="$1"
  local user_skills_dir="$2"

  mkdir -p "${user_skills_dir}"
  cleanup_legacy "${user_skills_dir}" "${target}"

  # Remove existing symlink if present
  if [[ -L "${user_skills_dir}/ha-nova" ]]; then
    rm -f "${user_skills_dir}/ha-nova"
  fi

  if should_copy_file_client_install; then
    copy_tree_install "${SOURCE_SKILLS_DIR}" "${user_skills_dir}/ha-nova"
    log "[${target}] Copied: ${user_skills_dir}/ha-nova <- ${SOURCE_SKILLS_DIR}"
    return 0
  fi

  if ln -sfn "${SOURCE_SKILLS_DIR}" "${user_skills_dir}/ha-nova"; then
    log "[${target}] Symlinked: ${user_skills_dir}/ha-nova -> ${SOURCE_SKILLS_DIR}"
    return 0
  fi

  copy_tree_install "${SOURCE_SKILLS_DIR}" "${user_skills_dir}/ha-nova"
  log "[${target}] Symlink unavailable; copied: ${user_skills_dir}/ha-nova <- ${SOURCE_SKILLS_DIR}"
}

install_gemini_flat() {
  local user_skills_dir="${HOME}/.gemini/skills"
  mkdir -p "${user_skills_dir}"

  # Clean up legacy Gemini installs from the shared agents root without
  # touching the current Codex install if one exists there.
  cleanup_legacy_flat_only "${HOME}/.agents/skills" "gemini-legacy"

  # Auto-cleanup: remove any ha-nova* dir that doesn't match a current skill.
  # This catches renamed/deleted skills without needing a manual legacy list.
  cleanup_gemini_orphans "${user_skills_dir}"

  # Context skill as ha-nova/SKILL.md (flat, level 1)
  local context_dir="${user_skills_dir}/ha-nova"
  if [[ -d "${context_dir}" ]]; then
    # Existing directory (legacy copy) — replace with fresh copy
    rm -rf "${context_dir}"
    copy_flat_skill_markdown "ha-nova" "${context_dir}"
    log "[gemini] Installed: ha-nova/SKILL.md (context skill, replaced legacy copy)"
  else
    copy_flat_skill_markdown "ha-nova" "${context_dir}"
    log "[gemini] Installed: ha-nova/SKILL.md (context skill)"
  fi

  # Sub-skills get ha-nova- prefix for Gemini (flat, level 1).
  # Source dirs are short names (read/, write/), target dirs are ha-nova-read/, ha-nova-write/.
  for sub in "${GEMINI_SUB_SKILLS[@]}"; do
    local dest_name="ha-nova-${sub}"
    local dest_dir="${user_skills_dir}/${dest_name}"
    if [[ -f "${SOURCE_SKILLS_DIR}/${sub}/SKILL.md" ]]; then
      copy_flat_skill_markdown "${sub}" "${dest_dir}"
      log "[gemini] Installed: ${dest_name}/SKILL.md"
    fi
  done
}

install_claude_plugin() {
  if ! command -v claude &>/dev/null; then
    log "[claude] Skipped — claude CLI not found"
    return 0
  fi

  # Add marketplace (idempotent — overwrites if already present)
  if claude plugin marketplace add "${REPO_ROOT}" 2>/dev/null; then
    log "[claude] Marketplace registered: ${REPO_ROOT}"
  else
    log "[claude] Warning: could not register marketplace"
    return 0
  fi

  # Install plugin
  if claude plugin install ha-nova@ha-nova 2>/dev/null; then
    log "[claude] Plugin installed: ha-nova@ha-nova"
  else
    log "[claude] Warning: could not install plugin (may already be installed)"
  fi
}

install_target() {
  local target="$1"
  case "$target" in
    codex)
      install_symlink "codex" "${HOME}/.agents/skills"
      ;;
    claude)
      install_claude_plugin
      ;;
    opencode)
      install_symlink "opencode" "${HOME}/.config/opencode/skills"
      ;;
    gemini)
      install_gemini_flat
      ;;
    *)
      die "Unsupported target: ${target}"
      ;;
  esac

  # Repo/dev helper wrappers. These keep legacy local entrypoints working
  # against the repo runtime without depending on release assets.
  local relay_cli_target="${HOME}/.config/ha-nova/relay"
  local relay_binary_target="${relay_cli_target}"
  mkdir -p "${HOME}/.config/ha-nova"
  if [[ "${CURRENT_PLATFORM_ID}" != "windows" ]]; then
    write_repo_cli_wrapper "${relay_cli_target}" "relay"
    log "[${target}] Installed relay wrapper: ${relay_cli_target}"
  else
    relay_binary_target="${HOME}/.config/ha-nova/relay.exe"
    local bundled_relay
    if bundled_relay="$(bundled_relay_path)"; then
      cp "${bundled_relay}" "${relay_binary_target}"
      chmod 755 "${relay_binary_target}"
      log "[${target}] Installed bundled relay CLI: ${relay_binary_target}"
    else
      write_repo_cli_wrapper "${relay_cli_target}" "relay"
      cp "${relay_cli_target}" "${relay_binary_target}"
      log "[${target}] Installed relay wrapper fallback: ${relay_binary_target}"
    fi
  fi

  if [[ "${CURRENT_PLATFORM_ID}" == "windows" && -f "${relay_binary_target}" ]]; then
    cat > "${relay_cli_target}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "${SCRIPT_DIR}/relay.exe" "$@"
EOF
    chmod 755 "${relay_cli_target}"
  fi

  # Version check script + local version.json (for flat-copy installs without git repo)
  if [[ -f "${REPO_ROOT}/scripts/version-check.sh" ]]; then
    write_repo_cli_wrapper "${HOME}/.config/ha-nova/version-check" "check-update" "--quiet"
    cp "${REPO_ROOT}/version.json" "${HOME}/.config/ha-nova/version.json"
    log "[${target}] Installed version-check + version.json"
  fi

  # Self-update wrapper for repo/dev installs.
  if [[ -f "${REPO_ROOT}/scripts/update.sh" ]]; then
    write_repo_cli_wrapper "${HOME}/.config/ha-nova/update" "update"
    log "[${target}] Installed self-update script"
  fi
}

main() {
  local target="${1:-}"

  if [[ -z "$target" ]]; then
    usage
    die "No target specified. Please provide a target explicitly."
  fi

  case "$target" in
    codex|claude|opencode|gemini)
      install_target "$target"
      ;;
    all)
      install_target "codex"
      install_target "gemini"
      install_target "claude"
      install_target "opencode"
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      usage
      die "Unknown target: ${target}"
      ;;
  esac

  log "Done. Restart your client to refresh skill discovery."
}

main "$@"
