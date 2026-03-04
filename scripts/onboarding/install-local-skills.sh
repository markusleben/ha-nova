#!/usr/bin/env bash
set -euo pipefail

SKILL_NAMES=(
  "ha-nova"
  "ha-nova-write"
  "ha-nova-read"
  "ha-nova-entity-discovery"
  "ha-nova-onboarding"
  "ha-nova-service-call"
)

LEGACY_SKILL_NAMES=(
  "ha-nova-automation-create"
  "ha-nova-automation-update"
  "ha-nova-automation-delete"
  "ha-nova-script-create"
  "ha-nova-script-update"
  "ha-nova-script-delete"
  "ha-nova-resolve-targets"
  "ha-nova-onboarding-diagnostics"
)

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
  codex    -> ~/.agents/skills/{ha-nova,ha-nova-write,ha-nova-read,ha-nova-entity-discovery,ha-nova-onboarding,ha-nova-service-call}
  claude   -> ~/.claude/skills/{ha-nova,ha-nova-write,ha-nova-read,ha-nova-entity-discovery,ha-nova-onboarding,ha-nova-service-call}
  opencode -> ~/.config/opencode/skills/{ha-nova,ha-nova-write,ha-nova-read,ha-nova-entity-discovery,ha-nova-onboarding,ha-nova-service-call}
  all      -> install for codex + claude + opencode
USAGE
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

render_skill_file() {
  local source_skill_file="$1"
  local output_file="$2"
  awk -v repo_root="${REPO_ROOT}" \
    '{ gsub(/__HA_NOVA_REPO_ROOT__/, repo_root); print }' \
    "${source_skill_file}" > "${output_file}"
}

install_one_skill_for_target() {
  local target="$1"
  local user_skills_dir="$2"
  local skill_name="$3"
  local source_skill_dir="${REPO_ROOT}/.agents/skills/${skill_name}"
  local source_skill_file="${source_skill_dir}/SKILL.md"
  local dest_skill_path="${user_skills_dir}/${skill_name}"
  local dest_skill_file="${dest_skill_path}/SKILL.md"
  local managed_marker="ha-nova-managed-install repo_root: ${REPO_ROOT}"

  [[ -f "${source_skill_file}" ]] || die "Missing source skill: ${source_skill_file}"

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
  render_skill_file "${source_skill_file}" "${dest_skill_file}"
  log "[${target}] Installed skill file: ${dest_skill_file}"
}

archive_legacy_skill() {
  local target="$1"
  local legacy_path="$2"
  local backup_path="${legacy_path}.legacy-backup.$(date +%Y%m%d%H%M%S)"

  if [[ -e "${backup_path}" ]]; then
    backup_path="${legacy_path}.legacy-backup.$(date +%Y%m%d%H%M%S).$$"
  fi

  mv "${legacy_path}" "${backup_path}"
  log "[${target}] Archived legacy skill path to: ${backup_path}"
}

install_target() {
  local target="$1"
  local user_skills_dir
  local legacy_name
  local relay_cli_source="${REPO_ROOT}/scripts/relay.sh"
  local relay_cli_target="${HOME}/.config/ha-nova/relay"

  case "$target" in
    codex) user_skills_dir="${HOME}/.agents/skills" ;;
    claude) user_skills_dir="${HOME}/.claude/skills" ;;
    opencode) user_skills_dir="${HOME}/.config/opencode/skills" ;;
    *) die "Unsupported target: ${target}" ;;
  esac

  mkdir -p "${user_skills_dir}"

  for legacy_name in "${LEGACY_SKILL_NAMES[@]}"; do
    if [[ -e "${user_skills_dir}/${legacy_name}" ]]; then
      archive_legacy_skill "$target" "${user_skills_dir}/${legacy_name}"
    fi
  done

  local skill_name
  for skill_name in "${SKILL_NAMES[@]}"; do
    install_one_skill_for_target "$target" "$user_skills_dir" "$skill_name"
  done

  if [[ -f "${relay_cli_source}" ]]; then
    mkdir -p "${HOME}/.config/ha-nova"
    cp "${relay_cli_source}" "${relay_cli_target}"
    chmod 755 "${relay_cli_target}"
    log "[${target}] Installed relay CLI: ${relay_cli_target}"
  fi
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
