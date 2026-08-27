#!/usr/bin/env bash
set -euo pipefail

REPO="${GITHUB_REPOSITORY:-}"
PR_NUMBER="${1:-}"
POLICY_FILE="${2:-.github/policy/repo-policy.json}"

fail() {
  echo "[resolve-cloud-candidate-source] ERROR: $*" >&2
  exit 1
}

require_sha() {
  [[ "$1" =~ ^[0-9a-f]{40}$ ]] || fail "$2 must be a full lowercase SHA-1"
}

resolve_remote_sha() {
  local ref="$1" label="$2" remote_line remote_sha remote_name extra
  remote_line="$(git ls-remote --refs origin "${ref}")"
  [[ -n "${remote_line}" && "${remote_line}" != *$'\n'* ]] \
    || fail "${label} must resolve exactly once"
  read -r remote_sha remote_name extra <<<"${remote_line}"
  require_sha "${remote_sha:-}" "${label} SHA"
  [[ "${remote_name:-}" == "${ref}" && -z "${extra:-}" ]] \
    || fail "${label} response is invalid"
  printf '%s\n' "${remote_sha}"
}

[[ "${GITHUB_EVENT_NAME:-}" == "workflow_dispatch" ]] \
  || fail "candidate builds require workflow_dispatch"
[[ "${GITHUB_REF:-}" == "refs/heads/main" ]] \
  || fail "candidate builds must run from main"
[[ "${REPO}" == "markusleben/ha-nova" ]] \
  || fail "candidate builds are pinned to markusleben/ha-nova"
[[ "${GITHUB_ACTOR:-}" == "markusleben" \
  && "${GITHUB_ACTOR_ID:-}" == "6522814" ]] \
  || fail "candidate dispatch requires the exact maintainer identity"
[[ "${GITHUB_TRIGGERING_ACTOR:-}" == "markusleben" \
  && "${GITHUB_RUN_ATTEMPT:-}" == "1" ]] \
  || fail "candidate builds cannot be rerun or triggered by another account"
require_sha "${GITHUB_SHA:-}" "GITHUB_SHA"
trusted_head="$(git rev-parse --verify HEAD)"
require_sha "${trusted_head}" "trusted checkout HEAD"
[[ "${trusted_head}" == "${GITHUB_SHA}" ]] \
  || fail "trusted checkout must equal the main workflow SHA"
[[ "${PR_NUMBER}" =~ ^[1-9][0-9]*$ ]] \
  || fail "pull request number must be a positive integer"
[[ -f "${POLICY_FILE}" ]] || fail "repository policy is missing"
[[ -n "${GITHUB_OUTPUT:-}" ]] || fail "GITHUB_OUTPUT is required"
command -v gh >/dev/null 2>&1 || fail "gh is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"

pr="$(gh api "repos/${REPO}/pulls/${PR_NUMBER}")"
jq -e '
  type == "object"
  and .state == "open"
  and .draft == false
  and .base.ref == "main"
  and .base.repo.full_name == "markusleben/ha-nova"
  and .head.repo.full_name == "markusleben/ha-nova"
' >/dev/null <<<"${pr}" \
  || fail "pull request must be open, ready, same-repository, and target main"

base_sha="$(jq -r '.base.sha' <<<"${pr}")"
head_sha="$(jq -r '.head.sha' <<<"${pr}")"
merge_sha="$(jq -r '.merge_commit_sha // empty' <<<"${pr}")"
require_sha "${base_sha}" "pull request base SHA"
require_sha "${head_sha}" "pull request head SHA"
require_sha "${merge_sha}" "pull request merge commit SHA"
[[ "${base_sha}" == "${GITHUB_SHA}" ]] \
  || fail "pull request base must equal the current main workflow SHA"

source_ref="refs/pull/${PR_NUMBER}/merge"
remote_main_sha="$(resolve_remote_sha refs/heads/main "remote main")"
[[ "${remote_main_sha}" == "${GITHUB_SHA}" ]] \
  || fail "workflow SHA is no longer current main"

remote_sha="$(resolve_remote_sha "${source_ref}" "pull request merge ref")"
[[ "${remote_sha}" == "${merge_sha}" ]] \
  || fail "pull request API and merge ref disagree"

commit="$(gh api "repos/${REPO}/git/commits/${merge_sha}")"
tree_sha="$(jq -r '.tree.sha' <<<"${commit}")"
require_sha "${tree_sha}" "pull request merge tree SHA"
jq -e \
  --arg merge "${merge_sha}" \
  --arg base "${base_sha}" \
  --arg head "${head_sha}" '
    .sha == $merge
    and (.parents | type == "array" and length == 2)
    and .parents[0].sha == $base
    and .parents[1].sha == $head
  ' >/dev/null <<<"${commit}" \
  || fail "pull request merge commit does not bind the current base and head"
