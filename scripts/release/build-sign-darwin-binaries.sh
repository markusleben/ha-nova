#!/usr/bin/env bash
set -euo pipefail
umask 077

TRUSTED_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ROOT_DIR="$(cd -- "${HA_NOVA_SOURCE_ROOT:-${TRUSTED_ROOT}}" && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
RAW_TAG="${1:-}"
EXPECTED_IDENTITY="Developer ID Application: Markus Leben (CTF9J94274)"
SIGNING_IDENTIFIER="com.markusleben.ha-nova.cli"

fail() {
  echo "[build-sign-darwin-binaries] ERROR: $*" >&2
  exit 1
}

[[ "${RAW_TAG}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc[1-9][0-9]*)?$ ]] \
  || fail "tag must be strict vX.Y.Z or vX.Y.Z-rcN"
[[ "$(uname -s)" == "Darwin" ]] || fail "macOS is required"
[[ -n "${HA_NOVA_MACOS_CERTIFICATE_P12_BASE64:-}" ]] \
  || fail "HA_NOVA_MACOS_CERTIFICATE_P12_BASE64 is required"
[[ -n "${HA_NOVA_MACOS_CERTIFICATE_PASSWORD:-}" ]] \
  || fail "HA_NOVA_MACOS_CERTIFICATE_PASSWORD is required"

version="${RAW_TAG#v}"
temporary_base="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
[[ -d "${temporary_base}" ]] || fail "temporary directory does not exist"
signing_dir="$(mktemp -d "${temporary_base%/}/ha-nova-signing.XXXXXX")"
certificate_path="${signing_dir}/developer-id.p12"
keychain_path="${signing_dir}/ha-nova-signing.keychain-db"
original_keychains=()
keychain_search_list_changed=false

cleanup() {
  if [[ "${keychain_search_list_changed}" == true ]]; then
    /usr/bin/security list-keychains -d user -s "${original_keychains[@]}" >/dev/null 2>&1 || true
  fi
  /usr/bin/security delete-keychain "${keychain_path}" >/dev/null 2>&1 || true
  /bin/chmod -R u+rwX "${signing_dir}" >/dev/null 2>&1 || true
  /bin/rm -rf "${signing_dir}"
}
trap cleanup EXIT

certificate_password="${HA_NOVA_MACOS_CERTIFICATE_PASSWORD}"
keychain_password="$(/usr/bin/openssl rand -base64 48)"
printf '%s' "${HA_NOVA_MACOS_CERTIFICATE_P12_BASE64}" \
  | /usr/bin/base64 --decode >"${certificate_path}" \
  || fail "could not decode Developer ID certificate"
unset HA_NOVA_MACOS_CERTIFICATE_P12_BASE64
unset HA_NOVA_MACOS_CERTIFICATE_PASSWORD

original_keychain_lines="$(
  /usr/bin/security list-keychains -d user \
    | /usr/bin/sed -E 's/^[[:space:]]*"//; s/"$//'
)" || fail "could not read the user keychain search list"
while IFS= read -r original_keychain; do
  [[ -z "${original_keychain}" ]] || original_keychains+=("${original_keychain}")
done <<< "${original_keychain_lines}"

/usr/bin/security create-keychain -p "${keychain_password}" "${keychain_path}"
/usr/bin/security set-keychain-settings -lut 21600 "${keychain_path}"
/usr/bin/security unlock-keychain -p "${keychain_password}" "${keychain_path}"
/usr/bin/security import "${certificate_path}" \
  -P "${certificate_password}" \
  -T /usr/bin/codesign \
  -t cert \
  -f pkcs12 \
  -k "${keychain_path}" >/dev/null
/usr/bin/security set-key-partition-list \
  -S apple-tool:,apple:,codesign: \
  -s \
  -k "${keychain_password}" \
  "${keychain_path}" >/dev/null
/usr/bin/security list-keychains -d user -s "${keychain_path}"
keychain_search_list_changed=true
unset certificate_password
unset keychain_password

identity_lines="$(
  /usr/bin/security find-identity -v -p codesigning "${keychain_path}" \
    | grep -F "\"${EXPECTED_IDENTITY}\"" || true
)"
identity_count="$(grep -c . <<<"${identity_lines}" || true)"
[[ "${identity_count}" == "1" ]] \
  || fail "expected exactly one ${EXPECTED_IDENTITY} identity"

mkdir -p "${DIST_DIR}"
for arch in amd64 arm64; do
  output="${DIST_DIR}/ha-nova-darwin-${arch}"
  [[ ! -L "${output}" ]] || fail "refusing to replace symlink: ${output}"
  echo "[build-sign-darwin-binaries] Building ${output} at ${version}"
  (
    cd "${ROOT_DIR}/cli"
    CGO_ENABLED=0 GOOS=darwin GOARCH="${arch}" \
      go build -trimpath -tags cloudremote_official \
        -ldflags="-s -w -X main.Version=${version}" \
        -o "${output}" .
  )
  /usr/bin/codesign \
    --force \
    --sign "${EXPECTED_IDENTITY}" \
    --keychain "${keychain_path}" \
    --timestamp \
    --options runtime,hard,kill,library \
    --identifier "${SIGNING_IDENTIFIER}" \
    "${output}"
  bash "${TRUSTED_ROOT}/scripts/release/verify-macos-signature.sh" "${output}"
done

echo "[build-sign-darwin-binaries] Signed Darwin binaries ready for v${version}"
