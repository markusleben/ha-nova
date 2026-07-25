#!/usr/bin/env bash
set -euo pipefail

REPO="${1:-markusleben/ha-nova}"
BRANCH="${2:-main}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
POLICY_FILE="${SCRIPT_DIR}/../../.github/policy/repo-policy.json"

if ! command -v gh >/dev/null 2>&1; then
  echo "::error::gh is required to verify GitHub branch protection."
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "::error::jq is required to verify GitHub branch protection."
  exit 1
fi

if [[ ! -f "${POLICY_FILE}" ]]; then
  echo "::error::Missing repo policy file at ${POLICY_FILE}."
  exit 1
fi

json="$(gh api "repos/${REPO}/branches/${BRANCH}/protection")"

expected_contexts_json="$(
  jq -c '.main_branch_protection.required_status_checks | sort' "${POLICY_FILE}"
)"
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

while IFS=$'\t' read -r context expected_app_id; do
  [[ -n "${context}" && -n "${expected_app_id}" ]] || continue
  [[ "${expected_app_id}" =~ ^[1-9][0-9]*$ ]] || {
    echo "::error::Required status check ${context} App id is not provisioned in repo-policy.json."
    exit 1
  }
  actual_app_ids="$(
    printf '%s' "${json}" \
      | jq -c --arg context "${context}" \
        '[.required_status_checks.checks[] | select(.context == $context) | .app_id] | unique'
  )"
  if [[ "${actual_app_ids}" != "[${expected_app_id}]" ]]; then
    echo "::error::Required status check ${context} must be pinned to GitHub App id ${expected_app_id}."
    echo "Actual app ids: ${actual_app_ids}"
    exit 1
  fi
done < <(
  jq -r \
    '.main_branch_protection.required_status_check_apps // {} | to_entries[] | [.key, (.value | tostring)] | @tsv' \
    "${POLICY_FILE}"
)

approvals="$(
  printf '%s' "${json}" \
  | jq -r '.required_pull_request_reviews.required_approving_review_count'
)"
expected_approvals="$(
  jq -r '.main_branch_protection.required_approving_review_count' "${POLICY_FILE}"
)"
if [[ "${approvals}" != "${expected_approvals}" ]]; then
  echo "::error::Expected exactly ${expected_approvals} required approving review(s), got ${approvals}."
  exit 1
fi

codeowners="$(
  printf '%s' "${json}" \
  | jq -r '.required_pull_request_reviews.require_code_owner_reviews'
)"
expected_codeowners="$(
  jq -r '.main_branch_protection.require_code_owner_reviews' "${POLICY_FILE}"
)"
if [[ "${codeowners}" != "${expected_codeowners}" ]]; then
  echo "::error::CODEOWNERS review must stay required on ${REPO}:${BRANCH}."
  exit 1
fi

for review_setting in dismiss_stale_reviews; do
  actual="$(
    printf '%s' "${json}" \
      | jq -r ".required_pull_request_reviews.${review_setting}"
  )"
  expected="$(
    jq -r ".main_branch_protection.${review_setting}" "${POLICY_FILE}"
  )"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "::error::Expected ${review_setting}=${expected} for ${REPO}:${BRANCH}, got ${actual}."
    exit 1
  fi
done

conversation_resolution="$(
  printf '%s' "${json}" \
  | jq -r '.required_conversation_resolution.enabled'
)"
expected_conversation_resolution="$(
  jq -r '.main_branch_protection.required_conversation_resolution' "${POLICY_FILE}"
)"
if [[ "${conversation_resolution}" != "${expected_conversation_resolution}" ]]; then
  echo "::error::Conversation resolution must stay enabled on ${REPO}:${BRANCH}."
  exit 1
fi

strict="$(
  printf '%s' "${json}" \
  | jq -r '.required_status_checks.strict'
)"
expected_strict="$(
  jq -r '.main_branch_protection.strict_required_status_checks' "${POLICY_FILE}"
)"
if [[ "${strict}" != "${expected_strict}" ]]; then
  echo "::error::Expected required status check strictness ${expected_strict} for ${REPO}:${BRANCH}, got ${strict}."
  exit 1
fi

mapfile -t advisory_checks < <(jq -r '.main_branch_protection.advisory_checks[]?' "${POLICY_FILE}")
for advisory_check in "${advisory_checks[@]}"; do
  if printf '%s' "${actual_contexts_json}" | grep -Fq "\"${advisory_check}\""; then
    echo "::error::${advisory_check} must remain advisory on ${REPO}:${BRANCH}."
    exit 1
  fi
done

echo "[verify-github-main-protection] OK: ${REPO}:${BRANCH}"