git merge-base --is-ancestor "${base_sha}" "${head_sha}" \
  || fail "pull request head must contain current main"

source_app_id="$(
  jq -r '.main_branch_protection.required_status_check_apps["cloud-source-gate"]' \
    "${POLICY_FILE}"
)"
[[ "${source_app_id}" =~ ^[1-9][0-9]*$ ]] \
  || fail "cloud-source-gate App ID is not provisioned"
github_actions_app_id=15368

verify_checks() {
  local check_pages status_pages workflow_pages checks statuses
  local check_name expected_workflow expected_event
  workflow_pages="$(
    gh api --paginate --slurp \
      "repos/${REPO}/actions/runs?head_sha=${head_sha}&per_page=100"
  )"
  status_pages="$(
    gh api --paginate --slurp \
      "repos/${REPO}/commits/${head_sha}/status?per_page=100"
  )"
  check_pages="$(
    gh api --paginate --slurp \
      "repos/${REPO}/commits/${head_sha}/check-runs?filter=latest&per_page=100"
  )"
  checks="$(jq -c '[.[].check_runs[]]' <<<"${check_pages}")"
  statuses="$(jq -c '[.[].statuses[]]' <<<"${status_pages}")"

  while IFS= read -r check_name; do
    [[ -n "${check_name}" ]] || continue
    case "${check_name}" in
      analyze) expected_workflow=".github/workflows/codeql.yml"; expected_event="pull_request" ;;
      ci-gate) expected_workflow=".github/workflows/ci.yml"; expected_event="pull_request" ;;
      dependency-review) expected_workflow=".github/workflows/dependency-review.yml"; expected_event="pull_request" ;;
      manifest-review-gate) expected_workflow=".github/workflows/manifest-review-gate.yml"; expected_event="pull_request_target" ;;
      readme-release-gate) expected_workflow=".github/workflows/readme-release-gate.yml"; expected_event="pull_request_target" ;;
      codex-review-gate) expected_workflow=".github/workflows/codex-review-gate.yml"; expected_event="pull_request" ;;
      *) fail "required pre-evidence check ${check_name} has no trusted workflow binding" ;;
    esac
    jq -e \
      --arg name "${check_name}" \
      --arg workflow "${expected_workflow}" \
      --arg event "${expected_event}" \
      --argjson app_id "${github_actions_app_id}" \
      --argjson pr "${PR_NUMBER}" \
      --arg base "${base_sha}" \
      --arg head "${head_sha}" \
      --slurpfile workflows <(
        jq -c '[.[].workflow_runs[]]' <<<"${workflow_pages}"
      ) '
        (
          [ $workflows[0][]
            | select(
                .path == $workflow
                and .event == $event
                and .head_sha == $head
                and any(
                  .pull_requests[];
                  .number == $pr
                  and .base.sha == $base
                  and .head.sha == $head
                )
              )
          ] | max_by(.id)
        ) as $latest_workflow
        | (
          [ .[]
            | select(
                .name == $name
                and .app.id == $app_id
                and .head_sha == $head
                and any(
                  .pull_requests[];
                  .number == $pr
                  and .base.sha == $base
                  and .head.sha == $head
                )
              )
          ] | max_by(.id)
        ) as $latest
        | (
            $latest.details_url
            | capture(
                "^https://github\\.com/markusleben/ha-nova/actions/runs/(?<run>[0-9]+)/job/[0-9]+$"
              ).run
          ) as $run
        | $latest != null
          and $latest_workflow != null
          and $latest.status == "completed"
          and (
            $latest.conclusion == "success"
            or $latest.conclusion == "skipped"
            or $latest.conclusion == "neutral"
          )
          and ($latest_workflow.id | tostring) == $run
          and $latest_workflow.status == "completed"
          and $latest_workflow.conclusion == "success"
      ' >/dev/null <<<"${checks}" \
      || fail "required pre-evidence check ${check_name} is not successful"
    jq -e --arg name "${check_name}" '
      [ .[] | select(.context == $name) ] | length == 0
    ' >/dev/null <<<"${statuses}" \
      || fail "required pre-evidence check ${check_name} must not be shadowed by a commit status"
  done < <(
    jq -r '
      .main_branch_protection
      | ((.required_status_checks - ["cloud-source-gate"]) + .advisory_checks)
      | unique[]
    ' "${POLICY_FILE}"
  )

  jq -e \
    --argjson app_id "${source_app_id}" \
    --argjson pr "${PR_NUMBER}" \
    --arg base "${base_sha}" \
    --arg head "${head_sha}" \
    --arg target "workflow-run:" \
    --arg suffix ":target:${merge_sha}" '
      [ .[]
        | select(
            .name == "cloud-source-gate"
            and .app.id == $app_id
            and .head_sha == $head
            and (.external_id | type == "string")
            and (.external_id | startswith($target) and endswith($suffix))
            and any(
              .pull_requests[];
              .number == $pr
              and .base.sha == $base
              and .head.sha == $head
            )
          )
      ] | max_by(.id) as $latest
      | $latest != null
        and $latest.status == "completed"
        and $latest.conclusion == "failure"
    ' >/dev/null <<<"${checks}" \
    || fail "cloud-source-gate must be the sole expected failed evidence check"
  jq -e '
    [ .[] | select(.context == "cloud-source-gate") ] | length == 0
  ' >/dev/null <<<"${statuses}" \
    || fail "cloud-source-gate must not be shadowed by a commit status"
}

