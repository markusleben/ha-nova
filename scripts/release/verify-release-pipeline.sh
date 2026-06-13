#!/usr/bin/env bash
# Verify the release pipeline contract.
#
# Two layers:
#   1. Static workflow invariants every release depends on (no GitHub
#      permissions; runs in CI and locally).
#   2. The live GitHub tag-ruleset state that makes "release tags are
#      maintainer-pushed, the RC workflow never auto-publishes" actually true.
#
# The live layer auto-skips entirely with a notice when the current token cannot
# read rulesets at all (e.g. a token with no repo access), so the same script
# serves both as a maintainer preflight command and as the weekly audit.
# Verifying the no-App-bypass guard additionally needs *write* access to the
# ruleset — GitHub only returns bypass_actors to requesters with write access.
# When the token can read the ruleset but not bypass_actors (e.g. the default CI
# GITHUB_TOKEN), the no-App-bypass guard is skipped with a notice so the weekly
# audit stays green; the release preflight sets
# HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 to make that case fail closed instead.
#
# Background: the v0.5.0 release exposed two pipeline traps — a GoReleaser tag
# auto-detection footgun when an rc tag and the final tag share a commit, and
# an RC workflow that tried to publish via a path the tag ruleset blocks. This
# check pins both so they cannot silently come back.
set -euo pipefail

REPO="${1:-markusleben/ha-nova}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
POLICY_FILE="${ROOT_DIR}/.github/policy/repo-policy.json"

# The no-App-bypass guard needs ruleset write access to see bypass_actors. By
# default it skips with a notice when the token cannot see them, so the weekly
# audit on the read-only default token does not go red on every run. The
# release preflight sets HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 to make that
# case fail closed, so a release never proceeds with the guard unverified.
REQUIRE_BYPASS="${HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS:-0}"

fail() {
  echo "::error::$*" >&2
  exit 1
}
note() { echo "::notice::$*"; }

# Skip a live check when it cannot run — but never in strict mode. The release
# preflight sets HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 precisely to prove the
# live tag-ruleset / no-App-bypass guard, so any inability to run it there is a
# hard failure rather than a green "static checks only" pass.
skip_or_fail() {
  if [[ "${REQUIRE_BYPASS}" == "1" ]]; then
    fail "$* Strict release preflight (HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1) requires the live tag-ruleset contract to be fully verified; run as a maintainer with admin 'gh auth'."
  fi
  note "$* Skipping the live tag-ruleset drift check."
  exit 0
}

command -v jq >/dev/null 2>&1 || fail "jq is required to verify the release pipeline contract."
[[ -f "${POLICY_FILE}" ]] || fail "Missing repo policy file at ${POLICY_FILE}."

release_workflow="${ROOT_DIR}/.github/workflows/release.yml"
rc_workflow="${ROOT_DIR}/.github/workflows/release-candidate.yml"
goreleaser="${ROOT_DIR}/.goreleaser.yml"

# --- Static workflow contract (no GitHub permissions required) --------------
[[ -f "${release_workflow}" ]] || fail "Missing ${release_workflow}."
[[ -f "${goreleaser}" ]] || fail "Missing ${goreleaser}."

# Pin GoReleaser to the triggering tag: an rc tag and the final tag can share a
# commit, and without this GoReleaser auto-detects a tag and publishes onto the
# wrong release (the live v0.5.0 failure).
grep -Fq 'GORELEASER_CURRENT_TAG: ${{ github.ref_name }}' "${release_workflow}" \
  || fail "release.yml must pin GORELEASER_CURRENT_TAG to github.ref_name."

# Never publish a tag that is not on main.
grep -Fq 'git merge-base --is-ancestor' "${release_workflow}" \
  || fail "release.yml must verify the tag is an ancestor of main before publishing."

# -rcN dress-rehearsal tags must publish as a prerelease, not a stable release.
grep -Eq '^[[:space:]]*prerelease:[[:space:]]*auto[[:space:]]*$' "${goreleaser}" \
  || fail ".goreleaser.yml must set 'prerelease: auto' so -rcN tags publish as prereleases."

# The RC workflow must not try to publish: the v* tag ruleset blocks the Actions
# token, so any automated publish only ever 422s. Real RC publishing is the
# tag-first rehearsal driven by release.yml.
if [[ -f "${rc_workflow}" ]] && grep -Eq 'gh release (create|edit|upload)' "${rc_workflow}"; then
  fail "release-candidate.yml must not publish releases (the v* tag ruleset blocks it). Use the tag-first rehearsal: push vX.Y.Z-rcN and let release.yml publish."
