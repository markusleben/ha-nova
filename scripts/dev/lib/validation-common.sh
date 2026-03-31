#!/usr/bin/env bash

validation_repo_root() {
  local source_path="${1:?missing source path}"
  local levels_up="${2:?missing levels}"
  local script_dir
  script_dir="$(cd -- "$(dirname "${source_path}")" && pwd)"
  printf '%s\n' "$(cd -- "${script_dir}/${levels_up}" && pwd)"
}

create_temp_dir() {
  local template="$1"
  mktemp -d "${template}"
}

create_temp_file() {
  local template="$1"
  mktemp "${template}"
}

macos_detect_bundle_name() {
  case "$(uname -m)" in
    x86_64) printf '%s\n' "ha-nova-installer-bundle-macos-amd64" ;;
    arm64) printf '%s\n' "ha-nova-installer-bundle-macos-arm64" ;;
    *) echo "Unsupported macOS arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

macos_bundle_url() {
  local base_url="$1"
  local bundle_name
  bundle_name="$(macos_detect_bundle_name)"
  printf '%s\n' "${HA_NOVA_BUNDLE_URL:-${base_url}/${bundle_name}.tar.gz}"
}

macos_bundle_sha_url() {
  local bundle_url="$1"
  printf '%s\n' "${HA_NOVA_BUNDLE_SHA256_URL:-${bundle_url}.sha256}"
}

macos_bundle_reported_version() {
  local bundle_path="$1"
  local version_member
  version_member="$(tar -tzf "${bundle_path}" 2>/dev/null | grep -E '(^|/)version\.json$' | head -1 || true)"
  [[ -n "${version_member}" ]] || return 1
  tar -xOf "${bundle_path}" "${version_member}" 2>/dev/null | node -e 'const fs=require("fs"); const data=JSON.parse(fs.readFileSync(0,"utf8")); console.log(data.skill_version || data.version || "");'
}

macos_local_bundle_path() {
  local root_dir="$1"
  local bundle_name
  bundle_name="$(macos_detect_bundle_name)"
  printf '%s\n' "${root_dir}/dist/install-bundles/${bundle_name}.tar.gz"
}

require_default_bundle_version_or_explicit_mock() {
  local root_dir="$1"
  local base_url="$2"
  local bundle_url="$3"
  local default_bundle_url="${base_url}/$(macos_detect_bundle_name).tar.gz"
  local local_bundle_path
  local reported_version

  local_bundle_path="$(macos_local_bundle_path "${root_dir}")"

  if [[ -n "${MOCK_REPORTED_VERSION:-}" ]]; then
    printf '%s\n' "${MOCK_REPORTED_VERSION}"
    return 0
  fi

  if [[ -n "${HA_NOVA_BUNDLE_URL:-}" && "${bundle_url}" != "${default_bundle_url}" ]]; then
    echo "Set MOCK_REPORTED_VERSION explicitly when overriding the bundle source." >&2
    exit 1
  fi

  reported_version="$(macos_bundle_reported_version "${local_bundle_path}" || true)"
  if [[ -z "${reported_version}" ]]; then
    echo "Local macOS bundle not ready at ${local_bundle_path}" >&2
    echo "Run 'npm run test:desktop:macos' first, or start an equivalent fresh local bundle server." >&2
    exit 1
  fi

  printf '%s\n' "${reported_version}"
}

require_bundle_assets_ready() {
  local bundle_url="$1"
  local bundle_sha_url="$2"

  if curl -fsS --head "${bundle_url}" >/dev/null 2>&1 && curl -fsS --head "${bundle_sha_url}" >/dev/null 2>&1; then
    return 0
  fi

  echo "Bundle server not ready for ${bundle_url}" >&2
  echo "Run 'npm run test:desktop:macos' first, or start an equivalent fresh local bundle server." >&2
  exit 1
}
