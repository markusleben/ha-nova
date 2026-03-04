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
  codex    -> ~/.agents/skills/{ha-nova,...}   (also used by Gemini CLI)
  claude   -> ~/.claude/skills/{ha-nova,...}
  opencode -> ~/.config/opencode/skills/{ha-nova,...}
  gemini   -> alias for codex (both use ~/.agents/skills/)
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

  [[ -f "${source_skill_file}" ]] || die "Missing source skill: ${source_skill_file}"

  # Remove symlinks (legacy install method)
  if [[ -L "${dest_skill_path}" ]]; then
    rm -f "${dest_skill_path}"
  fi

  mkdir -p "${dest_skill_path}"
  render_skill_file "${source_skill_file}" "${dest_skill_file}"
  log "[${target}] Installed skill file: ${dest_skill_file}"
}

install_target() {
  local target="$1"
  local user_skills_dir
  local relay_cli_source="${REPO_ROOT}/scripts/relay.sh"
  local relay_cli_target="${HOME}/.config/ha-nova/relay"

  case "$target" in
    codex|gemini) user_skills_dir="${HOME}/.agents/skills" ;;
    claude) user_skills_dir="${HOME}/.claude/skills" ;;
    opencode) user_skills_dir="${HOME}/.config/opencode/skills" ;;
    *) die "Unsupported target: ${target}" ;;
  esac

  mkdir -p "${user_skills_dir}"

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
      install_target "codex"   # also covers gemini (shared ~/.agents/skills/)
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
