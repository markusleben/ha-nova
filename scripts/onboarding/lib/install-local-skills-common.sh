#!/usr/bin/env bash

repo_root_from_bash_source() {
  local source_path="${1:?missing source path}"
  local levels_up="${2:?missing levels}"
  local script_dir
  script_dir="$(cd -- "$(dirname "${source_path}")" && pwd)"
  printf '%s\n' "$(cd -- "${script_dir}/${levels_up}" && pwd)"
}

log() {
  echo "[install-local-skills] $*"
}

die() {
  echo "[install-local-skills] $*" >&2
  exit 1
}

detect_platform_id() {
  local platform_source="${HA_NOVA_PLATFORM_OVERRIDE:-$(uname -s)}"

  case "$platform_source" in
    macos|Darwin) printf 'macos' ;;
    windows|MINGW*|MSYS*|CYGWIN*) printf 'windows' ;;
    Linux) printf 'linux' ;;
    *) printf '%s' "$platform_source" | tr '[:upper:]' '[:lower:]' ;;
  esac
}

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

read_version_json_field() {
  local file_path="$1"
  local field_name="$2"

  sed -n "s/.*\"${field_name}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "${file_path}" | head -1
}

repo_skill_version() {
  read_version_json_field "${REPO_ROOT}/version.json" "skill_version"
}

repo_claude_plugin_version() {
  read_version_json_field "${REPO_ROOT}/.claude-plugin/plugin.json" "version"
}

LEGACY_FLAT_SKILLS=(
  "ha-nova-dashboard"
  "ha-nova-organize"
  "ha-nova-history"
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

cleanup_legacy() {
  local user_skills_dir="$1"
  local target="$2"

  for legacy_skill in "${LEGACY_FLAT_SKILLS[@]}"; do
    local legacy_path="${user_skills_dir}/${legacy_skill}"
    if [[ -e "${legacy_path}" || -L "${legacy_path}" ]]; then
      rm -rf "${legacy_path}"
      log "[${target}] Cleaned up legacy flat skill: ${legacy_path}"
    fi
  done

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
