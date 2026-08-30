#!/usr/bin/env bash
# Mechanics for the Home Assistant Cloud evidence envelope — never the attestation.
#
# The envelope binds to a PR's synthetic merge tree, so every merge into main
# needs a fresh one (docs/releasing.md -> "Home Assistant Cloud publication
# gate"). Doing that by hand is ~20 steps across three machines, and both real
# incidents happened in exactly the mechanical steps: #559 set only one of the
# two secret locations and main went red seconds after the squash merge; #560
# left the envelope pointing at the discarded synthetic merge commit and every
# later PR failed the gate. This script owns the mechanics and refuses the
# judgement:
#
#   resolve the exact target   refs/pull/<pr>/merge -> commit, tree, relay version
#   bind the candidate run     exact PR/base/head/merge request identity
#   verify the download        sha256 over the artifact's own checksum files
#   prove identity FIRST       every bundle's embedded manifest must match the
#                              target before any candidate binary executes
#   run provenance             one internal-cloud-release-check per enabled
#                              platform, fail-closed: an unreachable host or a
#                              failed run is a hard stop, never a skip
#   move the secret            BOTH locations (repository secret AND production
#                              environment), both updated_at verified after
#   repoint after the merge    --repoint: same tree, renamed to the main commit
#
# It never writes a check boolean into the secret. checks/keyrings values come
# exclusively from the maintainer's envelope file (--envelope), validated
# mechanically against the gate contract and this exact target. The
# qualification decision and the PR ledger stay with the maintainer
# (docs/work/2026-07-30-cloud-release-evidence-risk-scope-spec.md).
#
# The contract and execution halves live next to this file and are sourced:
#   cloud-evidence-envelope.sh    validate/template/secret-write
#   cloud-evidence-provenance.sh  checksums/identity/provenance runs
#
#   bash scripts/release/build-cloud-evidence.sh 541 --dry-run
#   bash scripts/release/build-cloud-evidence.sh 541
#   bash scripts/release/build-cloud-evidence.sh 541 --set --envelope envelope.json
#   bash scripts/release/build-cloud-evidence.sh --repoint envelope.json
#
# Environment knobs:
#   HA_NOVA_LINUX_SSH    (default: ai-machine)    ssh destination, Linux lab host
#   HA_NOVA_WINDOWS_SSH  (default: ha-nova-win)   ssh destination, Windows lab host
#   HA_NOVA_REPO         (default: markusleben/ha-nova)
#   HA_NOVA_VERSION_TAG  (default: v<skill_version>-rc1 at the target)
#   HA_NOVA_WINDOWS_PWSH (default: powershell)
#   HA_NOVA_POLL_SECONDS (default: 10) run-appearance poll cadence
# The ssh values are DESTINATIONS, so put the address, user and any
# HostKeyAlias in ~/.ssh/config rather than here. A lab guest's address
# changes; an alias does not, and this repo must not carry one maintainer's
# IP addresses.
set -euo pipefail

# shellcheck disable=SC2034  # used by the sourced cloud-evidence-envelope.sh
SECRET_NAME="HA_NOVA_CLOUD_GATE_EVIDENCE_JSON"
USAGE="usage: build-cloud-evidence.sh <pr-number> [--dry-run | --set --envelope <file>] [--envelope <file>]
       build-cloud-evidence.sh --repoint <envelope.json>"

die() { KEEP_WORK=1; echo "[cloud-evidence] ERROR: $*" >&2; exit 1; }
step() { echo ""; echo "[cloud-evidence] $*"; }