fi

echo "[verify-release-pipeline] static workflow contract OK"

# --- Live tag-ruleset contract (admin token; auto-skips unless strict) -------
if ! command -v gh >/dev/null 2>&1; then
  skip_or_fail "gh not available, so the live tag-ruleset contract cannot be checked."
fi

rulesets_json="$(gh api "repos/${REPO}/rulesets" 2>/dev/null || true)"
if [[ -z "${rulesets_json}" ]] || ! printf '%s' "${rulesets_json}" | jq -e 'type == "array"' >/dev/null 2>&1; then
  skip_or_fail "Cannot read ${REPO} rulesets with the current token (run with a maintainer 'gh auth' or set RELEASE_AUDIT_TOKEN)."
fi

expected_name="$(jq -r '.release_tag_protection.name' "${POLICY_FILE}")"
ruleset_id="$(printf '%s' "${rulesets_json}" | jq -r --arg n "${expected_name}" 'map(select(.name == $n)) | (.[0].id // empty)')"
[[ -n "${ruleset_id}" ]] \
  || fail "Tag ruleset '${expected_name}' is missing — v* release tags are unprotected."

ruleset="$(gh api "repos/${REPO}/rulesets/${ruleset_id}" 2>/dev/null || true)"
if [[ -z "${ruleset}" ]] || ! printf '%s' "${ruleset}" | jq -e '.rules' >/dev/null 2>&1; then
  skip_or_fail "Cannot read ruleset ${ruleset_id} detail with the current token."
fi

[[ "$(printf '%s' "${ruleset}" | jq -r '.enforcement')" == "active" ]] \
  || fail "Tag ruleset '${expected_name}' is not active."
[[ "$(printf '%s' "${ruleset}" | jq -r '.target')" == "tag" ]] \
  || fail "Tag ruleset '${expected_name}' must target tags."

expected_ref="$(jq -r '.release_tag_protection.ref_include' "${POLICY_FILE}")"
printf '%s' "${ruleset}" \
  | jq -e --arg r "${expected_ref}" '(.conditions.ref_name.include | index($r)) != null' >/dev/null \
  || fail "Tag ruleset '${expected_name}' must include ${expected_ref}."

missing_rules="$(printf '%s' "${ruleset}" | jq -r --slurpfile p "${POLICY_FILE}" '
  ($p[0].release_tag_protection.required_rules - [.rules[].type]) | join(", ")
')"
[[ -z "${missing_rules}" ]] \
  || fail "Tag ruleset '${expected_name}' is missing required rule(s): ${missing_rules}. The 'creation' rule is what blocks the Actions token from minting v* tags."

# GitHub only returns bypass_actors to requesters with write access to the
# ruleset; a read-only token omits the field entirely. Treating an absent field
# as an empty list would silently hide the exact App bypass this guard catches,
# so never enforce against an absent field. Skip with a notice by default (the
# read-only weekly audit), and fail closed when the release preflight asks for
# strict verification.
if printf '%s' "${ruleset}" | jq -e 'has("bypass_actors")' >/dev/null 2>&1; then
  forbidden_present="$(printf '%s' "${ruleset}" | jq -r --slurpfile p "${POLICY_FILE}" '
    [.bypass_actors[].actor_type] as $have
    | [$p[0].release_tag_protection.forbidden_bypass_actor_types[] | select(. as $t | $have | index($t))]
    | join(", ")
  ')"
  [[ -z "${forbidden_present}" ]] \
    || fail "Tag ruleset '${expected_name}' grants a forbidden bypass actor type (${forbidden_present}); automated v* tag creation is no longer blocked. Update the release flow contract deliberately."
  bypass_status="verified"
elif [[ "${REQUIRE_BYPASS}" == "1" ]]; then
  fail "Cannot read bypass actors for '${expected_name}' — GitHub returns them only to requesters with write access to the ruleset. Run as a maintainer (admin 'gh auth') or give RELEASE_AUDIT_TOKEN ruleset write access; the no-App-bypass guard is required for a release."
else
  note "bypass_actors not visible for '${expected_name}' (token lacks ruleset write access); skipping the no-App-bypass guard. Run with a maintainer token or set HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 to enforce it."
  bypass_status="skipped (read-only token)"
fi

echo "[verify-release-pipeline] live tag-ruleset contract OK (${expected_name}; bypass guard: ${bypass_status})"
echo "[verify-release-pipeline] OK: ${REPO}"
