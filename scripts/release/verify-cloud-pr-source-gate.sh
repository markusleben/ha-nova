#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PR_NUMBER="${HA_NOVA_CLOUD_GATE_PR_NUMBER:-}"

fail() {
  echo "[verify-cloud-pr-source-gate] ERROR: $*" >&2
  exit 1
}

[[ "${PR_NUMBER}" =~ ^[1-9][0-9]*$ ]] \
  || fail "HA_NOVA_CLOUD_GATE_PR_NUMBER must be a positive integer"

HA_NOVA_CLOUD_GATE_SOURCE_REF="refs/pull/${PR_NUMBER}/merge" \
  bash "${ROOT_DIR}/scripts/release/verify-cloud-target-source-gate.sh"
