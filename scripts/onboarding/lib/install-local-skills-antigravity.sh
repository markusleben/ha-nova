#!/usr/bin/env bash

cleanup_legacy_gemini_flat() {
  local legacy_skills_dir="${HOME}/.gemini/skills"
  local managed_skills=("ha-nova")
  local legacy_name
  while IFS= read -r legacy_name; do
    [[ -n "$legacy_name" ]] && managed_skills+=("$legacy_name")
  done < <(legacy_flat_skill_names)

  for name in "${managed_skills[@]}"; do
    local existing="${legacy_skills_dir}/${name}"
    [[ ! -d "$existing" ]] && continue
    rm -rf "$existing"
    log "[antigravity] Removed legacy Gemini skill: $(basename "$existing")"
  done
}

cleanup_legacy_codex_gemini_flat() {
  cleanup_legacy_flat_only "${HOME}/.agents/skills" "antigravity-legacy-codex"
}

install_antigravity_flat() {
  local user_skills_dir="${HOME}/.gemini/config/skills"
  mkdir -p "${user_skills_dir}"

  cleanup_legacy_gemini_flat
  cleanup_legacy_codex_gemini_flat

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