[ $# -ge 1 ] || die "$USAGE"
MODE=""; PR=""; ENVELOPE_FILE=""; REPOINT_FILE=""
if [ "$1" = "--repoint" ]; then
  [ $# -eq 2 ] || die "--repoint takes exactly one argument: the envelope.json whose tree already sits on origin/main"
  MODE="--repoint"; REPOINT_FILE="$2"
else
  PR="$1"; shift
  while [ $# -gt 0 ]; do
    case "$1" in
      --dry-run|--set)
        [ -z "$MODE" ] || die "pick one of --dry-run or --set, not both"
        MODE="$1"; shift ;;
      --envelope) [ $# -ge 2 ] || die "--envelope needs a file"; ENVELOPE_FILE="$2"; shift 2 ;;
      *) die "unknown option: $1
$USAGE" ;;
    esac
  done
  [[ "$PR" =~ ^[0-9]+$ ]] || die "PR must be a number, got '$PR' — it becomes an API path and a git ref"
  if [ "$MODE" = "--set" ] && [ -z "$ENVELOPE_FILE" ]; then
    die "refusing --set without --envelope <file>: the check booleans are the maintainer's attestation, and this script never invents them. Run without --set for a template, edit every boolean you can attest, then pass the file."
  fi
fi

REPO="${HA_NOVA_REPO:-markusleben/ha-nova}"
LINUX_SSH="${HA_NOVA_LINUX_SSH:-ai-machine}"
WINDOWS_SSH="${HA_NOVA_WINDOWS_SSH:-ha-nova-win}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v gh >/dev/null || die "gh is required"
command -v jq >/dev/null || die "jq is required"

active="$(gh api user --jq .login 2>/dev/null || true)"
[ "$active" = "${REPO%%/*}" ] || die "gh is authenticated as '${active:-none}', expected '${REPO%%/*}'"

# shellcheck source=scripts/release/cloud-evidence-envelope.sh
. "$SCRIPT_DIR/cloud-evidence-envelope.sh"
# shellcheck source=scripts/release/cloud-evidence-provenance.sh
. "$SCRIPT_DIR/cloud-evidence-provenance.sh"

# ── --repoint: after the squash merge ────────────────────────────────────────
# The PR's synthetic merge commit is thrown away at merge time and is an
# ancestor of nothing afterwards, and BOTH stale-evidence escapes require the
# evidence commit to be an ancestor of their target. A squash merge keeps the
# tree, so repointing commit_sha (and its relay_app twin) to the resulting
# main commit attests exactly the same content under a name that survives
# (docs/releasing.md, observed on #560).
if [ "$MODE" = "--repoint" ]; then
  step "repoint: resolving origin/main"
  # A private ref, not FETCH_HEAD: FETCH_HEAD is per-worktree shared state
  # that any concurrent `git fetch` (editor autofetch, a parallel agent)
  # rewrites between the reads below — and a foreign head whose tree happens
  # to equal the envelope tree would be written into both secrets as a
  # non-main commit_sha, recreating exactly the #560 failure.
  git -C "$ROOT_DIR" fetch --quiet --force origin "+refs/heads/main:refs/cloud-evidence/main" 2>/dev/null \
    || die "cannot fetch origin/main"
  main_commit="$(git -C "$ROOT_DIR" rev-parse refs/cloud-evidence/main)"
  main_tree="$(git -C "$ROOT_DIR" rev-parse "refs/cloud-evidence/main^{tree}")"
  echo "  main commit $main_commit"
  echo "  main tree   $main_tree"

  [ -f "$REPOINT_FILE" ] || die "envelope file not found: $REPOINT_FILE"
  envelope_tree="$(jq -r '.tree_sha // empty' "$REPOINT_FILE" 2>/dev/null || true)"
  [ "$envelope_tree" = "$main_tree" ] \
    || die "origin/main tree ($main_tree) does not match the envelope tree (${envelope_tree:-unreadable}) — something else landed after the merge, so this envelope attests different content. Mint fresh evidence instead of repointing."

  relay_version="$(git -C "$ROOT_DIR" show "refs/cloud-evidence/main:nova/config.yaml" \
    | sed -n 's/^version:[[:space:]]*"\([^"]*\)"[[:space:]]*$/\1/p')"
  case "$relay_version" in
    "" ) die "nova/config.yaml has no quoted \`version:\` line on origin/main" ;;
    *[$'\n']* ) die "nova/config.yaml has more than one quoted \`version:\` line on origin/main" ;;
  esac
  platforms="$(git -C "$ROOT_DIR" show "refs/cloud-evidence/main:version.json" | jq -r '.cloud_remote_platforms[]?')"
  [ -n "$platforms" ] || die "version.json on origin/main lists no enabled cloud_remote_platforms"

  validate_envelope "$REPOINT_FILE" "$main_tree" "" "$relay_version" "$platforms"

  repointed="$(jq --arg c "$main_commit" '.commit_sha = $c | .relay_app.source_commit = $c' <<<"$VALIDATED_DOC")" \
    || die "cannot rewrite the envelope"
  step "repointed envelope (the input file is left untouched)"
  echo "$repointed" | jq .
  # Same exact-state rule as the PR path: re-fetch immediately before the
  # write and require main to still be the commit this repoint names — a
  # merge that lands during validation must not be papered over with a
  # stale envelope that reports success.
  git -C "$ROOT_DIR" fetch --quiet --force origin "+refs/heads/main:refs/cloud-evidence/main" 2>/dev/null \
    || die "cannot re-fetch origin/main"
  main_now="$(git -C "$ROOT_DIR" rev-parse refs/cloud-evidence/main)"
  [ "$main_now" = "$main_commit" ] \
    || die "origin/main moved during validation (now $main_now, repointing to $main_commit) — re-run"
  set_both_secrets "$repointed"
  echo ""
  echo "[cloud-evidence] done. main's ci-gate reads the repository secret on the next"
  echo "                 push; later PRs need this main commit as the evidence ancestor."
  exit 0
fi

# check_identity runs node — a missing node there reads as an identity
# mismatch, which is an attestation-critical message for a check that never
# happened. shasum/unzip guard the artifact before anything executes it.
command -v node >/dev/null || die "node is required"
command -v shasum >/dev/null || die "shasum is required"
command -v unzip >/dev/null || die "unzip is required"

# ── 1. the exact target ──────────────────────────────────────────────────────
step "resolving PR #$PR"
pr_json="$(gh api "repos/$REPO/pulls/$PR")" || die "cannot read PR #$PR from GitHub"
state="$(jq -r .state <<<"$pr_json")"
mergeable="$(jq -r '.mergeable_state // "unknown"' <<<"$pr_json")"
merge_commit="$(jq -r '.merge_commit_sha // ""' <<<"$pr_json")"
[ "$state" = "open" ] || die "PR #$PR is $state — after a merge, use --repoint instead"
# Mirror the candidate resolver's PR eligibility predicate locally (draft,
# base branch, same-repo head — resolve-cloud-candidate-source.sh): every one
# of these would only be rejected AFTER the dispatch and burn the run.
[ "$(jq -r '.draft // false' <<<"$pr_json")" != "true" ] \
  || die "PR #$PR is a draft — mark it ready for review first; the candidate workflow rejects drafts"
base_ref="$(jq -r '.base.ref // ""' <<<"$pr_json")"
[ "$base_ref" = "main" ] \
  || die "PR #$PR targets '${base_ref:-unknown}', not main — the candidate workflow only builds PRs into main"
resolved_base_sha="$(jq -r '.base.sha // ""' <<<"$pr_json")"
[[ "$resolved_base_sha" =~ ^[0-9a-f]{40}$ ]] \
  || die "PR #$PR has no full lowercase base sha in the API response"
resolved_head_sha="$(jq -r '.head.sha // ""' <<<"$pr_json")"
[[ "$resolved_head_sha" =~ ^[0-9a-f]{40}$ ]] \
  || die "PR #$PR has no full lowercase head sha in the API response"
head_repo="$(jq -r '.head.repo.full_name // ""' <<<"$pr_json")"
[ "$head_repo" = "$REPO" ] \
  || die "PR #$PR's head lives in '${head_repo:-unknown}', not $REPO — the candidate workflow only builds same-repo branches"
# The resolver also refuses requested-changes reviews and unresolved review
# threads; server-side that verdict lands only AFTER the dispatch and burns
# the run (#609/rc22: a stale bot thread from a pre-session review). GraphQL
# because REST cannot see thread resolution. Called here at resolve time and
# again at the moment of spending (dispatch_and_bind): review state arriving
# during envelope/reachability work would still burn the dispatch.
require_reviewable_pr() {
  local thread_pages
  # Same --paginate --slurp + $endCursor shape as the server resolver
  # (resolve-cloud-candidate-source.sh), so the preflight sees EVERY thread
  # page, and the same reviewDecision allowlist.
  thread_pages="$(gh api graphql --paginate --slurp \
    -f owner="${REPO%%/*}" -f name="${REPO##*/}" -F number="$PR" \
    -f query='query($owner:String!,$name:String!,$number:Int!,$endCursor:String){repository(owner:$owner,name:$name){pullRequest(number:$number){state isDraft reviewDecision reviewThreads(first:100,after:$endCursor){nodes{isResolved} pageInfo{hasNextPage endCursor}}}}}')" \
    || die "cannot read #$PR's review threads"
  jq -e '
    length > 0
    and all(.[];
      .data.repository.pullRequest != null
      and .data.repository.pullRequest.state == "OPEN"
      and .data.repository.pullRequest.isDraft == false
      and (
        .data.repository.pullRequest.reviewDecision == "APPROVED"
        or .data.repository.pullRequest.reviewDecision == "REVIEW_REQUIRED"
        or .data.repository.pullRequest.reviewDecision == null
      )
      and all(.data.repository.pullRequest.reviewThreads.nodes[]; .isResolved == true)
    )' >/dev/null <<<"$thread_pages" \
    || die "PR #$PR is not an open non-draft PR with clean review state (closed/draft/requested-changes/unresolved threads) — the candidate workflow refuses the dispatch; resolve that first (stale bot threads from earlier reviews count)"
}
# The resolver requires every pre-evidence check green before it builds;
# dispatching while ci-gate still runs burns the run (#611/rc23: dispatched
# seconds after a push, CI unfinished). Mirror only ci-gate — the one that
# actually burned a dispatch, and the slowest of the set; the resolver stays
# the authority for the full policy list and its workflow bindings.
require_ci_workflow_green() {
  local gate_state fresh_merge main_now check_state run_info suite_id check_info check_suite policy_file required_names head_checks required_check shadow_count run_pages check_pages status_pages
  # The resolver requires the OWNING CI workflow run successful — the whole
  # run, not just the ci-gate job (a green ci-gate next to a red sibling job
  # still fails server-side; ci-gate has no needs: on its siblings). Bind to
  # THIS PR's own run: the same head sha can carry a run from a stacked
  # branch, and an unattributed run fails closed.
  # The resolver also binds the run to the CURRENT base: a run recorded
  # against an older main is rejected server-side even when the merge is
  # unchanged, so mirror the base predicate too.
  main_now="$(git -C "$ROOT_DIR" ls-remote origin refs/heads/main 2>/dev/null | awk 'NR==1{print $1}')"
  [ -n "$main_now" ] || die "cannot resolve origin/main for the CI-run base binding"
  # NOTE: gh refuses --slurp together with --jq — fetch raw pages, jq locally.
  run_pages="$(gh api --paginate --slurp "repos/$REPO/actions/runs?head_sha=$resolved_head_sha&event=pull_request&per_page=100" 2>/dev/null)" \
    || die "cannot read the CI workflow runs for #$PR's head"
  run_info="$(jq -r --arg base "$main_now" --argjson pr "$PR" \
      '[.[] | .workflow_runs[] | select(.path == ".github/workflows/ci.yml" and ([.pull_requests[]? | select(.number == $pr and (.base.sha // "") == $base)] | length) > 0)] | max_by(.id) | if . == null then "absent" else "\(.check_suite_id // 0) \(.status):\(.conclusion // "-")" end' <<<"$run_pages")"
  suite_id="${run_info%% *}"; gate_state="${run_info#* }"
  [ "$gate_state" = "completed:success" ] \
    || die "the CI workflow run is '${run_info}' for #$PR's head against the current base — rerun CI (or wait for it) before dispatching; the resolver refuses anything else"
  # AND every resolver-required pre-evidence check from the repo policy —
  # not just ci-gate: any pending or failed sibling check (analyze,
  # dependency-review, the gates) rejects the dispatch server-side. The
  # resolver binds the GLOBALLY latest check per name and verifies its
  # workflow binding; locally, the newest github-actions check of THIS PR
  # must sit in the allowlist, and ci-gate's must come from the selected CI
  # run's suite (a workflow edit that drops the job, or a check emitted by
  # another workflow, fails closed).
  # Policy from TRUSTED origin/main, never the working tree: a PR that edits
  # the policy file must not feed the preflight its own check list (the
  # server-side resolver reads main's policy the same way).
  git -C "$ROOT_DIR" fetch --quiet --force origin "+refs/heads/main:refs/cloud-evidence/policy-main" 2>/dev/null \
    || die "cannot fetch origin/main for the check policy"
  required_names="$(git -C "$ROOT_DIR" show "refs/cloud-evidence/policy-main:.github/policy/repo-policy.json" 2>/dev/null \
      | jq -c '.main_branch_protection | ((.required_status_checks - ["cloud-source-gate"]) + .advisory_checks) | unique')" \
    || die "cannot read the required-check list from origin/main's repo policy"
  [ -n "$required_names" ] || die "origin/main's repo policy yields no required-check list"
  check_pages="$(gh api --paginate --slurp "repos/$REPO/commits/$resolved_head_sha/check-runs?per_page=100" 2>/dev/null)" \
    || die "cannot read the check runs for #$PR's head"
  head_checks="$(jq --argjson pr "$PR" \
      '[.[] | .check_runs[] | select((.app.slug // "") == "github-actions" and ([.pull_requests[]? | select(.number == $pr)] | length) > 0)]' <<<"$check_pages")"
  while IFS= read -r required_check; do
    check_info="$(jq -r --arg n "$required_check" '[.[] | select(.name == $n)] | max_by(.id) | if . == null then "absent" else "\(.check_suite.id // 0) \(.status):\(.conclusion // "-")" end' <<<"$head_checks")"
    check_suite="${check_info%% *}"; check_state="${check_info#* }"
    case "$check_state" in
      # The resolver's conclusion allowlist.
      completed:success|completed:skipped|completed:neutral) : ;;
      *) die "required pre-evidence check '$required_check' is '$check_state' on #$PR's head — the resolver refuses the dispatch" ;;
    esac
    if [ "$required_check" = "ci-gate" ]; then
      [ "$check_suite" = "$suite_id" ] \
        || die "the newest ci-gate check (suite ${check_suite}) does not come from the selected CI run's suite ($suite_id) — the resolver binds the newest check and would reject"
    fi
  done < <(jq -r '.[]' <<<"$required_names")
  # The resolver also refuses any required check shadowed by a legacy commit
  # STATUS of the same name.
  status_pages="$(gh api --paginate --slurp "repos/$REPO/commits/$resolved_head_sha/status?per_page=100" 2>/dev/null)" \
    || die "cannot read commit statuses for #$PR's head"
  shadow_count="$(jq --argjson names "$required_names" '[.[] | .statuses[]?.context | select(. as $c | $names | index($c))] | length' <<<"$status_pages")"
  [ "${shadow_count:-0}" = "0" ] \
    || die "a legacy commit status shadows a required check on #$PR's head — the resolver refuses; remove the status source first"
  # Staleness stop LAST — after every API read above: the synthetic merge
  # moves when the PR head OR main advances, and request_id/target identity
  # are bound to the RESOLVED merge; a match here proves the reads above ran
  # against the still-current state. At resolve time target_commit is not set
  # yet; the freshly resolved state needs no check.
  if [ -n "${target_commit:-}" ]; then
    git -C "$ROOT_DIR" fetch --quiet --force origin "+refs/pull/$PR/merge:refs/cloud-evidence/$PR" 2>/dev/null \
      || die "cannot re-fetch the synthetic merge ref for #$PR"
    fresh_merge="$(git -C "$ROOT_DIR" rev-parse "refs/cloud-evidence/$PR")"
    [ "$fresh_merge" = "$target_commit" ] \
      || die "the synthetic merge moved since resolve ($target_commit -> $fresh_merge) — head or base advanced; re-run the pipeline"
  fi
}
require_reviewable_pr
# Only the plain pipeline mode gates here: --set with a reusable candidate
# dispatches nothing (and checklist step 8 reruns CI AFTER --set — a repeat
# --set in that window must not be refused), and --dry-run exits before any
# spend. The dispatch itself stays guarded in dispatch_and_bind, which also
# covers --set's expired-artifact fresh-dispatch fallback.
if [ -z "$MODE" ]; then require_ci_workflow_green; fi
case "$mergeable" in
  # `blocked` is the EXPECTED state here: cloud-source-gate is a required check
  # and it is red precisely because the envelope this script prepares does not
  # exist yet. Refusing it would make the script unusable for its only job.
  # What must not pass is a tree that is not the tree to attest.
  clean|has_hooks|blocked|unstable) : ;;
  dirty) die "PR #$PR has merge conflicts — the merge tree is not buildable" ;;
  draft) die "PR #$PR is a draft" ;;
  unknown) die "GitHub has not computed #$PR's merge state yet — retry in a moment, do not rebase" ;;
  behind) die "PR #$PR is behind main — update the branch, then re-run" ;;
  *) die "PR #$PR is '$mergeable', not clean — the tree that would merge is not the tree to attest" ;;
