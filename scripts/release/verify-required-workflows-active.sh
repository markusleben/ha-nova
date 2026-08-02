#!/usr/bin/env bash
# Disabled workflows receive no events, so their required checks silently
# never appear and PRs hang BLOCKED forever (observed 2026-08-02 when
# cloud-source-gate and the Dependabot safe lane were disabled_manually).
# This gate fails loudly instead.
set -euo pipefail

REPO="${1:-${GITHUB_REPOSITORY:?repository (owner/name) is required}}"
POLICY_FILE="${2:-.github/policy/repo-policy.json}"

guarded=()
while IFS= read -r workflow; do
  guarded+=("${workflow}")
done < <(jq -er '.guarded_workflows[]' "${POLICY_FILE}")

if (( ${#guarded[@]} == 0 )); then
  echo "::error::${POLICY_FILE} guarded_workflows must list the workflows that deliver required checks"
  exit 1
fi

# Exit 3 = the token cannot read workflow states at all (callers may treat
# that as skippable). After this probe succeeds, a per-workflow failure is a
# renamed/deleted guarded file — real policy drift, so it stays exit 1.
if ! gh api "repos/${REPO}/actions/workflows?per_page=1" --jq '.total_count' >/dev/null 2>&1; then
  echo "::notice::cannot read workflow states for ${REPO} (gh unavailable or missing Actions: read)"
  exit 3
fi

failures=()
for workflow in "${guarded[@]}"; do
  file_name="$(basename "${workflow}")"
  state="$(gh api "repos/${REPO}/actions/workflows/${file_name}" --jq '.state')"
  if [[ "${state}" != "active" ]]; then
    failures+=("${file_name} (state=${state})")
  fi
done

if (( ${#failures[@]} > 0 )); then
  for failure in "${failures[@]}"; do
    echo "::error::guarded workflow is not active: ${failure} — restore it with: gh workflow enable ${failure%% *}"
  done
  exit 1
fi

echo "All ${#guarded[@]} guarded workflows are active."
