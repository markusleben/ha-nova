#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/onboarding/macos-onboarding.sh setup
  bash scripts/onboarding/macos-onboarding.sh doctor
  bash scripts/onboarding/macos-onboarding.sh env

Commands:
  setup   Interactive setup. Stores secrets in macOS Keychain.
  doctor  Run onboarding diagnostics and show what is missing.
  env     Print shell exports from stored config + Keychain secrets.
USAGE
}

main() {
  local command="${1:-setup}"
  shift || true

  case "$command" in
    setup)
      exec bash "${SCRIPT_DIR}/macos-setup.sh" "$@"
      ;;
    doctor)
      exec bash "${SCRIPT_DIR}/macos-doctor.sh" "$@"
      ;;
    env)
      exec bash "${SCRIPT_DIR}/macos-env.sh" "$@"
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      usage
      echo "[macos-onboarding] Unknown command: $command" >&2
      exit 1
      ;;
  esac
}

main "$@"
