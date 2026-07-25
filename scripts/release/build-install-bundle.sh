#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
OUTPUT_DIR="${DIST_DIR}/install-bundles"
VERSION="${1:-$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${ROOT_DIR}/version.json" | head -1)}"
CLOUD_REMOTE_ENABLED="$(
  node -e \
    'process.stdout.write(String(JSON.parse(require("node:fs").readFileSync(process.argv[1], "utf8")).cloud_remote_enabled))' \
    "${ROOT_DIR}/version.json"
)"
CLOUD_REMOTE_PLATFORMS="$(
  node -e \
    'process.stdout.write(JSON.parse(require("node:fs").readFileSync(process.argv[1], "utf8")).cloud_remote_platforms.join(","))' \
    "${ROOT_DIR}/version.json"
)"
SOURCE_TREE_SHA="$(git -C "${ROOT_DIR}" rev-parse --verify "HEAD^{tree}")"

log() {
  echo "[build-install-bundle] $*"
}

die() {
  echo "[build-install-bundle] ERROR: $*" >&2
  exit 1
}

compute_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return 0
  fi
  die "sha256sum or shasum is required to write bundle checksums."
}

binary_asset_path() {
  local os_name="$1" arch_name="$2"
  local source_os="${os_name}" flat_path binary_name
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

  mapfile -t nested_paths < <(
    find "${DIST_DIR}" -type f -name "${binary_name}" -path "*_${source_os}_${arch_name}_*/*" | sort
  )
  if [[ -f "${flat_path}" && "${#nested_paths[@]}" -gt 0 ]]; then
    die "Ambiguous binary candidates for ${os_name}/${arch_name}: flat=${flat_path} nested=${nested_paths[*]}"
  fi
  if [[ -f "${flat_path}" ]]; then
    printf '%s\n' "${flat_path}"
    return
  fi
  if [[ "${#nested_paths[@]}" -eq 1 ]]; then
    printf '%s\n' "${nested_paths[0]}"
    return
  fi
  if [[ "${#nested_paths[@]}" -gt 1 ]]; then
    die "Ambiguous nested binary candidates for ${os_name}/${arch_name}: ${nested_paths[*]}"
  fi

  printf '%s\n' "${flat_path}"
}

copy_common_bundle_files() {
  local bundle_root="$1"

  mkdir -p "${bundle_root}" "${bundle_root}/docs"
  cp -R "${ROOT_DIR}/clients" "${bundle_root}/clients"
  cp -R "${ROOT_DIR}/skills" "${bundle_root}/skills"
  cp -R "${ROOT_DIR}/docs/reference" "${bundle_root}/docs/reference"
  cp -R "${ROOT_DIR}/.claude-plugin" "${bundle_root}/.claude-plugin"
  cp "${ROOT_DIR}/version.json" "${bundle_root}/version.json"
  cp "${ROOT_DIR}/PRIVACY.md" "${bundle_root}/PRIVACY.md"
  [[ -f "${ROOT_DIR}/README.md" ]] && cp "${ROOT_DIR}/README.md" "${bundle_root}/README.md"
  [[ -f "${ROOT_DIR}/PROJECT.md" ]] && cp "${ROOT_DIR}/PROJECT.md" "${bundle_root}/PROJECT.md"
}

write_bundle_metadata() {
  local bundle_root="$1" os_name="$2" arch_name="$3" binary_name="$4"
  local evidence_json="null"
  if [[ "${CLOUD_REMOTE_ENABLED}" == "true" ]]; then
    evidence_json="$(
      node "${ROOT_DIR}/scripts/release/sign-cloud-release-evidence.mjs" \
        "${VERSION}" "${os_name}" "${arch_name}" "${binary_name}" \
        "${bundle_root}/${binary_name}" "${SOURCE_TREE_SHA}" \
        "${CLOUD_REMOTE_PLATFORMS}"
    )"
  fi
  node - "${bundle_root}/bundle.json" "${VERSION}" "${os_name}" \
    "${arch_name}" "${binary_name}" "${evidence_json}" <<'NODE'
const fs = require("node:fs");
const [output, version, os, arch, binaryName, evidenceJSON] =
  process.argv.slice(2);
const bundle = {
  bundle_format_version: 1,
  version,
  os,
  arch,
  binary_name: binaryName,
};
const evidence = JSON.parse(evidenceJSON);
if (evidence !== null) {
  bundle.cloud_release = evidence;
}
fs.writeFileSync(output, `${JSON.stringify(bundle, null, 2)}\n`);
NODE
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

  output="${OUTPUT_DIR}/ha-nova-installer-bundle-${os_name}-${arch_name}.tar.gz"
  COPYFILE_DISABLE=1 tar --format ustar -czf "${output}" -C "${stage_dir}" ha-nova
  rm -rf "${stage_dir}"
  log "Built ${output}"
}

build_windows_bundle() {
  local arch_name="$1"
  command -v zip >/dev/null 2>&1 || die "zip is required to build Windows bundles."

  local stage_dir output
  stage_dir="$(mktemp -d)"
  prepare_bundle_root "${stage_dir}" "windows" "${arch_name}" >/dev/null

  output="${OUTPUT_DIR}/ha-nova-installer-bundle-windows-${arch_name}.zip"
  (
    cd "${stage_dir}"
    zip -qr "${output}" ha-nova
  )
  rm -rf "${stage_dir}"
  log "Built ${output}"
}

write_bundle_checksums() {
  local bundle sum
  for bundle in "${OUTPUT_DIR}"/ha-nova-installer-bundle-*.tar.gz "${OUTPUT_DIR}"/ha-nova-installer-bundle-*.zip; do
    [[ -f "${bundle}" ]] || continue
    sum="$(compute_sha256 "${bundle}")"
    printf '%s  %s\n' "${sum}" "$(basename "${bundle}")" > "${bundle}.sha256"
    log "Wrote ${bundle}.sha256"
  done
}

main() {
  [[ -n "${VERSION}" ]] || die "Could not determine HA NOVA version."
  [[ "${VERSION}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc[1-9][0-9]*)?$ ]] \
    || die "Version must be strict X.Y.Z or X.Y.Z-rcN."
  [[ "${SOURCE_TREE_SHA}" =~ ^[0-9a-f]{40}$ ]] \
    || die "Could not determine the source tree SHA."
  [[ "${CLOUD_REMOTE_ENABLED}" == "true" || "${CLOUD_REMOTE_ENABLED}" == "false" ]] \
    || die "version.json cloud_remote_enabled must be a boolean."
  if [[ "${CLOUD_REMOTE_ENABLED}" == "true" && -z "${CLOUD_REMOTE_PLATFORMS}" ]]; then
    die "Enabled Cloud Remote metadata requires at least one platform."
  fi
  [[ -d "${DIST_DIR}" ]] || die "dist directory not found: ${DIST_DIR}"

  mkdir -p "${OUTPUT_DIR}"
  rm -f "${OUTPUT_DIR}"/ha-nova-installer-bundle-macos-*.tar.gz "${OUTPUT_DIR}"/ha-nova-installer-bundle-linux-*.tar.gz "${OUTPUT_DIR}"/ha-nova-installer-bundle-windows-*.zip "${OUTPUT_DIR}"/ha-nova-installer-bundle-*.sha256

  build_unix_bundle "macos" "amd64"
  build_unix_bundle "macos" "arm64"
  build_unix_bundle "linux" "amd64"
  build_unix_bundle "linux" "arm64"
  build_windows_bundle "amd64"
  write_bundle_checksums

  log "Install bundles ready for v${VERSION} in ${OUTPUT_DIR}"
}

main "$@"
