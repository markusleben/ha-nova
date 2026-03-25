#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
OUTPUT_DIR="${DIST_DIR}/winget"
RAW_VERSION="${1:-$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${ROOT_DIR}/version.json" | head -1)}"
REPO_SLUG="${2:-markusleben/ha-nova}"
PACKAGE_IDENTIFIER="${PACKAGE_IDENTIFIER:-markusleben.ha-nova}"
PACKAGE_NAME="${PACKAGE_NAME:-HA NOVA}"
PACKAGE_LOCALE="${PACKAGE_LOCALE:-en-US}"
MANIFEST_VERSION="${MANIFEST_VERSION:-1.9.0}"
WINDOWS_BUNDLE_NAME="ha-nova-installer-bundle-windows-amd64.zip"
WINDOWS_BUNDLE_PATH="${DIST_DIR}/install-bundles/${WINDOWS_BUNDLE_NAME}"
WINDOWS_BUNDLE_SHA_PATH="${WINDOWS_BUNDLE_PATH}.sha256"

normalize_version() {
  local raw="$1"
  printf '%s\n' "${raw#v}"
}

VERSION="$(normalize_version "${RAW_VERSION}")"
RELEASE_TAG="${3:-v${VERSION}}"
INSTALLER_URL="${HA_NOVA_WINGET_INSTALLER_URL:-https://github.com/${REPO_SLUG}/releases/download/${RELEASE_TAG}/${WINDOWS_BUNDLE_NAME}}"

log() {
  echo "[build-winget-manifest] $*"
}

die() {
  echo "[build-winget-manifest] ERROR: $*" >&2
  exit 1
}

package_json_value() {
  local key_path="$1"
  node --input-type=module -e '
    import fs from "node:fs";

    const pkg = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const path = process.argv[2].split(".");
    let value = pkg;
    for (const part of path) {
      value = value?.[part];
    }
    if (value == null || value === "") {
      process.exit(1);
    }
    process.stdout.write(String(value));
  ' "${ROOT_DIR}/package.json" "${key_path}"
}

compute_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print toupper($1)}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print toupper($1)}'
    return 0
  fi
  die "sha256sum or shasum is required to build winget manifests."
}

bundle_sha256() {
  local computed sidecar
  computed="$(compute_sha256 "${WINDOWS_BUNDLE_PATH}")"
  if [[ -f "${WINDOWS_BUNDLE_SHA_PATH}" ]]; then
    sidecar="$(awk 'NF { print toupper($1); exit }' "${WINDOWS_BUNDLE_SHA_PATH}")"
    [[ -n "${sidecar}" ]] || die "Bundle SHA sidecar is empty: ${WINDOWS_BUNDLE_SHA_PATH}"
    [[ "${computed}" == "${sidecar}" ]] || die "Bundle SHA sidecar mismatch. Expected ${computed}, got ${sidecar} from ${WINDOWS_BUNDLE_SHA_PATH}."
  fi
  printf '%s\n' "${computed}"
}

manifest_root() {
  local first_segment package_suffix
  first_segment="${PACKAGE_IDENTIFIER%%.*}"
  first_segment="${first_segment,,}"
  first_segment="${first_segment:0:1}"
  package_suffix="${PACKAGE_IDENTIFIER#*.}"
  printf '%s/manifests/%s/%s/%s/%s\n' "${OUTPUT_DIR}" "${first_segment}" "${PACKAGE_IDENTIFIER%%.*}" "${package_suffix}" "${VERSION}"
}

write_schema_header() {
  local file="$1" manifest_type="$2"
  cat > "${file}" <<EOF
# Created by HA NOVA release automation
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.${manifest_type}.${MANIFEST_VERSION}.schema.json

EOF
}

write_version_manifest() {
  local file="$1"
  write_schema_header "${file}" "version"
  cat >> "${file}" <<EOF
PackageIdentifier: ${PACKAGE_IDENTIFIER}
PackageVersion: ${VERSION}
DefaultLocale: ${PACKAGE_LOCALE}
ManifestType: version
ManifestVersion: ${MANIFEST_VERSION}
EOF
}

write_default_locale_manifest() {
  local file="$1"
  local publisher package_url support_url description license_id
  publisher="$(package_json_value author)"
  package_url="$(package_json_value homepage)"
  support_url="$(package_json_value bugs.url)"
  description="$(package_json_value description)"
  license_id="$(package_json_value license)"

  write_schema_header "${file}" "defaultLocale"
  cat >> "${file}" <<EOF
PackageIdentifier: ${PACKAGE_IDENTIFIER}
PackageVersion: ${VERSION}
PackageLocale: ${PACKAGE_LOCALE}
Publisher: ${publisher}
PublisherUrl: ${package_url}
PublisherSupportUrl: ${support_url}
Author: ${publisher}
PackageName: ${PACKAGE_NAME}
PackageUrl: ${package_url}
License: ${license_id}
LicenseUrl: ${package_url}/blob/main/LICENSE
ShortDescription: ${description}
Moniker: ha-nova
Tags:
  - home-assistant
  - automation
  - smart-home
  - ai
ReleaseNotesUrl: ${package_url}/releases/tag/${RELEASE_TAG}
ManifestType: defaultLocale
ManifestVersion: ${MANIFEST_VERSION}
EOF
}

write_installer_manifest() {
  local file="$1" installer_sha="$2"
  write_schema_header "${file}" "installer"
  cat >> "${file}" <<EOF
PackageIdentifier: ${PACKAGE_IDENTIFIER}
PackageVersion: ${VERSION}
InstallerType: zip
NestedInstallerType: portable
UpgradeBehavior: install
Installers:
  - Architecture: x64
    InstallerUrl: ${INSTALLER_URL}
    InstallerSha256: ${installer_sha}
    NestedInstallerFiles:
      - RelativeFilePath: ha-nova/ha-nova.exe
        PortableCommandAlias: ha-nova
ManifestType: installer
ManifestVersion: ${MANIFEST_VERSION}
EOF
}

write_archive_checksum() {
  local archive="$1" checksum
  checksum="$(compute_sha256 "${archive}")"
  printf '%s  %s\n' "${checksum}" "$(basename "${archive}")" > "${archive}.sha256"
  log "Wrote ${archive}.sha256"
}

main() {
  [[ -n "${VERSION}" ]] || die "Could not determine HA NOVA version."
  [[ -f "${WINDOWS_BUNDLE_PATH}" ]] || die "Missing Windows install bundle: ${WINDOWS_BUNDLE_PATH}"
  command -v zip >/dev/null 2>&1 || die "zip is required to archive winget manifests."

  local root version_file locale_file installer_file installer_sha archive
  root="$(manifest_root)"
  rm -rf "${OUTPUT_DIR}/manifests"
  mkdir -p "${root}"

  version_file="${root}/${PACKAGE_IDENTIFIER}.yaml"
  locale_file="${root}/${PACKAGE_IDENTIFIER}.locale.${PACKAGE_LOCALE}.yaml"
  installer_file="${root}/${PACKAGE_IDENTIFIER}.installer.yaml"
  installer_sha="$(bundle_sha256)"

  write_version_manifest "${version_file}"
  write_default_locale_manifest "${locale_file}"
  write_installer_manifest "${installer_file}" "${installer_sha}"

  archive="${OUTPUT_DIR}/ha-nova-winget-manifest-v${VERSION}.zip"
  rm -f "${archive}" "${archive}.sha256"
  (
    cd "${OUTPUT_DIR}"
    zip -qr "${archive}" "manifests"
  )
  write_archive_checksum "${archive}"

  log "Winget manifest ready in ${root}"
}

main "$@"
