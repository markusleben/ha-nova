#!/usr/bin/env bash
set -euo pipefail

find_runtime_binary() {
  local candidates=(
    "${HOME}/.local/bin/ha-nova"
    "${HOME}/.local/share/ha-nova/ha-nova"
  )

  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

if runtime_bin="$(find_runtime_binary)"; then
  exec "${runtime_bin}" uninstall "$@"
fi

if command -v go >/dev/null 2>&1 && [[ -f "${REPO_ROOT}/cli/main.go" ]]; then
  cd "${REPO_ROOT}/cli"
  exec go run . uninstall "$@"
fi

echo "[ha-nova:uninstall] ERROR: no Go runtime found. Install HA NOVA first." >&2
exit 1
