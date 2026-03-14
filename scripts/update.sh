#!/usr/bin/env bash
set -euo pipefail

find_runtime_binary() {
  local candidates=(
    "${HOME}/.local/bin/ha-nova"
    "${HOME}/.local/bin/ha-nova.exe"
    "${HOME}/.local/share/ha-nova/ha-nova"
    "${HOME}/.local/share/ha-nova/ha-nova.exe"
  )

  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -x "${candidate}" || ( -f "${candidate}" && "${candidate}" == *.exe ) ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

if runtime_bin="$(find_runtime_binary)"; then
  exec "${runtime_bin}" update "$@"
fi

if command -v go >/dev/null 2>&1 && [[ -f "${REPO_ROOT}/cli/main.go" ]]; then
  exec go run "${REPO_ROOT}/cli" update "$@"
fi

echo "[ha-nova:update] ERROR: no Go runtime found. Install HA NOVA first." >&2
exit 1
