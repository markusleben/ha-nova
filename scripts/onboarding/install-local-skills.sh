#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/onboarding/install-local-skills.sh codex
  bash scripts/onboarding/install-local-skills.sh claude
  bash scripts/onboarding/install-local-skills.sh opencode
  bash scripts/onboarding/install-local-skills.sh antigravity
  bash scripts/onboarding/install-local-skills.sh hermes
  bash scripts/onboarding/install-local-skills.sh all

Targets:
  codex    -> link/copy ~/.agents/skills/ha-nova -> repo skills
  claude   -> stage local Claude marketplace + install ha-nova@ha-nova
  opencode -> link/copy ~/.config/opencode/skills/ha-nova -> repo skills
  antigravity -> flat copy ~/.gemini/config/skills/ha-nova-*/SKILL.md (+ local companion .md files)
  hermes   -> namespaced copy ~/.hermes/skills/ha-nova/ha-nova-*
  all      -> install for codex + claude + opencode + antigravity + hermes (non-Windows)
USAGE
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/install-local-skills-common.sh
. "${SCRIPT_DIR}/lib/install-local-skills-common.sh"
REPO_ROOT="$(repo_root_from_bash_source "${BASH_SOURCE[0]}" "../..")"
SOURCE_SKILLS_DIR="${REPO_ROOT}/skills"
CURRENT_PLATFORM_ID="$(detect_platform_id)"

