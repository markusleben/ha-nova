#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=lib/runtime.sh
. "${REPO_ROOT}/scripts/lib/runtime.sh"

if runtime_bin="$(find_runtime_binary)"; then
  exec "${runtime_bin}" update "$@"
fi

if exec_repo_dev_runtime update "$@"; then
  exit 0
fi

echo "[ha-nova:update] ERROR: no Go runtime found. Install HA NOVA first." >&2
exit 1
