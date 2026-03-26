#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUNDLE_PORT="${BUNDLE_PORT:-8917}"
HA_PORT="${HA_PORT:-8123}"
RELAY_PORT="${RELAY_PORT:-8791}"
WITH_MOCK=0
SKIP_BUILD=0
REPORTED_VERSION="${REPORTED_VERSION:-}"

usage() {
  cat <<'EOF'
Usage: scripts/dev/start-local-validation-harness.sh [--with-mock] [--no-build]

Starts a local install-bundle server for manual macOS/Windows validation.

Options:
  --with-mock   Also start the tiny fake Home Assistant + relay /health mock
  --no-build    Reuse the current dist/install-bundles contents
EOF
}

while (($# > 0)); do
  case "$1" in
    --with-mock)
      WITH_MOCK=1
      shift
      ;;
    --no-build)
      SKIP_BUILD=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

cleanup() {
  if [[ -n "${MOCK_PID:-}" ]]; then
    kill "${MOCK_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${BUNDLE_PID:-}" ]]; then
    kill "${BUNDLE_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

wait_for_url() {
  local url="$1"
  local attempts="${2:-20}"
  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

bundle_reported_version() {
  local bundle_path="$1"
  local version_member
  version_member="$(tar -tzf "${bundle_path}" 2>/dev/null | grep -E '(^|/)version\.json$' | head -1 || true)"
  if [[ -z "${version_member}" ]]; then
    return 1
  fi
  tar -xOf "${bundle_path}" "${version_member}" 2>/dev/null | node -e 'const fs=require("fs"); const data=JSON.parse(fs.readFileSync(0,"utf8")); console.log(data.skill_version || data.version || "");'
}

ensure_port_free() {
  local port="$1"
  local label="$2"
  local listeners
  listeners="$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -n "${listeners}" ]]; then
    echo "${label} port ${port} is already in use." >&2
    echo "Stop the old process first or rerun with a different port." >&2
    echo >&2
    echo "${listeners}" >&2
    exit 1
  fi
}

detect_lan_ip() {
  python3 - <<'PY'
import socket

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
try:
    sock.connect(("1.1.1.1", 80))
    print(sock.getsockname()[0])
except OSError:
    print("")
finally:
    sock.close()
PY
}

cd "${ROOT_DIR}"

LAN_IP="$(detect_lan_ip)"
WINDOWS_HOST="${LAN_IP:-<your-mac-lan-ip>}"

if [[ "${SKIP_BUILD}" != "1" ]]; then
  npm run release:rc:local
fi

MACOS_ARM64_BUNDLE="dist/install-bundles/ha-nova-installer-bundle-macos-arm64.tar.gz"
MACOS_AMD64_BUNDLE="dist/install-bundles/ha-nova-installer-bundle-macos-amd64.tar.gz"
WINDOWS_BUNDLE="dist/install-bundles/ha-nova-installer-bundle-windows-amd64.zip"
if [[ ! -f "${MACOS_ARM64_BUNDLE}" ]]; then
  echo "Missing ${MACOS_ARM64_BUNDLE}. Run npm run release:rc:local first." >&2
  exit 1
fi
if [[ ! -f "${MACOS_AMD64_BUNDLE}" ]]; then
  echo "Missing ${MACOS_AMD64_BUNDLE}. Run npm run release:rc:local first." >&2
  exit 1
fi
if [[ ! -f "${WINDOWS_BUNDLE}" ]]; then
  echo "Missing ${WINDOWS_BUNDLE}. Run npm run release:rc:local first." >&2
  exit 1
fi

if [[ -z "${REPORTED_VERSION}" ]]; then
  REPORTED_VERSION="$(bundle_reported_version "${MACOS_ARM64_BUNDLE}")"
fi
if [[ -z "${REPORTED_VERSION}" ]]; then
  echo "Could not derive reported version from ${MACOS_ARM64_BUNDLE}" >&2
  exit 1
fi

ensure_port_free "${BUNDLE_PORT}" "Bundle server"
if [[ "${WITH_MOCK}" == "1" ]]; then
  ensure_port_free "${HA_PORT}" "HA mock"
  ensure_port_free "${RELAY_PORT}" "Relay mock"
fi

python3 -m http.server "${BUNDLE_PORT}" --bind 0.0.0.0 --directory . >/dev/null 2>&1 &
BUNDLE_PID=$!
if ! wait_for_url "http://127.0.0.1:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-macos-arm64.tar.gz.sha256"; then
  echo "Bundle server did not become ready on :${BUNDLE_PORT}" >&2
  exit 1
fi
for asset_path in \
  "dist/install-bundles/ha-nova-installer-bundle-macos-arm64.tar.gz" \
  "dist/install-bundles/ha-nova-installer-bundle-macos-arm64.tar.gz.sha256" \
  "dist/install-bundles/ha-nova-installer-bundle-macos-amd64.tar.gz" \
  "dist/install-bundles/ha-nova-installer-bundle-macos-amd64.tar.gz.sha256" \
  "dist/install-bundles/ha-nova-installer-bundle-windows-amd64.zip" \
  "dist/install-bundles/ha-nova-installer-bundle-windows-amd64.zip.sha256" \
  "install.ps1"
do
  if ! wait_for_url "http://127.0.0.1:${BUNDLE_PORT}/${asset_path}"; then
    echo "Harness asset missing or not reachable: ${asset_path}" >&2
    exit 1
  fi
done

if [[ "${WITH_MOCK}" == "1" ]]; then
  python3 scripts/dev/mock-ha-relay.py \
    --ha-port "${HA_PORT}" \
    --relay-port "${RELAY_PORT}" \
    --reported-version "${REPORTED_VERSION}" >/dev/null 2>&1 &
  MOCK_PID=$!
  if ! wait_for_url "http://127.0.0.1:${HA_PORT}/"; then
    echo "HA mock did not become ready on :${HA_PORT}" >&2
    exit 1
  fi
  if ! wait_for_url "http://127.0.0.1:${RELAY_PORT}/health"; then
    echo "Relay mock did not become ready on :${RELAY_PORT}" >&2
    exit 1
  fi
fi

cat <<EOF

Local validation harness is ready.

Bundle server:
  http://127.0.0.1:${BUNDLE_PORT}
  http://${WINDOWS_HOST}:${BUNDLE_PORT}

macOS install (Apple Silicon):
  export HA_NOVA_CLAUDE_MARKETPLACE_LOCAL='1'
  export HA_NOVA_BUNDLE_URL='http://127.0.0.1:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-macos-arm64.tar.gz'
  export HA_NOVA_BUNDLE_SHA256_URL='http://127.0.0.1:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-macos-arm64.tar.gz.sha256'
  bash ./install.sh

macOS install (Intel):
  export HA_NOVA_CLAUDE_MARKETPLACE_LOCAL='1'
  export HA_NOVA_BUNDLE_URL='http://127.0.0.1:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-macos-amd64.tar.gz'
  export HA_NOVA_BUNDLE_SHA256_URL='http://127.0.0.1:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-macos-amd64.tar.gz.sha256'
  bash ./install.sh

Windows install:
  \$env:HA_NOVA_CLAUDE_MARKETPLACE_LOCAL='1'
  \$env:HA_NOVA_BUNDLE_URL='http://${WINDOWS_HOST}:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-windows-amd64.zip'
  \$env:HA_NOVA_BUNDLE_SHA256_URL='http://${WINDOWS_HOST}:${BUNDLE_PORT}/dist/install-bundles/ha-nova-installer-bundle-windows-amd64.zip.sha256'
  irm http://${WINDOWS_HOST}:${BUNDLE_PORT}/install.ps1 | iex
EOF

if [[ "${WITH_MOCK}" == "1" ]]; then
  cat <<EOF

Mock Home Assistant:
  http://127.0.0.1:${HA_PORT}
  http://${WINDOWS_HOST}:${HA_PORT}

Fake relay /health:
  http://127.0.0.1:${RELAY_PORT}/health
  http://${WINDOWS_HOST}:${RELAY_PORT}/health
  reported version: ${REPORTED_VERSION}
EOF
fi

cat <<'EOF'

Keep this script running while you test.
Stop it with Ctrl-C.
EOF

wait
