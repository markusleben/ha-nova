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
  codex    -> symlink ~/.agents/skills/ha-nova -> repo skills
  claude   -> skipped (use Claude Code plugin system)
  opencode -> symlink ~/.config/opencode/skills/ha-nova -> repo skills
  gemini   -> flat copy ~/.gemini/skills/ha-nova-*/SKILL.md (+ local companion .md files)
  all      -> install for codex + claude + opencode + gemini
USAGE
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
SOURCE_SKILLS_DIR="${REPO_ROOT}/skills"

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

install_symlink() {
  local target="$1"
  local user_skills_dir="$2"

  mkdir -p "${user_skills_dir}"
  cleanup_legacy "${user_skills_dir}" "${target}"

  # Remove existing symlink if present
  if [[ -L "${user_skills_dir}/ha-nova" ]]; then
    rm -f "${user_skills_dir}/ha-nova"
  fi

  ln -sfn "${SOURCE_SKILLS_DIR}" "${user_skills_dir}/ha-nova"
  log "[${target}] Symlinked: ${user_skills_dir}/ha-nova -> ${SOURCE_SKILLS_DIR}"
}

install_gemini_flat() {
  local user_skills_dir="${HOME}/.gemini/skills"
  mkdir -p "${user_skills_dir}"

  # Clean up legacy Gemini installs from the shared agents root without
  # touching the Codex symlink if one exists there.
  cleanup_legacy "${HOME}/.agents/skills" "gemini-legacy"

  # Clean up legacy Gemini flat dirs
  for sub in "${GEMINI_SUB_SKILLS[@]}"; do
    local flat_path="${user_skills_dir}/ha-nova-${sub}"
    if [[ -e "${flat_path}" || -L "${flat_path}" ]]; then
      rm -rf "${flat_path}"
    fi
  done

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

  # Sub-skills keep their namespaced directory names (flat, level 1).
  for sub in "${GEMINI_SUB_SKILLS[@]}"; do
    local dest_dir="${user_skills_dir}/${sub}"
    if [[ -f "${SOURCE_SKILLS_DIR}/${sub}/SKILL.md" ]]; then
      copy_flat_skill_markdown "${sub}" "${dest_dir}"
      log "[gemini] Installed: ${sub}/SKILL.md"
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
  local relay_cli_source="${REPO_ROOT}/scripts/relay.sh"
  local relay_cli_target="${HOME}/.config/ha-nova/relay"

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

  if [[ -f "${relay_cli_source}" ]]; then
    mkdir -p "${HOME}/.config/ha-nova"
    cp "${relay_cli_source}" "${relay_cli_target}"
    chmod 755 "${relay_cli_target}"
    log "[${target}] Installed relay CLI: ${relay_cli_target}"
  fi

  # Version check script + local version.json (for flat-copy installs without git repo)
  if [[ -f "${REPO_ROOT}/scripts/version-check.sh" ]]; then
    cp "${REPO_ROOT}/scripts/version-check.sh" "${HOME}/.config/ha-nova/version-check"
    chmod 755 "${HOME}/.config/ha-nova/version-check"
    cp "${REPO_ROOT}/version.json" "${HOME}/.config/ha-nova/version.json"
    log "[${target}] Installed version-check + version.json"
  fi

  # Self-update script (allows agent-driven updates without repo checkout)
  if [[ -f "${REPO_ROOT}/scripts/update.sh" ]]; then
    cp "${REPO_ROOT}/scripts/update.sh" "${HOME}/.config/ha-nova/update"
    chmod 755 "${HOME}/.config/ha-nova/update"
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