verify_checks

issue_comment_pages="$(
  gh api --paginate --slurp \
    "repos/${REPO}/issues/${PR_NUMBER}/comments?per_page=100"
)"
review_pages="$(
  gh api --paginate --slurp \
    "repos/${REPO}/pulls/${PR_NUMBER}/reviews?per_page=100"
)"
inline_comment_pages="$(
  gh api --paginate --slurp \
    "repos/${REPO}/pulls/${PR_NUMBER}/comments?per_page=100"
)"
reaction_pages="$(
  gh api --paginate --slurp \
    "repos/${REPO}/issues/${PR_NUMBER}/reactions?per_page=100"
)"
thread_pages="$(
  gh api graphql --paginate --slurp \
    -f owner=markusleben \
    -f repo=ha-nova \
    -F number="${PR_NUMBER}" \
    -f query='
      query($owner: String!, $repo: String!, $number: Int!, $endCursor: String) {
        repository(owner: $owner, name: $repo) {
          pullRequest(number: $number) {
            reviewDecision
            reviewThreads(first: 100, after: $endCursor) {
              nodes { isResolved }
              pageInfo { hasNextPage endCursor }
            }
          }
        }
      }
    '
)"
head_prefix="${head_sha:0:10}"
verdict_at="$(
  jq -r --arg prefix "${head_prefix}" '
    [ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and .user.type == "Bot"
          and (.body | contains("**Reviewed commit:** `" + $prefix + "`"))
          and (
            (.body | contains("Codex Review: Didn\u0027t find any major issues"))
            or (.body | contains("Here are some automated review suggestions"))
          )
        )
      | .created_at
    ]
    | sort
    | last // empty
  ' <<<"${issue_comment_pages}"
)"
[[ "${verdict_at}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T ]] \
  || fail "current pull request head lacks a real Codex bot verdict"
verdict_clean="$(
  jq -r --arg prefix "${head_prefix}" --arg at "${verdict_at}" '
    [ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and .user.type == "Bot"
          and .created_at == $at
          and (.body | contains("**Reviewed commit:** `" + $prefix + "`"))
        )
    ]
    | any(.[]; .body | contains("Codex Review: Didn\u0027t find any major issues"))
  ' <<<"${issue_comment_pages}"
)"
jq -e '
  length > 0
  and all(.[];
    .data.repository.pullRequest != null
    and (
      .data.repository.pullRequest.reviewDecision == "APPROVED"
      or .data.repository.pullRequest.reviewDecision == "REVIEW_REQUIRED"
      or .data.repository.pullRequest.reviewDecision == null
    )
    and all(
      .data.repository.pullRequest.reviewThreads.nodes[];
      .isResolved == true
    )
  )
' >/dev/null <<<"${thread_pages}" \
  || fail "pull request has requested changes or unresolved review threads"
if [[ "${verdict_clean}" != "true" ]]; then
  jq -e --arg head "${head_sha}" '
    ([ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and .user.type == "Bot"
          and .commit_id == $head
        )
    ] | length) > 0
  ' >/dev/null <<<"${inline_comment_pages}" \
    || fail "findings verdict carries no head-bound triageable finding"
fi
jq -e --arg verdict_at "${verdict_at}" --arg head "${head_sha}" '
  all(
    [ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and .user.type == "Bot"
        )
    ][];
    (.commit_id != $head) or (.submitted_at <= $verdict_at)
  )
