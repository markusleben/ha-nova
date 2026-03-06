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
  gemini   -> flat copy ~/.agents/skills/ha-nova-{skill}/SKILL.md
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
  "ha-nova-entity-discovery"
  "ha-nova-onboarding"
  "ha-nova-service-call"
  "ha-nova-review"
)

# Sub-skills that get flat-copied for Gemini
GEMINI_SUB_SKILLS=(
  "read"
  "write"
  "entity-discovery"
  "onboarding"
  "service-call"
  "review"
)

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
  local user_skills_dir="${HOME}/.agents/skills"
  mkdir -p "${user_skills_dir}"

  # Clean up legacy Gemini flat dirs
  for sub in "${GEMINI_SUB_SKILLS[@]}"; do
    local flat_path="${user_skills_dir}/ha-nova-${sub}"
    if [[ -e "${flat_path}" || -L "${flat_path}" ]]; then
      rm -rf "${flat_path}"
    fi
  done

  # Context skill as ha-nova/SKILL.md (flat, level 1)
  # When Codex symlink exists, it already provides the context skill — leave it.
  # Gemini can read through the symlink. If Codex is later uninstalled,
  # re-running `install gemini` will create a standalone copy.
  local context_dir="${user_skills_dir}/ha-nova"
  if [[ -L "${context_dir}" ]]; then
    log "[gemini] Context skill provided by Codex symlink: ${context_dir}"
  elif [[ -d "${context_dir}" ]]; then
    # Existing directory (legacy copy) — replace with fresh copy
    rm -rf "${context_dir}"
    mkdir -p "${context_dir}"
    cp "${SOURCE_SKILLS_DIR}/ha-nova/SKILL.md" "${context_dir}/SKILL.md"
    log "[gemini] Installed: ha-nova/SKILL.md (context skill, replaced legacy copy)"
  else
    mkdir -p "${context_dir}"
    cp "${SOURCE_SKILLS_DIR}/ha-nova/SKILL.md" "${context_dir}/SKILL.md"
    log "[gemini] Installed: ha-nova/SKILL.md (context skill)"
  fi

  # Sub-skills as ha-nova-{skill}/SKILL.md (flat, level 1)
  for sub in "${GEMINI_SUB_SKILLS[@]}"; do
    local dest_dir="${user_skills_dir}/ha-nova-${sub}"
    mkdir -p "${dest_dir}"
    local src="${SOURCE_SKILLS_DIR}/${sub}/SKILL.md"
    if [[ -f "${src}" ]]; then
      # For Gemini copies, resolve docs/reference/ to absolute paths
      sed "s|docs/reference/|${REPO_ROOT}/docs/reference/|g" "${src}" > "${dest_dir}/SKILL.md"
      log "[gemini] Installed: ha-nova-${sub}/SKILL.md"
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
