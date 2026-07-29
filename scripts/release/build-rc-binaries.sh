#!/usr/bin/env bash
set -euo pipefail

TRUSTED_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ROOT_DIR="$(cd -- "${HA_NOVA_SOURCE_ROOT:-${TRUSTED_ROOT}}" && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
RAW_TAG="${1:-}"

fail() {
  echo "[build-rc-binaries] ERROR: $*" >&2
  exit 1
}

[[ "${RAW_TAG}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc[1-9][0-9]*$ ]] \
  || fail "tag must be strict vX.Y.Z-rcN"
version="${RAW_TAG#v}"
mkdir -p "${DIST_DIR}"

build() {
  local os_name="$1" arch_name="$2" suffix="${3:-}"
  local output="${DIST_DIR}/ha-nova-${os_name}-${arch_name}${suffix}"
  echo "[build-rc-binaries] Building ${output} at ${version}"
  (
    cd "${ROOT_DIR}/cli"
    CGO_ENABLED=0 GOOS="${os_name}" GOARCH="${arch_name}" \
      go build -trimpath -tags cloudremote_official \
        -ldflags="-s -w -X main.Version=${version}" \
        -o "${output}" .
  )
}

build linux amd64
build linux arm64
build windows amd64 .exe
