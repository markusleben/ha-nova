#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUNDLE_SERVER_PORT="${BUNDLE_SERVER_PORT:-8917}"
SERVER_LOG="$(mktemp "${TMPDIR:-/tmp}/ha-nova-macos-private-rc-server.XXXXXX.log")"

ensure_port_free() {
  local port="$1"
  local listeners
  listeners="$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -n "${listeners}" ]]; then
    echo "Private RC bundle server port ${port} is already in use." >&2
    echo "Stop the old process first or rerun with a different BUNDLE_SERVER_PORT." >&2
    echo >&2
    echo "${listeners}" >&2
    exit 1
  fi
}

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
  rm -f "${SERVER_LOG}"
}
trap cleanup EXIT

wait_for_server() {
  local url="http://127.0.0.1:${BUNDLE_SERVER_PORT}/"
  local attempts=20

  for ((i = 1; i <= attempts; i++)); do
    if ! kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
      echo "Private RC bundle server exited early." >&2
      cat "${SERVER_LOG}" >&2 || true
      exit 1
    fi
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done

  echo "Private RC bundle server did not become ready." >&2
  cat "${SERVER_LOG}" >&2 || true
  exit 1
}

cd "${ROOT_DIR}"
npm run release:rc:local
ensure_port_free "${BUNDLE_SERVER_PORT}"
python3 -m http.server "${BUNDLE_SERVER_PORT}" --directory dist/install-bundles >"${SERVER_LOG}" 2>&1 &
SERVER_PID=$!
wait_for_server

export BUNDLE_SERVER_BASE_URL="http://127.0.0.1:${BUNDLE_SERVER_PORT}"

bash scripts/dev/macos-private-rc-smoke.sh
bash scripts/dev/macos-private-rc-setup-all.sh
bash scripts/dev/macos-private-rc-client.sh codex
bash scripts/dev/macos-private-rc-client.sh opencode
bash scripts/dev/macos-private-rc-client.sh gemini
bash scripts/dev/macos-private-rc-client.sh hermes
bash scripts/dev/macos-private-rc-client.sh claude