esac
[ -n "$merge_commit" ] || die "GitHub has not computed a merge commit for #$PR yet"

# Forced: this is our own scratch ref, and it must follow the PR as it moves.
# Without the +, a second run on a changed PR fails instead of updating.
git -C "$ROOT_DIR" fetch --quiet --force origin "+refs/pull/$PR/merge:refs/cloud-evidence/$PR" 2>/dev/null \
  || die "cannot fetch the synthetic merge ref for #$PR"
target_commit="$(git -C "$ROOT_DIR" rev-parse "refs/cloud-evidence/$PR")"
target_tree="$(git -C "$ROOT_DIR" rev-parse "refs/cloud-evidence/$PR^{tree}")"
# Bind the API's merge identity to the fetched ref: two views of the same
# synthetic merge commit that must agree, or GitHub is mid-recompute.
[ "$merge_commit" = "$target_commit" ] \
  || die "GitHub's merge identity ($merge_commit) does not match the fetched merge ref ($target_commit) — retry in a moment, do not rebase"
# Exactly the verifier's shape (verify-cloud-release-gate.sh): one quoted
# `version:` line. Accepting more here only defers the rejection to the gate,
# after all three platforms have run.
relay_version="$(git -C "$ROOT_DIR" show "$target_commit:nova/config.yaml" \
  | sed -n 's/^version:[[:space:]]*"\([^"]*\)"[[:space:]]*$/\1/p')"
# Exactly one line, and say so if not: a bare test under `set -e` exits with
# no message at all, which is how this check silently killed a dry run.
case "$relay_version" in
  "" ) die "nova/config.yaml has no quoted \`version:\` line at the target — the gate's verifier requires exactly one" ;;
  *[$'\n']* ) die "nova/config.yaml has more than one quoted \`version:\` line at the target" ;;
