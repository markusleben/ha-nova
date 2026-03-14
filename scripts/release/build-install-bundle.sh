#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
OUTPUT_DIR="${DIST_DIR}/install-bundles"
VERSION="${1:-$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${ROOT_DIR}/version.json" | head -1)}"

log() {
  echo "[build-install-bundle] $*"
}

die() {
  echo "[build-install-bundle] ERROR: $*" >&2
  exit 1
}

binary_asset_path() {
  local os_name="$1" arch_name="$2"
  local source_os="${os_name}" flat_path binary_name nested_path
  if [[ "${source_os}" == "macos" ]]; then
    source_os="darwin"
  fi
  binary_name="ha-nova"
  if [[ "${os_name}" == "windows" ]]; then
    binary_name="ha-nova.exe"
    flat_path="${DIST_DIR}/ha-nova-${source_os}-${arch_name}.exe"
  else
    flat_path="${DIST_DIR}/ha-nova-${source_os}-${arch_name}"
  fi

  if [[ -f "${flat_path}" ]]; then
    printf '%s\n' "${flat_path}"
    return
  fi

  nested_path="$(
    find "${DIST_DIR}" -type f -name "${binary_name}" -path "*_${source_os}_${arch_name}_*/*" | sort | head -1
  )"
  if [[ -n "${nested_path}" ]]; then
    printf '%s\n' "${nested_path}"
    return
  fi

  printf '%s\n' "${flat_path}"
}

copy_common_bundle_files() {
  local bundle_root="$1"

  mkdir -p "${bundle_root}" "${bundle_root}/docs"
  cp -R "${ROOT_DIR}/skills" "${bundle_root}/skills"
  cp -R "${ROOT_DIR}/docs/reference" "${bundle_root}/docs/reference"
  cp -R "${ROOT_DIR}/.claude-plugin" "${bundle_root}/.claude-plugin"
  cp "${ROOT_DIR}/version.json" "${bundle_root}/version.json"
  [[ -f "${ROOT_DIR}/README.md" ]] && cp "${ROOT_DIR}/README.md" "${bundle_root}/README.md"
  [[ -f "${ROOT_DIR}/PROJECT.md" ]] && cp "${ROOT_DIR}/PROJECT.md" "${bundle_root}/PROJECT.md"
}

write_bundle_metadata() {
  local bundle_root="$1" os_name="$2" arch_name="$3" binary_name="$4"
  cat > "${bundle_root}/bundle.json" <<EOF
{
  "bundle_format_version": 1,
  "version": "${VERSION}",
  "os": "${os_name}",
  "arch": "${arch_name}",
  "binary_name": "${binary_name}"
}
EOF
}

prepare_bundle_root() {
  local stage_dir="$1" os_name="$2" arch_name="$3"
  local bundle_root="${stage_dir}/ha-nova"
  copy_common_bundle_files "${bundle_root}"

  local binary_asset binary_name
  binary_asset="$(binary_asset_path "${os_name}" "${arch_name}")"
  [[ -f "${binary_asset}" ]] || die "Missing ha-nova artifact: ${binary_asset}"

  binary_name="ha-nova"
  if [[ "${os_name}" == "windows" ]]; then
    binary_name="ha-nova.exe"
  fi
  cp "${binary_asset}" "${bundle_root}/${binary_name}"
  chmod 755 "${bundle_root}/${binary_name}" 2>/dev/null || true
  write_bundle_metadata "${bundle_root}" "${os_name}" "${arch_name}" "${binary_name}"
  printf '%s\n' "${bundle_root}"
}

build_unix_bundle() {
  local os_name="$1" arch_name="$2"
  local stage_dir output
  stage_dir="$(mktemp -d)"
  prepare_bundle_root "${stage_dir}" "${os_name}" "${arch_name}" >/dev/null

  output="${OUTPUT_DIR}/ha-nova-${os_name}-${arch_name}.tar.gz"
  tar -czf "${output}" -C "${stage_dir}" ha-nova
  rm -rf "${stage_dir}"
  log "Built ${output}"
}

build_windows_bundle() {
  local arch_name="$1"
  command -v zip >/dev/null 2>&1 || die "zip is required to build Windows bundles."

  local stage_dir output
  stage_dir="$(mktemp -d)"
  prepare_bundle_root "${stage_dir}" "windows" "${arch_name}" >/dev/null

  output="${OUTPUT_DIR}/ha-nova-windows-${arch_name}.zip"
  (
    cd "${stage_dir}"
    zip -qr "${output}" ha-nova
  )
  rm -rf "${stage_dir}"
  log "Built ${output}"
}

write_bundle_checksums() {
  local bundle sum
  for bundle in "${OUTPUT_DIR}"/ha-nova-*.tar.gz "${OUTPUT_DIR}"/ha-nova-*.zip; do
    [[ -f "${bundle}" ]] || continue
    sum="$(sha256sum "${bundle}" | awk '{print $1}')"
    printf '%s  %s\n' "${sum}" "$(basename "${bundle}")" > "${bundle}.sha256"
    log "Wrote ${bundle}.sha256"
  done
}

main() {
  [[ -n "${VERSION}" ]] || die "Could not determine HA NOVA version."
  [[ -d "${DIST_DIR}" ]] || die "dist directory not found: ${DIST_DIR}"

  mkdir -p "${OUTPUT_DIR}"
  rm -f "${OUTPUT_DIR}"/ha-nova-macos-*.tar.gz "${OUTPUT_DIR}"/ha-nova-linux-*.tar.gz "${OUTPUT_DIR}"/ha-nova-windows-*.zip "${OUTPUT_DIR}"/ha-nova-*.sha256

  build_unix_bundle "macos" "amd64"
  build_unix_bundle "macos" "arm64"
  build_unix_bundle "linux" "amd64"
  build_unix_bundle "linux" "arm64"
  build_windows_bundle "amd64"
  write_bundle_checksums

  log "Install bundles ready for v${VERSION} in ${OUTPUT_DIR}"
}

main "$@"
