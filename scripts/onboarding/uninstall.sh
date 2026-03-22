#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=../lib/runtime.sh
. "${REPO_ROOT}/scripts/lib/runtime.sh"

if runtime_bin="$(find_runtime_binary)"; then
  exec "${runtime_bin}" uninstall "$@"
fi

if exec_repo_dev_runtime uninstall "$@"; then
  exit 0
fi

echo "[ha-nova:uninstall] ERROR: no Go runtime found. Install HA NOVA first." >&2
exit 1