esac
echo "  commit $target_commit"
echo "  tree   $target_tree"
echo "  relay  $relay_version"

# `[]?` so a version.json without the key reaches the explicit message below
# instead of dying inside jq with no context.
platforms="$(git -C "$ROOT_DIR" show "$target_commit:version.json" | jq -r '.cloud_remote_platforms[]?')"
[ -n "$platforms" ] \
  || die "version.json lists no enabled cloud_remote_platforms — there is nothing to verify, and an envelope built here would attest checks that never ran"
echo "  enabled platforms: $(tr '\n' ' ' <<<"$platforms")"

# Validate the maintainer's envelope BEFORE the expensive half: a file that
# cannot pass the gate must not cost a dispatch and three provenance runs.
if [ -n "$ENVELOPE_FILE" ]; then
  step "validating $ENVELOPE_FILE against this target"
  validate_envelope "$ENVELOPE_FILE" "$target_tree" "$target_commit" "$relay_version" "$platforms"
  echo "  envelope matches the target and the gate contract"
fi

# Reachability BEFORE the dispatch, not between the download and the runs:
# a preflight that reports "reachable" without asking is the failure it exists
# to prevent, and discovering an unreachable host after building and
# downloading a candidate wastes the expensive half of the job.
# Non-darwin lab hosts are best-effort (maintainer decision 2026-08-28), but
# the fallback is neither silent nor free: it requires the explicit per-run
# opt-in HA_NOVA_EVIDENCE_RUNNER_FALLBACK=1, and each fallback platform's
# final bundle still gets a POSITIVE local verification of its signed Cloud
# release evidence (Ed25519 signature, binary sha256, tree binding) via
# verify-cloud-release-evidence.mjs — only the installed-layout RUNTIME
# execution is skipped, and that residual gap goes in the ledger. A REACHABLE
# lab host is never skipped. darwin stays hard: it is the maintainer host.
RUNNER_FALLBACK=""
fallback_or_die() {  # <platform> <detail>
  if [ "${HA_NOVA_EVIDENCE_RUNNER_FALLBACK:-}" = "1" ]; then
    RUNNER_FALLBACK="$RUNNER_FALLBACK $1"
    echo "  $1: $2 unreachable — static signed-evidence verification + the workflow's native runner smoke stand in (record in the ledger)"
  else
    die "$1: $2 unreachable — bring it up, or set HA_NOVA_EVIDENCE_RUNNER_FALLBACK=1 to accept the static-verification fallback (recorded in the ledger)"
  fi
}
step "platform reachability"
for platform in $platforms; do
  case "$platform" in
    darwin)
      [ "$(uname -s)" = "Darwin" ] \
        || die "darwin provenance must run on macOS; this host is $(uname -s)"
      echo "  darwin: this host" ;;
    linux)
      if ssh -o BatchMode=yes -o ConnectTimeout=8 "$LINUX_SSH" true 2>/dev/null; then
        echo "  linux: $LINUX_SSH"
      else
        fallback_or_die linux "'$LINUX_SSH'"
      fi ;;
    windows)
      if ssh -o BatchMode=yes -o ConnectTimeout=8 "$WINDOWS_SSH" 'cmd /c "echo ok"' >/dev/null 2>&1; then
        echo "  windows: $WINDOWS_SSH"
      else
        fallback_or_die windows "'$WINDOWS_SSH'"
      fi ;;
    *) die "unknown platform '$platform' in cloud_remote_platforms" ;;
  esac
