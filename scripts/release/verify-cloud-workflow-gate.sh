#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ "$#" -eq 0 ]]; then
  set -- \
    "${ROOT_DIR}/.github/workflows/release.yml" \
    "${ROOT_DIR}/.github/workflows/release-candidate.yml" \
    "${ROOT_DIR}/.github/workflows/cloud-source-gate.yml" \
    "${ROOT_DIR}/.github/workflows/ci.yml" \
    "${ROOT_DIR}/.github/workflows/e2e-disposable-ha.yml"
fi

node "${ROOT_DIR}/scripts/release/verify-cloud-action-pins.mjs" "$@"

release_workflows=()
for workflow in "$@"; do
  case "$(basename "${workflow}")" in
    release.yml|release-candidate.yml)
      release_workflows+=("${workflow}")
      ;;
  esac
done
[[ "${#release_workflows[@]}" -gt 0 ]] || {
  echo "[verify-cloud-workflow-gate] ERROR: release workflows are required" >&2
  exit 1
}
exec node "${ROOT_DIR}/scripts/release/verify-cloud-workflow-gate.mjs" \
  "${release_workflows[@]}"
