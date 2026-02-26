#!/usr/bin/env bash
set -euo pipefail

SKILL_NAME="ha-nova"

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
  bash scripts/onboarding/install-local-skills.sh all

Targets:
  codex    -> ~/.agents/skills/ha-nova
  claude   -> ~/.claude/skills/ha-nova
  opencode -> ~/.config/opencode/skills/ha-nova
  all      -> install for codex + claude + opencode
USAGE
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
SOURCE_SKILL_DIR="${REPO_ROOT}/.agents/skills/${SKILL_NAME}"
SOURCE_SKILL_FILE="${SOURCE_SKILL_DIR}/SKILL.md"

[[ -f "${SOURCE_SKILL_FILE}" ]] || die "Missing source skill: ${SOURCE_SKILL_FILE}"

render_skill_file() {
  local output_file="$1"
  awk -v repo_root="${REPO_ROOT}" \
    '{ gsub(/__HA_NOVA_REPO_ROOT__/, repo_root); print }' \
    "${SOURCE_SKILL_FILE}" > "${output_file}"
}

install_target() {
  local target="$1"
  local user_skills_dir

  case "$target" in
    codex) user_skills_dir="${HOME}/.agents/skills" ;;
    claude) user_skills_dir="${HOME}/.claude/skills" ;;
    opencode) user_skills_dir="${HOME}/.config/opencode/skills" ;;
    *) die "Unsupported target: ${target}" ;;
  esac

  local dest_skill_path="${user_skills_dir}/${SKILL_NAME}"
  local dest_skill_file="${dest_skill_path}/SKILL.md"
  local managed_marker="ha-nova-managed-install repo_root: ${REPO_ROOT}"
  mkdir -p "${user_skills_dir}"

  if [[ -L "${dest_skill_path}" ]]; then
    local current_target
    local backup_path
    current_target="$(readlink "${dest_skill_path}" || true)"
    backup_path="${dest_skill_path}.backup.$(date +%Y%m%d%H%M%S)"
    mv "${dest_skill_path}" "${backup_path}"
    log "[${target}] Backed up existing symlink (${current_target}) to: ${backup_path}"
  elif [[ -d "${dest_skill_path}" && -f "${dest_skill_file}" ]] && grep -Fq "${managed_marker}" "${dest_skill_file}"; then
    :
  elif [[ -e "${dest_skill_path}" ]]; then
    local backup_path
    backup_path="${dest_skill_path}.backup.$(date +%Y%m%d%H%M%S)"
    mv "${dest_skill_path}" "${backup_path}"
    log "[${target}] Backed up existing path to: ${backup_path}"
  fi

  mkdir -p "${dest_skill_path}"
  render_skill_file "${dest_skill_file}"
  log "[${target}] Installed skill file: ${dest_skill_file}"
}

main() {
  local target="${1:-codex}"

  case "$target" in
    codex|claude|opencode)
      install_target "$target"
      ;;
    all)
      install_target "codex"
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