done

if [ "$MODE" = "--dry-run" ]; then
  echo ""
  echo "[cloud-evidence] dry run: target resolved; the platform evidence plan is printed above."
  echo "                 Re-run without --dry-run to dispatch."
  exit 0
fi

# NOTE there is deliberately no local "is it Codex-clean" pre-check. The
# candidate workflow owns that question (resolve-cloud-candidate-source.sh:
# a commit-bound `Codex Review: Didn't find any major issues` comment naming
# this exact head), and a second copy here drifted immediately — it read
# REACTIONS, which are a different channel, from an unpaginated list whose
# last entry was commit #100 rather than the head. A dispatch that fails this
# way costs two minutes and is not scarce: only RERUNS are blocked.

# ── 2. the candidate bundle ──────────────────────────────────────────────────
# One dispatch per PR; reruns are rejected, so an existing run for this exact
# commit is adopted or reused instead of burning the second attempt.
skill_version="$(git -C "$ROOT_DIR" show "$target_commit:version.json" | jq -r '.skill_version // empty')"
[ -n "$skill_version" ] \
  || die "version.json at the target has no skill_version — refusing to dispatch a nonsense version_tag"
version_tag="${HA_NOVA_VERSION_TAG:-v${skill_version}-rc1}"
step "candidate bundle ($version_tag)"
# shellcheck disable=SC2034  # used by check_identity in the sourced provenance lib
expected_version="${version_tag#v}"

work="$(mktemp -d)"
# Serialize whole mint sessions on this machine: two overlapping sessions
# (parallel agent sessions are normal here) would adopt each other's runs in
# the selectors below. The artifact identity check would still refuse a wrong
# attestation, but only after wasting the one explicit dispatch. The lock is
# released by the EXIT trap on every outcome; cross-machine or manual-UI
# overlap stays possible and lands in the same identity refusal, fail-closed.
MINT_LOCK="${TMPDIR:-/tmp}/ha-nova-cloud-evidence-mint.lock"
# Release only a lock THIS process acquired: a contender that dies on the
# failed mkdir must not rmdir the owner's live lock on its way out.
MINT_LOCK_ACQUIRED=0
# Keep the work dir on failure: every provenance `die` names a log inside it,
# and deleting it on the way out destroyed the diagnostics for the most
# expensive stage of the run.
trap '{ [ "${KEEP_WORK:-0}" = 1 ] && echo "[cloud-evidence] logs kept in $work" || rm -rf "$work"; }; { [ "$MINT_LOCK_ACQUIRED" = 1 ] && rmdir "$MINT_LOCK" 2>/dev/null; } || true' EXIT
mkdir "$MINT_LOCK" 2>/dev/null \
  || die "another mint session appears to be running (lock: $MINT_LOCK) — if none is, remove that directory and re-run"
