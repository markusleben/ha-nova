#!/usr/bin/env bash
set -euo pipefail

REPO="${1:-markusleben/ha-nova}"
BRANCH="${2:-main}"

if ! command -v gh >/dev/null 2>&1; then
  echo "::error::gh is required to verify GitHub branch protection."
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "::error::jq is required to verify GitHub branch protection."
  exit 1
fi

json="$(gh api "repos/${REPO}/branches/${BRANCH}/protection")"

expected_contexts_json='["analyze","ci-gate","dependency-review","manifest-review-gate"]'
actual_contexts_json="$(
  printf '%s' "${json}" \
    | jq -c '.required_status_checks.contexts | sort'
)"

if [[ "${actual_contexts_json}" != "${expected_contexts_json}" ]]; then
  echo "::error::Required status checks drifted for ${REPO}:${BRANCH}."
  echo "Expected: ${expected_contexts_json}"
  echo "Actual:   ${actual_contexts_json}"
  exit 1
fi

approvals="$(
  printf '%s' "${json}" \
    | jq -r '.required_pull_request_reviews.required_approving_review_count'
)"
if [[ "${approvals}" != "1" ]]; then
  echo "::error::Expected exactly 1 required approving review, got ${approvals}."
  exit 1
fi

codeowners="$(
  printf '%s' "${json}" \
    | jq -r '.required_pull_request_reviews.require_code_owner_reviews'
)"
if [[ "${codeowners}" != "true" ]]; then
  echo "::error::CODEOWNERS review must stay required on ${REPO}:${BRANCH}."
  exit 1
fi

conversation_resolution="$(
  printf '%s' "${json}" \
    | jq -r '.required_conversation_resolution.enabled'
)"
if [[ "${conversation_resolution}" != "true" ]]; then
  echo "::error::Conversation resolution must stay enabled on ${REPO}:${BRANCH}."
  exit 1
fi

strict="$(
  printf '%s' "${json}" \
    | jq -r '.required_status_checks.strict'
)"
if [[ "${strict}" != "false" ]]; then
  echo "::error::Expected non-strict required status checks for ${REPO}:${BRANCH}, got ${strict}."
  exit 1
fi

if printf '%s' "${actual_contexts_json}" | grep -Fq '"codex-review-gate"'; then
  echo "::error::codex-review-gate must remain advisory on ${REPO}:${BRANCH}."
  exit 1
fi

echo "[verify-github-main-protection] OK: ${REPO}:${BRANCH}"