' >/dev/null <<<"${review_pages}" \
  || fail "a later Codex review supersedes the verdict"
jq -e --arg verdict_at "${verdict_at}" --arg head "${head_sha}" '
  all(
    [ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and .user.type == "Bot"
        )
    ][];
    (.commit_id != $head) or (.created_at <= $verdict_at)
  )
' >/dev/null <<<"${inline_comment_pages}" \
  || fail "a later Codex inline finding supersedes the verdict"
jq -e --arg verdict_at "${verdict_at}" --arg prefix "${head_prefix}" '
  all(
    [ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and .user.type == "Bot"
        )
    ][];
    (.created_at <= $verdict_at)
    or (
      (.body | contains("**Reviewed commit:** `" + $prefix + "`"))
      and (
        (.body | contains("Codex Review: Didn\u0027t find any major issues"))
        or (.body | contains("Here are some automated review suggestions"))
      )
    )
  )
' >/dev/null <<<"${issue_comment_pages}" \
  || fail "a later Codex issue result supersedes the verdict"
jq -e --arg verdict_at "${verdict_at}" '
  all(
    [ .[][]
      | select(
          .user.login == "chatgpt-codex-connector[bot]"
          and .user.id == 199175422
          and (.user.type == "User" or .user.type == "Bot")
        )
    ][];
    (.created_at <= $verdict_at) or .content == "+1"
  )
' >/dev/null <<<"${reaction_pages}" \
  || fail "a later Codex reaction supersedes the verdict"

HA_NOVA_CLOUD_GATE_SOURCE_REF="${source_ref}" \
HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT="${merge_sha}" \
HA_NOVA_CLOUD_GATE_EXPECTED_HEAD_COMMIT="${head_sha}" \
HA_NOVA_CLOUD_GATE_EXPECTED_BASE_COMMIT="${base_sha}" \
  bash scripts/release/verify-cloud-target-source-gate.sh candidate

expected_commit="${HA_NOVA_CLOUD_CANDIDATE_EXPECTED_COMMIT:-}"
expected_tree="${HA_NOVA_CLOUD_CANDIDATE_EXPECTED_TREE:-}"
expected_base="${HA_NOVA_CLOUD_CANDIDATE_EXPECTED_BASE:-}"
expected_head="${HA_NOVA_CLOUD_CANDIDATE_EXPECTED_HEAD:-}"
if [[ -n "${expected_commit}${expected_tree}${expected_base}${expected_head}" ]]; then
  [[ -n "${expected_commit}" && -n "${expected_tree}" \
    && -n "${expected_base}" && -n "${expected_head}" ]] \
    || fail "expected candidate identity must be provided completely"
  require_sha "${expected_commit}" "expected candidate commit"
  require_sha "${expected_tree}" "expected candidate tree"
  require_sha "${expected_base}" "expected candidate base"
  require_sha "${expected_head}" "expected candidate head"
  [[ "${merge_sha}" == "${expected_commit}" \
    && "${tree_sha}" == "${expected_tree}" \
    && "${base_sha}" == "${expected_base}" \
    && "${head_sha}" == "${expected_head}" ]] \
    || fail "candidate identity changed since the initial resolution"
fi

verify_checks

latest_pr="$(gh api "repos/${REPO}/pulls/${PR_NUMBER}")"
jq -e \
  --arg base "${base_sha}" \
  --arg head "${head_sha}" \
  --arg merge "${merge_sha}" '
    .state == "open"
    and .draft == false
    and .base.sha == $base
    and .head.sha == $head
    and .merge_commit_sha == $merge
  ' >/dev/null <<<"${latest_pr}" \
  || fail "pull request changed while candidate source was resolved"
[[ "$(resolve_remote_sha refs/heads/main "final remote main")" == "${base_sha}" ]] \
  || fail "main changed while candidate source was resolved"
[[ "$(resolve_remote_sha "${source_ref}" "final pull request merge ref")" == "${merge_sha}" ]] \
  || fail "pull request merge ref changed while candidate source was resolved"

{
  printf 'commit_sha=%s\n' "${merge_sha}"
  printf 'tree_sha=%s\n' "${tree_sha}"
  printf 'base_sha=%s\n' "${base_sha}"
  printf 'head_sha=%s\n' "${head_sha}"
} >>"${GITHUB_OUTPUT}"

echo "[resolve-cloud-candidate-source] OK: PR #${PR_NUMBER} -> ${merge_sha}"