MINT_LOCK_ACQUIRED=1

# The workflow resolver validates this complete request_id against its own
# authoritative PR resolution. GitHub exposes the evaluated run-name, trusted
# workflow head, and attempt in run-list output, so selection can reject a run
# before watching or downloading it. Tree identity remains an additional
# artifact check; it is not a substitute because different commits can share
# one tree.
request_id="pr${PR}-${resolved_base_sha}-${resolved_head_sha}-${target_commit}"
expected_run_title="Cloud candidate PR #${PR} ${version_tag} (${request_id})"
# Final artifacts expire after seven days. Include one upload/runtime margin
# day, then paginate every workflow-dispatch run on the trusted workflow head
# so a burst of unrelated runs cannot hide an exact reusable candidate.
candidate_since="$(node -e 'const d = new Date(Date.now() - 8 * 86400000); process.stdout.write(d.toISOString().slice(0, 10))')"
list_candidate_runs() {
  local pages total
  pages="$(gh api --paginate --slurp --method GET \
    "repos/$REPO/actions/workflows/cloud-candidate-bundle.yml/runs" \
    -f event=workflow_dispatch \
    -f "head_sha=$resolved_base_sha" \
    -f "created=>=$candidate_since" \
    -F per_page=100)" \
    || return
  total="$(jq -r '[.[].total_count] | max // 0' <<<"$pages")" \
    || die "cannot count candidate runs"
  [ "$total" -le 1000 ] \
    || die "candidate run search returned $total results in the artifact window; GitHub caps filtered workflow searches at 1000 — narrow the retained run set before dispatching"
  jq -c '[.[] | .workflow_runs[]]' <<<"$pages"
}
matching_candidate_runs() {
  jq -c --arg title "$expected_run_title" --arg trusted "$resolved_base_sha" '
    map(select(
      .event == "workflow_dispatch"
      and .display_title == $title
      and .head_sha == $trusted
      and .run_attempt == 1
    ))
  '
}
matching_run_now() {  # <run-id>
  local selected_id="$1" run matched
  run="$(gh api "repos/$REPO/actions/runs/$selected_id")" \
    || die "cannot revalidate candidate run $selected_id"
  matched="$(matching_candidate_runs <<<"[$run]")" \
    || die "cannot filter candidate run $selected_id"
  jq -e --argjson id "$selected_id" \
    'length == 1 and .[0].id == $id' >/dev/null <<<"$matched"
}
require_matching_run() {  # <run-id> <phase>
  local selected_id="$1" phase="$2"
  matching_run_now "$selected_id" \
    || die "candidate run $selected_id no longer has the exact request identity and attempt 1 $phase"
}

runs_json="$(list_candidate_runs)" || die "cannot list candidate runs"
matching_runs_json="$(matching_candidate_runs <<<"$runs_json")" \
  || die "cannot filter candidate runs"
inflight_id="$(jq -r 'map(select(.status != "completed")) | max_by(.id) | .id // empty' <<<"$matching_runs_json")"
if [ -n "$inflight_id" ]; then
  echo "  matching run $inflight_id is in flight — watching it first"
  if matching_run_now "$inflight_id"; then
    watch_succeeded=true
    gh run watch "$inflight_id" --repo "$REPO" --exit-status >/dev/null \
      || watch_succeeded=false
    if ! matching_run_now "$inflight_id"; then
      echo "  in-flight run $inflight_id changed identity or attempt — discarding it"
    elif [ "$watch_succeeded" != true ]; then
      echo "  in-flight run $inflight_id did not succeed — continuing"
    fi
  else
    echo "  in-flight run $inflight_id changed identity or attempt before watch — discarding it"
  fi
  runs_json="$(list_candidate_runs)" || die "cannot list candidate runs"
  matching_runs_json="$(matching_candidate_runs <<<"$runs_json")" \
    || die "cannot filter candidate runs"
fi

fetch_run_bundles() {  # <run-id> [quiet] ; missing artifact returns nonzero
  matching_run_now "$1" || return 2
  rm -rf "$work/bundles"
  if [ "${2:-}" = quiet ]; then
    gh run download "$1" --repo "$REPO" --name cloud-candidate-install-bundles --dir "$work/bundles" 2>/dev/null \
      || return
  else
    gh run download "$1" --repo "$REPO" --name cloud-candidate-install-bundles --dir "$work/bundles" \
      || return
  fi
  matching_run_now "$1" || return 2
}
download_run() {  # <run-id> ; hard-fails
  local result
  if fetch_run_bundles "$1"; then
    :
  else
    result="$?"
    [ "$result" != 2 ] \
      || die "candidate run $1 changed identity or attempt before its artifact could be trusted"
    die "cannot download the install bundles from run $1 (artifacts expire after 7 days)"
  fi
  step "verifying the artifact's checksums"
  verify_bundle_checksums
}

