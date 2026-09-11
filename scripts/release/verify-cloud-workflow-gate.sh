#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ "$#" -eq 0 ]]; then
  set -- \
    "${ROOT_DIR}/.github/workflows/release.yml" \
    "${ROOT_DIR}/.github/workflows/release-candidate.yml" \
    "${ROOT_DIR}/.github/workflows/cloud-candidate-bundle.yml" \
    "${ROOT_DIR}/.github/workflows/cloud-source-gate.yml" \
    "${ROOT_DIR}/.github/workflows/ci.yml" \
    "${ROOT_DIR}/.github/workflows/e2e-disposable-ha.yml" \
    "${ROOT_DIR}/.github/workflows/codeql.yml" \
    "${ROOT_DIR}/.github/workflows/dependency-review.yml" \
    "${ROOT_DIR}/.github/workflows/manifest-review-gate.yml" \
    "${ROOT_DIR}/.github/workflows/pairing-e2e.yml" \
    "${ROOT_DIR}/.github/workflows/pr-review-watchdog.yml" \
    "${ROOT_DIR}/.github/workflows/relay-image.yml" \
    "${ROOT_DIR}/.github/workflows/release-pipeline-audit.yml"
fi
# Not listed on purpose: codex-review-gate.yml, dependabot-safe-auto-merge.yml,
# readme-release-gate.yml (no actions), dependabot-safe-lane-prepare.yml (a
# `uses:` string inside a run: block trips the line matcher) — every action-
# bearing workflow is covered.

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
node "${ROOT_DIR}/scripts/release/verify-cloud-candidate-workflow.mjs" \
  "${ROOT_DIR}/.github/workflows/cloud-candidate-bundle.yml" \
  "${ROOT_DIR}/scripts/release/resolve-cloud-candidate-source.sh"
exec node "${ROOT_DIR}/scripts/release/verify-cloud-workflow-gate.mjs" \
  "${release_workflows[@]}"
