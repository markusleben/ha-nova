#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ "$#" -eq 0 ]]; then
  set -- \
    "${ROOT_DIR}/.github/workflows/release.yml" \
    "${ROOT_DIR}/.github/workflows/release-candidate.yml"
fi

exec node "${ROOT_DIR}/scripts/release/verify-cloud-workflow-gate.mjs" "$@"