dispatch_and_bind() {  # sets run_id
  local max_before
  max_before="$(jq -r '[.[].id] | max // 0' <<<"$runs_json")"
  # Recheck at the moment of spending: the resolve-time snapshot can go stale
  # while the envelope, reachability, and reuse probes run. CI reads first,
  # the cheap reviewability read LAST — its window to the dispatch is the
  # smallest, so review state arriving during the slower CI reads still
  # refuses.
  require_ci_workflow_green
  require_reviewable_pr
  echo "  dispatching (request_id=$request_id)"
  gh workflow run cloud-candidate-bundle.yml --repo "$REPO" \
    -f "pull_request=$PR" -f "version_tag=$version_tag" -f "request_id=$request_id" \
    || die "dispatch rejected — inspect the workflow before dispatching again"
  echo "  waiting for the run to appear"
  run_id=""
  for _ in $(seq 1 30); do
    sleep "${HA_NOVA_POLL_SECONDS:-10}"
    run_id="$(list_candidate_runs | matching_candidate_runs \
      | jq -r --argjson m "$max_before" '
          map(select(.id > $m)) | max_by(.id) | .id // empty
        ')" \
      || die "cannot list candidate runs"
    [ -n "$run_id" ] && break
  done
  [ -n "$run_id" ] \
    || die "the dispatched run never appeared within 300s. It may still be queued — check: gh run list --workflow cloud-candidate-bundle.yml --repo $REPO"
  echo "  run $run_id — waiting for completion"
  require_matching_run "$run_id" "before watch"
  watch_succeeded=true
  gh run watch "$run_id" --repo "$REPO" --exit-status >/dev/null \
    || watch_succeeded=false
  require_matching_run "$run_id" "after watch"
  [ "$watch_succeeded" = true ] \
    || die "run $run_id failed — fix the cause in a reviewed PR, then dispatch once more"
}

run_id="$(jq -r '
  map(select(.status == "completed" and .conclusion == "success"))
  | max_by(.id) | .id // empty
' <<<"$matching_runs_json")"
bundles_ok=false
if [ -n "$run_id" ]; then
  step "reuse candidate: exact successful run $run_id — its artifact must also prove this target"
  if fetch_run_bundles "$run_id" quiet; then
    step "verifying the artifact's checksums"
    verify_bundle_checksums
    step "verifying bundle identity (before anything executes)"
    if check_bundles_against_target; then
      bundles_ok=true
      echo "  reusing run $run_id"
    else
      echo "  not this target's candidate — dispatching fresh"
    fi
  else
    download_result="$?"
    if [ "$download_result" = 2 ]; then
      echo "  run $run_id changed identity or attempt — dispatching fresh"
    else
      echo "  cannot download run $run_id's artifact (likely expired after 7 days) — dispatching fresh"
    fi
  fi
fi

if [ "$bundles_ok" != true ]; then
  rm -rf "$work/bundles"
  dispatch_and_bind
  download_run "$run_id"
  step "verifying bundle identity (before anything executes)"
  check_bundles_against_target \
    || die "the dispatched run's artifact does not match this target — refusing to execute it (see above)"
fi

# ── 3. provenance, per platform ──────────────────────────────────────────────
# The positive path: the INSTALLED bundle must accept its own provenance on
# the maintainer host (darwin) and on every reachable lab host. Platforms in
# RUNNER_FALLBACK instead get a POSITIVE local verification of the exact
# bundle's signed Cloud release evidence; the installed-layout runtime
# execution is the one thing skipped (opt-in, ledger-recorded).
platforms_csv="$(git -C "$ROOT_DIR" show "$target_commit:version.json" | jq -r '.cloud_remote_platforms | join(",")')"
fallback_static_verify() {  # <platform>
  local fb_platform="$1" fb_arch archive x
  case "$fb_platform" in
    linux)
      for fb_arch in amd64 arm64; do
        archive="$work/bundles/ha-nova-installer-bundle-linux-${fb_arch}.tar.gz"
        [ -f "$archive" ] || die "linux: $archive missing from the artifact"
        x="$work/fallback-linux-$fb_arch"; mkdir -p "$x"
        tar -xzf "$archive" -C "$x" || die "linux/$fb_arch: cannot extract $archive"
        node "$SCRIPT_DIR/verify-cloud-release-evidence.mjs" \
          "$x/ha-nova/bundle.json" "$x/ha-nova/ha-nova" "${version_tag#v}" \
          linux "$fb_arch" ha-nova "$target_tree" "$platforms_csv" \
          || die "linux/$fb_arch: static signed-evidence verification failed — the fallback never accepts a broken bundle"
      done ;;
    windows)
      archive="$work/bundles/ha-nova-installer-bundle-windows-amd64.zip"
      [ -f "$archive" ] || die "windows: $archive missing from the artifact"
      x="$work/fallback-windows"; mkdir -p "$x"
      unzip -q "$archive" -d "$x" || die "windows: cannot extract $archive"
      node "$SCRIPT_DIR/verify-cloud-release-evidence.mjs" \
        "$x/ha-nova/bundle.json" "$x/ha-nova/ha-nova.exe" "${version_tag#v}" \
        windows amd64 ha-nova.exe "$target_tree" "$platforms_csv" \
        || die "windows: static signed-evidence verification failed — the fallback never accepts a broken bundle" ;;
  esac
}
for platform in $platforms; do
  case " $RUNNER_FALLBACK " in
    *" $platform "*)
      step "provenance fallback: $platform (static signed-evidence verification, local)"
      fallback_static_verify "$platform"
      echo "  $platform: signed evidence verified locally; installed-layout runtime execution skipped (ledger)"
      continue ;;
  esac
  case "$platform" in
    darwin)
      arch="$(uname -m | sed 's/x86_64/amd64/')"
      archive="$work/bundles/ha-nova-installer-bundle-macos-${arch}.tar.gz"
      [ -f "$archive" ] || die "darwin: $archive missing from the artifact"
      step "provenance: darwin ($arch, local)"
      provenance_unix darwin "$archive" "" || die "darwin: provenance failed — see $work/darwin.out"
      ;;
    linux)
      # The artifact carries amd64 and arm64 Linux bundles; ask the host which
      # one it can actually execute instead of hardcoding amd64.
      linux_arch="$(ssh -o BatchMode=yes "$LINUX_SSH" uname -m 2>/dev/null | tr -d '[:space:]')" \
        || die "linux: cannot query the architecture on $LINUX_SSH"
      case "$linux_arch" in
        x86_64) linux_arch=amd64 ;;
        aarch64|arm64) linux_arch=arm64 ;;
        *) die "linux: unsupported architecture '${linux_arch:-unknown}' on $LINUX_SSH" ;;
      esac
      archive="$work/bundles/ha-nova-installer-bundle-linux-${linux_arch}.tar.gz"
      [ -f "$archive" ] || die "linux: $archive missing from the artifact"
      step "provenance: linux ($linux_arch, ssh $LINUX_SSH)"
      provenance_unix linux "$archive" "$LINUX_SSH" || die "linux: provenance failed — see $work/linux.out"
      ;;
    windows)
      archive="$work/bundles/ha-nova-installer-bundle-windows-amd64.zip"
      [ -f "$archive" ] || die "windows: $archive missing from the artifact"
      step "provenance: windows (ssh $WINDOWS_SSH)"
      provenance_windows "$archive" || die "windows: provenance failed — see $work/windows.out"
      ;;
    *) die "unknown platform '$platform' in cloud_remote_platforms" ;;
  esac
  extract_manifest "$work/$platform.out" >"$work/$platform.bundle.json"
  check_identity "$work/$platform.bundle.json" \
    || die "$platform: bundle identity does not match the target tree"
  echo "  ✓ $platform"
