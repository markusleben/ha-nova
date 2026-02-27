#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/onboarding/macos-onboarding.sh setup
  bash scripts/onboarding/macos-onboarding.sh doctor
  bash scripts/onboarding/macos-onboarding.sh ready
  bash scripts/onboarding/macos-onboarding.sh env
  bash scripts/onboarding/macos-onboarding.sh quick

Commands:
  setup   Interactive setup. Stores relay token in macOS Keychain.
  doctor  Run onboarding diagnostics (verifies Relay upstream WS validation).
  ready   Fast readiness check (cached doctor pass; invalidates on secret/config changes).
  env     Print shell exports from stored config + required Keychain relay token.
  quick   Fast readiness check for fresh Codex sessions.
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
    ready)
      exec bash "${SCRIPT_DIR}/macos-ready.sh" "$@"
      ;;
    env)
      exec bash "${SCRIPT_DIR}/macos-env.sh" "$@"
      ;;
    quick)
      exec bash "${SCRIPT_DIR}/macos-quick.sh" "$@"
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
