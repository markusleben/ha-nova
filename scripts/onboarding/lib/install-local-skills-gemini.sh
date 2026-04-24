#!/usr/bin/env bash

cleanup_gemini_unprefixed() {
  :
}

cleanup_gemini_orphans() {
  local skills_dir="$1"

  cleanup_gemini_unprefixed "$skills_dir"

  local valid_skills="ha-nova"
  for skill_dir in "${SOURCE_SKILLS_DIR}"/*/SKILL.md; do
    local src_name
    src_name="$(basename "$(dirname "$skill_dir")")"
    [[ "$src_name" == "ha-nova" ]] && continue
    valid_skills="${valid_skills}"$'\n'"ha-nova-${src_name}"
  done

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

install_gemini_flat() {
  local user_skills_dir="${HOME}/.gemini/skills"
  mkdir -p "${user_skills_dir}"

  cleanup_legacy_flat_only "${HOME}/.agents/skills" "gemini-legacy"
  cleanup_gemini_orphans "${user_skills_dir}"

  local context_dir="${user_skills_dir}/ha-nova"
  if [[ -d "${context_dir}" ]]; then
    rm -rf "${context_dir}"
  fi
  copy_flat_skill_markdown "ha-nova" "${context_dir}"
  log "[gemini] Installed: ha-nova/SKILL.md (context skill)"

  for sub in "${GEMINI_SUB_SKILLS[@]}"; do
    local dest_name="ha-nova-${sub}"
    local dest_dir="${user_skills_dir}/${dest_name}"
    if [[ -f "${SOURCE_SKILLS_DIR}/${sub}/SKILL.md" ]]; then
      copy_flat_skill_markdown "${sub}" "${dest_dir}"
      log "[gemini] Installed: ${dest_name}/SKILL.md"
    fi
  done
}