done

# ── 4. the envelope ──────────────────────────────────────────────────────────
# Every platform above passed — that is EXECUTION backing for this session,
# not a boolean this script writes. checks/keyrings values come exclusively
# from the maintainer's --envelope file; without one, print a template with
# every attestation left false.
if [ -n "$ENVELOPE_FILE" ]; then
  # Validate AGAIN right before use: the pipeline above takes minutes, and the
  # file may have been edited in between. The bytes written are exactly the
  # bytes this validation passed — never a later re-read of the file.
  validate_envelope "$ENVELOPE_FILE" "$target_tree" "$target_commit" "$relay_version" "$platforms"
  envelope="$VALIDATED_DOC"
  step "envelope (validated against this target)"
  echo "$envelope" | jq .
else
  envelope="$(build_template_envelope "$target_commit" "$target_tree" "$relay_version" "$platforms")"
  step "envelope TEMPLATE — every attestation is false on purpose"
  echo "$envelope" | jq .
  echo ""
  echo "  This run proved: artifact checksums, plus one internal-cloud-release-check"
  echo "  from an installed layout on each platform that ran it."
  for platform in $platforms; do
    case " $RUNNER_FALLBACK " in
      *" $platform "*)
        echo "    $platform: FALLBACK — static signed-evidence verification only; NO installed-layout run (ledger)" ;;
      *)
        echo "    $platform: installed-layout internal-cloud-release-check ran" ;;
    esac
  done
  echo "  That is the execution backing for signing_and_update_matrix; every"
  echo "  other boolean (and keyrings per OS) is a qualification decision this script"
  echo "  will not make. Edit each value you can attest to true, save the file,"
  echo "  record the ledger in the PR, then re-run with: --set --envelope <file>"
fi

if [ "$MODE" = "--set" ]; then
  # The pipeline above takes minutes. Before touching the global secrets,
  # require the PR to still be OPEN and its merge identity to still be the
  # exact commit this envelope attests — GitHub retains the synthetic merge
  # ref after a close or merge, so the ref alone proves nothing about the
  # PR's liveness. Evidence is written only for the state it names.
  step "re-resolving #$PR before the write"
  pr_json_now="$(gh api "repos/$REPO/pulls/$PR")" || die "cannot re-read PR #$PR from GitHub"
  state_now="$(jq -r .state <<<"$pr_json_now")"
  [ "$state_now" = "open" ] \
    || die "PR #$PR is $state_now now — refusing to write evidence for a PR that is no longer open; after a merge use --repoint"
  api_merge_now="$(jq -r '.merge_commit_sha // ""' <<<"$pr_json_now")"
  [ "$api_merge_now" = "$target_commit" ] \
    || die "PR #$PR's merge identity moved during the pipeline (API: ${api_merge_now:-none}, attested: $target_commit) — re-run against the new state"
  git -C "$ROOT_DIR" fetch --quiet --force origin "+refs/pull/$PR/merge:refs/cloud-evidence/$PR" 2>/dev/null \
    || die "cannot re-fetch the synthetic merge ref for #$PR"
  now_commit="$(git -C "$ROOT_DIR" rev-parse "refs/cloud-evidence/$PR")"
  [ "$now_commit" = "$target_commit" ] \
    || die "PR #$PR moved during the pipeline (merge commit is now $now_commit, attested $target_commit) — re-run against the new state"
  echo "  open, unchanged: $now_commit"
  set_both_secrets "$envelope"
  echo ""
  echo "  written. Rerun CI on #$PR (no new commit), then cloud-source-gate should pass."
  echo "  After the squash merge: build-cloud-evidence.sh --repoint <file>"
elif [ -n "$ENVELOPE_FILE" ]; then
  echo ""
  echo "[cloud-evidence] validated only — nothing written. Re-run with --set to write both secrets."
else
  echo ""
  echo "[cloud-evidence] nothing written."
fi

echo ""
echo "[cloud-evidence] The check booleans carry the risk-scoped qualifications from"
echo "                 docs/work/2026-07-30-cloud-release-evidence-risk-scope-spec.md."
echo "                 Inspect the qualification-to-target diff before merging, and record"
echo "                 the ledger in the PR — neither this script nor the verifier makes"
echo "                 that decision."