FLAT_SUB_SKILLS=()
for _skill_dir in "${SOURCE_SKILLS_DIR}"/*/SKILL.md; do
  _skill_name="$(basename "$(dirname "$_skill_dir")")"
  [[ "$_skill_name" == "ha-nova" ]] && continue
  FLAT_SUB_SKILLS+=("$_skill_name")
done
unset _skill_dir _skill_name

flat_installed_skill_name() {
  local skill_name="$1"
  if [[ -z "${skill_name}" || "${skill_name}" == "ha-nova" ]]; then
    printf 'ha-nova'
    return
  fi
  printf 'ha-nova-%s' "${skill_name}"
}

rewrite_flat_markdown() {
  local skill_name="$1"
  local source_dir="$2"
  local src="$3"
  local dest="$4"
  local content
  local installed_skill_name

  installed_skill_name="$(flat_installed_skill_name "${skill_name}")"
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
    printf '%s' "${content}" | HA_NOVA_ROOT="${REPO_ROOT}" perl -0pe '
      s{`docs/reference/([^`]+)`}{sprintf("`%s/docs/reference/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
      s{`skills/([^`]+)`}{sprintf("`%s/skills/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
    '
  )"

  for flat_sub_skill in "${FLAT_SUB_SKILLS[@]}"; do
    content="${content//ha-nova:${flat_sub_skill}/ha-nova:ha-nova-${flat_sub_skill}}"
  done

  if [[ "${skill_name}" != "ha-nova" ]]; then
    content="$(
      printf '%s' "${content}" | SKILL_NAME="${skill_name}" INSTALLED_SKILL_NAME="${installed_skill_name}" perl -0pe '
        s/^name:\s*\Q$ENV{SKILL_NAME}\E$/name: $ENV{INSTALLED_SKILL_NAME}/m;
      '
    )"
  fi

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

hermes_installed_skill_name() {
  local skill_name="$1"
  if [[ -z "${skill_name}" || "${skill_name}" == "ha-nova" ]]; then
    printf 'ha-nova'
    return
  fi
  printf 'ha-nova-%s' "${skill_name}"
}

rewrite_hermes_markdown() {
  local skill_name="$1"
  local source_dir="$2"
  local src="$3"
  local dest="$4"
  local content
  local installed_skill_name

  installed_skill_name="$(hermes_installed_skill_name "${skill_name}")"
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
    printf '%s' "${content}" | HA_NOVA_ROOT="${REPO_ROOT}" perl -0pe '
      s{`docs/reference/([^`]+)`}{sprintf("`%s/docs/reference/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
      s{`skills/([^`]+)`}{sprintf("`%s/skills/%s`", $ENV{HA_NOVA_ROOT}, $1)}ge;
    '
  )"

  for sub_skill in "${FLAT_SUB_SKILLS[@]}"; do
    content="${content//ha-nova:${sub_skill}/ha-nova-${sub_skill}}"
  done

  if [[ "${skill_name}" != "ha-nova" ]]; then
    content="$(
      printf '%s' "${content}" | SKILL_NAME="${skill_name}" INSTALLED_SKILL_NAME="${installed_skill_name}" perl -0pe '
        s/^name:\s*\Q$ENV{SKILL_NAME}\E$/name: $ENV{INSTALLED_SKILL_NAME}/m;
      '
    )"
  fi

  printf '%s' "${content}" > "${dest}"
}

copy_hermes_skill_markdown() {
  local skill_name="$1"
  local source_dir="${SOURCE_SKILLS_DIR}/${skill_name}"
  local dest_dir="$2"

  mkdir -p "${dest_dir}"
  find "${dest_dir}" -maxdepth 1 -type f -name '*.md' -exec rm -f {} +

  if [[ -f "${source_dir}/SKILL.md" ]]; then
    rewrite_hermes_markdown "${skill_name}" "${source_dir}" "${source_dir}/SKILL.md" "${dest_dir}/SKILL.md"
  fi

  for companion in "${source_dir}"/*.md; do
    local companion_name
    companion_name="$(basename "${companion}")"
    [[ "${companion_name}" == "SKILL.md" ]] && continue
    rewrite_hermes_markdown "${skill_name}" "${source_dir}" "${companion}" "${dest_dir}/${companion_name}"
  done
}

install_hermes_tree() {
  local target_root="${HOME}/.hermes/skills/ha-nova"

  [[ "${CURRENT_PLATFORM_ID}" == "windows" ]] && die "[hermes] Hermes Agent is supported through WSL2, not native Windows"
  command -v hermes >/dev/null 2>&1 || die "[hermes] Hermes Agent not found in PATH"

  rm -rf "${target_root}"
  mkdir -p "${target_root}"

  copy_hermes_skill_markdown "ha-nova" "${target_root}/ha-nova"
  for skill_name in "${FLAT_SUB_SKILLS[@]}"; do
    copy_hermes_skill_markdown "${skill_name}" "${target_root}/$(hermes_installed_skill_name "${skill_name}")"
  done

  log "[hermes] Copied namespaced skill tree: ${target_root}"
}

install_symlink_tree() {
  local target="$1"
  local user_skills_dir="$2"

  mkdir -p "${user_skills_dir}"
  cleanup_legacy "${user_skills_dir}" "${target}"

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

# shellcheck source=lib/install-local-skills-antigravity.sh
. "${SCRIPT_DIR}/lib/install-local-skills-antigravity.sh"
# shellcheck source=lib/install-local-skills-claude.sh
. "${SCRIPT_DIR}/lib/install-local-skills-claude.sh"
# shellcheck source=lib/install-local-skills-repo-dev.sh
. "${SCRIPT_DIR}/lib/install-local-skills-repo-dev.sh"

require_repo_invariants() {
  [[ -d "${SOURCE_SKILLS_DIR}" ]] || die "Missing repo skills directory: ${SOURCE_SKILLS_DIR}"
  [[ -f "${REPO_ROOT}/version.json" ]] || die "Missing repo version file: ${REPO_ROOT}/version.json"
  [[ -x "${REPO_ROOT}/scripts/onboarding/bin/ha-nova" ]] || die "Missing repo helper runtime shim: ${REPO_ROOT}/scripts/onboarding/bin/ha-nova"
  [[ -f "${SOURCE_SKILLS_DIR}/ha-nova/session-bootstrap.md" ]] ||
    die "Missing mandatory session bootstrap: ${SOURCE_SKILLS_DIR}/ha-nova/session-bootstrap.md"
}

require_target_prereqs() {
  local target="$1"

  case "${target}" in
    claude)
      command -v claude >/dev/null 2>&1 || die "[claude] Claude CLI not found in PATH"
      if claude_plugin_state_helper_required; then
        claude_plugin_state_runtime_ready || die "[claude] Node.js not found in PATH (required for local plugin state helper)"
      fi
      ;;
  esac
}

install_target() {
  local target="$1"
  local helper_target="$target"

  require_target_prereqs "${target}"

  case "${target}" in
    codex)
      install_symlink_tree "codex" "${HOME}/.agents/skills"
      ;;
    claude)
      install_claude_plugin
      ;;
    opencode)
      install_symlink_tree "opencode" "${HOME}/.config/opencode/skills"
      ;;
    antigravity|gemini)
      install_antigravity_flat
      helper_target="antigravity"
      ;;
    hermes)
      install_hermes_tree
      ;;
    *)
      die "Unsupported target: ${target}"
      ;;
  esac

  install_repo_dev_helpers "${helper_target}"
}

main() {
  local target="${1:-}"

  require_repo_invariants

  if [[ -z "${target}" ]]; then
    usage
    die "No target specified. Please provide a target explicitly."
  fi

  case "${target}" in
    codex|claude|opencode|antigravity|gemini|hermes)
      install_target "${target}"
      ;;
    all)
      install_target "codex"
      install_target "antigravity"
      install_target "claude"
      install_target "opencode"
      if [[ "${CURRENT_PLATFORM_ID}" != "windows" ]]; then
        install_target "hermes"
      fi
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
