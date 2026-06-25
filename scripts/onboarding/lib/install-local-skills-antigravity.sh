#!/usr/bin/env bash

cleanup_antigravity_unprefixed() {
  :
}

cleanup_antigravity_orphans() {
  local skills_dir="$1"

  cleanup_antigravity_unprefixed "$skills_dir"

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
      log "[antigravity] Removed orphaned skill: ${name}"
    fi
  done
}

cleanup_legacy_gemini_flat() {
  local legacy_skills_dir="${HOME}/.gemini/skills"
  for existing in "${legacy_skills_dir}"/ha-nova*/; do
    [[ ! -d "$existing" ]] && continue
    rm -rf "$existing"
    log "[antigravity] Removed legacy Gemini skill: $(basename "$existing")"
  done
}

install_antigravity_flat() {
  local user_skills_dir="${HOME}/.gemini/config/skills"
  mkdir -p "${user_skills_dir}"

  cleanup_legacy_flat_only "${HOME}/.agents/skills" "gemini-legacy"
  cleanup_legacy_gemini_flat
  cleanup_antigravity_orphans "${user_skills_dir}"

  local context_dir="${user_skills_dir}/ha-nova"
  if [[ -d "${context_dir}" ]]; then
    rm -rf "${context_dir}"
  fi
  copy_flat_skill_markdown "ha-nova" "${context_dir}"
  log "[antigravity] Installed: ha-nova/SKILL.md (context skill)"

  for sub in "${FLAT_SUB_SKILLS[@]}"; do
    local dest_name="ha-nova-${sub}"
    local dest_dir="${user_skills_dir}/${dest_name}"
    if [[ -f "${SOURCE_SKILLS_DIR}/${sub}/SKILL.md" ]]; then
      copy_flat_skill_markdown "${sub}" "${dest_dir}"
      log "[antigravity] Installed: ${dest_name}/SKILL.md"
    fi
  done
}
