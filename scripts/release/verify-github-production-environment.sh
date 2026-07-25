#!/usr/bin/env bash
set -euo pipefail

REPO="${1:-markusleben/ha-nova}"
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
POLICY_FILE="${SCRIPT_DIR}/../../.github/policy/repo-policy.json"
API_VERSION="2026-03-10"

fail() {
  echo "[verify-github-production-environment] ERROR: $*" >&2
  exit 1
}

command -v gh >/dev/null 2>&1 || fail "gh is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
[[ -f "${POLICY_FILE}" ]] || fail "repo policy is missing"

environment_name="$(
  jq -er '.production_environment.name | select(type == "string" and length > 0)' \
    "${POLICY_FILE}"
)" || fail "production environment name policy must be a non-empty string"
expected_deployment_policy="$(
  jq -ec '.production_environment.deployment_branch_policy
    | select(
        type == "object"
        and keys == ["custom_branch_policies", "protected_branches"]
        and .protected_branches == false
        and .custom_branch_policies == true
      )' "${POLICY_FILE}"
)" || fail "production deployment policy must require custom policies only"
expected_branch_policies="$(
  jq -ec '.production_environment.deployment_branch_policies
    | select(
        type == "array"
        and length == 2
        and all(.[];
          type == "object"
          and keys == ["name", "type"]
          and (.name | type == "string" and length > 0)
          and (.type == "branch" or .type == "tag")
        )
      )
    | sort_by(.type, .name)' "${POLICY_FILE}"
)" || fail "production branch policies must be typed branch/tag objects"
expected_rule_types="$(
  jq -ec '.production_environment.protection_rule_types
    | select(type == "array")
    | sort' "${POLICY_FILE}"
)" || fail "production protection rule types must be an array"

environment_json="$(
  gh api \
    -H "X-GitHub-Api-Version: ${API_VERSION}" \
    "repos/${REPO}/environments/${environment_name}"
)" || fail "cannot read ${REPO} environment ${environment_name}"
actual_deployment_policy="$(
  jq -ec '.deployment_branch_policy
    | {
        protected_branches,
        custom_branch_policies
      }' <<<"${environment_json}"
)" || fail "production deployment branch policy is missing"
[[ "${actual_deployment_policy}" == "${expected_deployment_policy}" ]] \
  || fail "production must allow only custom main and v* deployment refs"

actual_rule_types="$(
  jq -ec '[.protection_rules[]?.type] | sort' <<<"${environment_json}"
)" || fail "production protection rules are malformed"
[[ "${actual_rule_types}" == "${expected_rule_types}" ]] \
  || fail "production protection rule types drifted"

branch_policies_json="$(
  gh api \
    -H "X-GitHub-Api-Version: ${API_VERSION}" \
    --paginate \
    --slurp \
    "repos/${REPO}/environments/${environment_name}/deployment-branch-policies?per_page=100"
)" || fail "cannot read production deployment branch policies"
actual_branch_policies="$(
  jq -ec '[
      .[]
      | if type == "array" then .[] else . end
      | .branch_policies[]?
      | {name, type}
    ] | sort_by(.type, .name)' \
    <<<"${branch_policies_json}"
)" || fail "production deployment branch policies are malformed"
[[ "${actual_branch_policies}" == "${expected_branch_policies}" ]] \
  || fail "production deployment refs must be exactly branch main and tag v* (expected ${expected_branch_policies}, got ${actual_branch_policies})"

echo "[verify-github-production-environment] OK: ${REPO}:${environment_name}"
