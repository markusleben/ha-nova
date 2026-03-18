#!/usr/bin/env bash
set -euo pipefail

windows_shell_allows_exe() {
  case "${OSTYPE:-}" in
    msys*|cygwin*|win32*) return 0 ;;
  esac

  case "$(uname -s 2>/dev/null || true)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
  esac

  return 1
}

find_runtime_binary() {
  local candidates=(
    "${HOME}/.local/bin/ha-nova"
    "${HOME}/.local/bin/ha-nova.exe"
    "${HOME}/.local/share/ha-nova/ha-nova"
    "${HOME}/.local/share/ha-nova/ha-nova.exe"
  )

  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
    if windows_shell_allows_exe && [[ -f "${candidate}" && "${candidate}" == *.exe ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

if runtime_bin="$(find_runtime_binary)"; then
  exec "${runtime_bin}" check-update --quiet "$@"
fi

if command -v go >/dev/null 2>&1 && [[ -f "${REPO_ROOT}/cli/main.go" ]]; then
  cd "${REPO_ROOT}/cli"
  exec go run . check-update --quiet "$@"
fi

exit 0
