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
#   bind the candidate run     request_id derived from the target commit
#   verify the download        sha256 over the artifact's own checksum files
#   run provenance             one internal-cloud-release-check per enabled
#                              platform, fail-closed: an unreachable host or a
#                              failed run is a hard stop, never a skip
#   move the secret            BOTH locations (repository secret AND production
#                              environment), both updated_at verified after
#   repoint after the merge    --repoint: same tree, renamed to the main commit
#
# It never writes a check boolean. checks/keyrings values come exclusively from
# the maintainer's envelope file (--envelope), validated mechanically against
# the gate contract and this exact target. The qualification decision and the
# PR ledger stay with the maintainer
# (docs/work/2026-07-30-cloud-release-evidence-risk-scope-spec.md).
#
#   bash scripts/release/build-cloud-evidence.sh 541 --dry-run
#   bash scripts/release/build-cloud-evidence.sh 541
#   bash scripts/release/build-cloud-evidence.sh 541 --set --envelope envelope.json
#   bash scripts/release/build-cloud-evidence.sh --repoint envelope.json
#
# Hosts for the non-local platforms come from the environment, defaulting to
# this repo's lab:
#   HA_NOVA_LINUX_SSH   (default: ai-machine)
#   HA_NOVA_WINDOWS_SSH (default: ha-nova-win)
# Both are ssh DESTINATIONS, so put the address, user and any HostKeyAlias in
# ~/.ssh/config rather than here. A lab guest's address changes; an alias does
# not, and this repo must not carry one maintainer's IP addresses.
set -euo pipefail

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
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

command -v gh >/dev/null || die "gh is required"
command -v jq >/dev/null || die "jq is required"

active="$(gh api user --jq .login 2>/dev/null || true)"
[ "$active" = "${REPO%%/*}" ] || die "gh is authenticated as '${active:-none}', expected '${REPO%%/*}'"

# ── envelope contract ────────────────────────────────────────────────────────
# Mirror of scripts/release/verify-cloud-release-gate.sh (requiredChecks plus
# the exact-key rules). The gate stays authoritative: drift here only moves
# WHERE a bad envelope is refused (locally instead of in CI) — it can never
# let one pass. tests/onboarding/cloud-evidence-behavior.test.ts pins the two
# check lists equal.
REQUIRED_CHECKS_SORTED="domains_mfa,installed_relay_app,keyrings,lifecycle,parity,redirects_non_disclosure,roles,routing,signing_and_update_matrix,stress_10000"

validate_envelope() {  # <file> <want-tree> <want-commit|""> <want-relay-version> <platforms-newline-list>
  local file="$1" want_tree="$2" want_commit="$3" want_relay="$4" want_platforms="$5"
  local doc got_tree
  [ -f "$file" ] || die "envelope file not found: $file"
  [ "$(wc -c <"$file" | tr -d '[:space:]')" -le 32768 ] || die "envelope exceeds the gate's 32 KiB limit"
  doc="$(jq -c . "$file" 2>/dev/null)" || die "envelope is not valid JSON: $file"
  jq -e 'type == "object"' >/dev/null <<<"$doc" || die "envelope must be a JSON object"
  [ "$(jq -r 'keys_unsorted | sort | join(",")' <<<"$doc")" = "checks,commit_sha,relay_app,schema,tree_sha" ] \
    || die "envelope must contain exactly schema, commit_sha, tree_sha, checks, relay_app"
  # Strict number, like the gate: a JSON string "2" would pass a text compare
  # here and still die in CI.
  jq -e '.schema == 2' >/dev/null <<<"$doc" || die "envelope schema must equal 2 (the number, not a string)"
  jq -e '.commit_sha | type == "string" and test("^[0-9a-f]{40}$")' >/dev/null <<<"$doc" \
    || die "envelope commit_sha must be a 40-character lowercase sha"
  jq -e '.tree_sha | type == "string" and test("^[0-9a-f]{40}$")' >/dev/null <<<"$doc" \
    || die "envelope tree_sha must be a 40-character lowercase sha"
  got_tree="$(jq -r '.tree_sha' <<<"$doc")"
  [ "$got_tree" = "$want_tree" ] \
    || die "envelope tree_sha ($got_tree) does not match the resolved tree ($want_tree)"
  if [ -n "$want_commit" ]; then
    [ "$(jq -r '.commit_sha' <<<"$doc")" = "$want_commit" ] \
      || die "envelope commit_sha ($(jq -r '.commit_sha' <<<"$doc")) does not match the resolved target commit ($want_commit)"
  fi
  [ "$(jq -r '.relay_app | keys_unsorted | sort | join(",")' <<<"$doc")" = "source_commit,source_tree_sha,version" ] \
    || die "envelope relay_app must contain exactly version, source_commit, source_tree_sha"
  [ "$(jq -r '.relay_app.version' <<<"$doc")" = "$want_relay" ] \
    || die "envelope relay_app.version ($(jq -r '.relay_app.version' <<<"$doc")) does not match nova/config.yaml ($want_relay)"
  [ "$(jq -r '.relay_app.source_commit' <<<"$doc")" = "$(jq -r '.commit_sha' <<<"$doc")" ] \
    || die "envelope relay_app.source_commit must equal commit_sha"
  [ "$(jq -r '.relay_app.source_tree_sha' <<<"$doc")" = "$got_tree" ] \
    || die "envelope relay_app.source_tree_sha must equal tree_sha"
  [ "$(jq -r '.checks | keys_unsorted | sort | join(",")' <<<"$doc")" = "$REQUIRED_CHECKS_SORTED" ] \
    || die "envelope checks must contain exactly the gate's required checks plus keyrings"
  jq -e '[.checks | to_entries[] | select(.key != "keyrings") | .value] | all(. == true)' >/dev/null <<<"$doc" \
    || die "every envelope check must be literally true — this script sets no boolean, and the gate rejects anything else"
  [ "$(jq -r '.checks.keyrings | keys_unsorted | sort | join(",")' <<<"$doc")" = "$(sort <<<"$want_platforms" | paste -s -d, -)" ] \
    || die "envelope keyrings keys must exactly match cloud_remote_platforms"
  jq -e '[.checks.keyrings[]] | all(. == true)' >/dev/null <<<"$doc" \
    || die "every envelope keyrings value must be literally true"
}

# ── the secret, in BOTH places ───────────────────────────────────────────────
# PR runs and merge groups read the `production` environment through the
# trusted broker; the ci-gate job on direct main pushes reads the REPOSITORY
# secret. Setting only one lets the PR go green and turns main red seconds
# after the squash merge, with the stale-evidence message that reads like a
# tree mismatch (docs/releasing.md, observed on #559).
repo_stamp() { gh api "repos/$REPO/actions/secrets/$SECRET_NAME" --jq .updated_at 2>/dev/null || true; }
env_stamp() { gh api "repos/$REPO/environments/production/secrets/$SECRET_NAME" --jq .updated_at 2>/dev/null || true; }

set_both_secrets() {  # <envelope-json>
  local body="$1" before_repo before_env after_repo after_env
  before_repo="$(repo_stamp)"; before_env="$(env_stamp)"
  step "writing $SECRET_NAME (repository secret)"
  gh secret set "$SECRET_NAME" --repo "$REPO" --body "$body" \
    || die "cannot write the repository secret — admin auth required"
  step "writing $SECRET_NAME (production environment secret)"
  gh secret set "$SECRET_NAME" --repo "$REPO" --env production --body "$body" \
    || die "cannot write the production environment secret — admin auth required"
  # Verify with GitHub's own clocks only: read updated_at before and after and
  # require movement in both places. A local-time comparison would inherit
  # clock skew; an unreadable stamp after a write is a failure, not a pass.
  after_repo="$(repo_stamp)"; after_env="$(env_stamp)"
  { [ -n "$after_repo" ] && [ "$after_repo" != "$before_repo" ]; } \
    || die "repository secret updated_at did not advance (before: '${before_repo:-absent}', after: '${after_repo:-absent}') — verify by hand before relying on the gate"
  { [ -n "$after_env" ] && [ "$after_env" != "$before_env" ]; } \
    || die "production environment secret updated_at did not advance (before: '${before_env:-absent}', after: '${after_env:-absent}') — verify by hand before relying on the gate"
  echo "  repository secret:             updated_at $after_repo"
  echo "  production environment secret: updated_at $after_env"
}

# ── --repoint: after the squash merge ────────────────────────────────────────
# The PR's synthetic merge commit is thrown away at merge time and is an
# ancestor of nothing afterwards, and BOTH stale-evidence escapes require the
# evidence commit to be an ancestor of their target. A squash merge keeps the
# tree, so repointing commit_sha (and its relay_app twin) to the resulting
# main commit attests exactly the same content under a name that survives
# (docs/releasing.md, observed on #560).
if [ "$MODE" = "--repoint" ]; then
  step "repoint: resolving origin/main"
  git -C "$ROOT_DIR" fetch --quiet origin main 2>/dev/null || die "cannot fetch origin/main"
  main_commit="$(git -C "$ROOT_DIR" rev-parse FETCH_HEAD)"
  main_tree="$(git -C "$ROOT_DIR" rev-parse "FETCH_HEAD^{tree}")"
  echo "  main commit $main_commit"
  echo "  main tree   $main_tree"

  [ -f "$REPOINT_FILE" ] || die "envelope file not found: $REPOINT_FILE"
  envelope_tree="$(jq -r '.tree_sha // empty' "$REPOINT_FILE" 2>/dev/null || true)"
  [ "$envelope_tree" = "$main_tree" ] \
    || die "origin/main tree ($main_tree) does not match the envelope tree (${envelope_tree:-unreadable}) — something else landed after the merge, so this envelope attests different content. Mint fresh evidence instead of repointing."

  relay_version="$(git -C "$ROOT_DIR" show "FETCH_HEAD:nova/config.yaml" \
    | sed -n 's/^version:[[:space:]]*"\([^"]*\)"[[:space:]]*$/\1/p')"
  case "$relay_version" in
    "" ) die "nova/config.yaml has no quoted \`version:\` line on origin/main" ;;
    *[$'\n']* ) die "nova/config.yaml has more than one quoted \`version:\` line on origin/main" ;;
  esac
  platforms="$(git -C "$ROOT_DIR" show "FETCH_HEAD:version.json" | jq -r '.cloud_remote_platforms[]?')"
  [ -n "$platforms" ] || die "version.json on origin/main lists no enabled cloud_remote_platforms"

  validate_envelope "$REPOINT_FILE" "$main_tree" "" "$relay_version" "$platforms"

  repointed="$(jq --arg c "$main_commit" '.commit_sha = $c | .relay_app.source_commit = $c' "$REPOINT_FILE")" \
    || die "cannot rewrite the envelope"
  step "repointed envelope (the input file is left untouched)"
  echo "$repointed" | jq .
  set_both_secrets "$repointed"
  echo ""
  echo "[cloud-evidence] done. main's ci-gate reads the repository secret on the next"
  echo "                 push; later PRs need this main commit as the evidence ancestor."
  exit 0
fi

# check_identity runs node AFTER all provenance — a missing node there reads as
# an identity mismatch, which is an attestation-critical message for a check
# that never happened. shasum guards the artifact before anything executes it.
command -v node >/dev/null || die "node is required"
command -v shasum >/dev/null || die "shasum is required"

# ── 1. the exact target ──────────────────────────────────────────────────────
step "resolving PR #$PR"
pr_json="$(gh api "repos/$REPO/pulls/$PR")"
state="$(jq -r .state <<<"$pr_json")"
mergeable="$(jq -r '.mergeable_state // "unknown"' <<<"$pr_json")"
merge_commit="$(jq -r '.merge_commit_sha // ""' <<<"$pr_json")"
[ "$state" = "open" ] || die "PR #$PR is $state — after a merge, use --repoint instead"
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
step "platform reachability"
for platform in $platforms; do
  case "$platform" in
    darwin)
      [ "$(uname -s)" = "Darwin" ] \
        || die "darwin provenance must run on macOS; this host is $(uname -s)"
      echo "  darwin: this host" ;;
    linux)
      ssh -o BatchMode=yes -o ConnectTimeout=8 "$LINUX_SSH" true 2>/dev/null \
        || die "linux: '$LINUX_SSH' unreachable — set HA_NOVA_LINUX_SSH or bring it up"
      echo "  linux: $LINUX_SSH" ;;
    windows)
      # No emptiness guard: `${HA_NOVA_WINDOWS_SSH:-ha-nova-win}` already
      # substitutes the default for an empty value, so it was unreachable. The
      # probe below is what actually protects the attestation — windows cannot
      # be skipped, and must never be marked true from a host that did not run it.
      ssh -o BatchMode=yes -o ConnectTimeout=8 "$WINDOWS_SSH" 'cmd /c "echo ok"' >/dev/null 2>&1 \
        || die "windows: '$WINDOWS_SSH' unreachable. Do not guess an address — ask the hypervisor for the guest's CURRENT one and fix the ssh alias." ;;
    *) die "unknown platform '$platform' in cloud_remote_platforms" ;;
  esac
done

if [ "$MODE" = "--dry-run" ]; then
  echo ""
  echo "[cloud-evidence] dry run: target resolved and every platform answered."
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
# One dispatch per PR; reruns are rejected, so an existing successful run for
# this exact commit is reused instead of burning the second attempt.
skill_version="$(git -C "$ROOT_DIR" show "$target_commit:version.json" | jq -r '.skill_version // empty')"
[ -n "$skill_version" ] \
  || die "version.json at the target has no skill_version — refusing to dispatch a nonsense version_tag"
version_tag="${HA_NOVA_VERSION_TAG:-v${skill_version}-rc1}"
step "candidate bundle ($version_tag)"
# A workflow_dispatch run's headSha is the SHA of the ref it ran FROM — main —
# never the synthetic merge commit, so keying reuse on it can never match. The
# only thing tying a run to this target is the request_id in its run-name, so
# that id is DERIVED from the target commit rather than from $$: the same
# invocation twice produces the same id and finds its own earlier run instead
# of dispatching again into cancel-in-progress.
request_id="pr${PR}-${target_commit:0:12}"
run_id="$(gh run list --repo "$REPO" --workflow cloud-candidate-bundle.yml \
  --json databaseId,conclusion,displayTitle --limit 30 \
  | jq -r --arg r "$request_id" \
      'map(select(.conclusion=="success" and (.displayTitle|contains($r)))) | first | .databaseId // empty')"

if [ -n "$run_id" ]; then
  echo "  reusing successful run $run_id"
else
  echo "  dispatching (request_id=$request_id)"
  gh workflow run cloud-candidate-bundle.yml --repo "$REPO" \
    -f "pull_request=$PR" -f "version_tag=$version_tag" -f "request_id=$request_id" \
    || die "dispatch rejected — inspect the workflow before dispatching again"
  echo "  waiting for the run to appear"
  for _ in $(seq 1 30); do
    sleep 10
    run_id="$(gh run list --repo "$REPO" --workflow cloud-candidate-bundle.yml \
      --json databaseId,displayTitle --limit 5 \
      | jq -r --arg r "$request_id" 'map(select(.displayTitle|contains($r))) | first | .databaseId // empty')"
    [ -n "$run_id" ] && break
  done
  [ -n "$run_id" ] \
    || die "the dispatched run never appeared within 300s. It may still be queued — find it with: gh run list --workflow cloud-candidate-bundle.yml --repo $REPO | grep $request_id"
  echo "  run $run_id — waiting for completion"
  gh run watch "$run_id" --repo "$REPO" --exit-status >/dev/null \
    || die "run $run_id failed — fix the cause in a reviewed PR, then dispatch once more"
fi

work="$(mktemp -d)"
# Keep the work dir on failure: every provenance `die` names a log inside it,
# and deleting it on the way out destroyed the diagnostics for the most
# expensive stage of the run.
trap '[ "${KEEP_WORK:-0}" = 1 ] && echo "[cloud-evidence] logs kept in $work" || rm -rf "$work"' EXIT
step "downloading install bundles"
gh run download "$run_id" --repo "$REPO" --name cloud-candidate-install-bundles --dir "$work/bundles" \
  || die "cannot download the install bundles from run $run_id (artifacts expire after 7 days)"

# CI verified these checksums before upload; recomputing them after the
# download binds the exact local bytes the provenance runs below will execute.
step "verifying the artifact's checksums"
checksum_count=0
for checksum in "$work"/bundles/*.sha256; do
  [ -f "$checksum" ] || die "no .sha256 files in the artifact — refusing unverifiable bundles"
  (cd "$work/bundles" && shasum -a 256 --check "$(basename "$checksum")" >/dev/null) \
    || die "checksum mismatch in $(basename "$checksum") — the download does not match what CI built"
  checksum_count=$((checksum_count + 1))
done
echo "  $checksum_count checksum file(s) verified"

# ── 3. provenance, per platform ──────────────────────────────────────────────
# The positive path: the INSTALLED bundle must accept its own provenance. CI
# only proves the negative (raw binaries reject it), so this cannot be skipped.
expected_version="${version_tag#v}"

# Both remotes print the bundle manifest between markers. Without them the
# extraction is guesswork: PowerShell wraps its progress stream in CLIXML and
# interleaves it with stdout, so "the first line that ends in }" is not the
# manifest.
BEGIN_MARK="<<<HA-NOVA-BUNDLE"
END_MARK="HA-NOVA-BUNDLE>>>"

extract_manifest() {  # <raw-output-file>
  # Strip CR first: Windows sends CRLF, so an exact match against the marker
  # compares "<<<HA-NOVA-BUNDLE\r" and never fires. The raw stream also carries
  # ssh's own warnings, which is precisely why the markers exist.
  tr -d '\r' <"$1" \
    | awk -v b="$BEGIN_MARK" -v e="$END_MARK" '$0==b{f=1;next} $0==e{f=0} f'
}

check_identity() {  # <manifest-file>
  node -e '
    const fs = require("node:fs");
    const [path, version, tree] = process.argv.slice(1);
    const raw = fs.readFileSync(path, "utf8").trim();
    if (!raw) throw new Error("no bundle manifest in the remote output");
    const bundle = JSON.parse(raw);
    if (bundle.version !== version || bundle.cloud_release?.source_tree_sha !== tree) {
      throw new Error(
        `bundle identity mismatch: ${bundle.version} / ${bundle.cloud_release?.source_tree_sha}`,
      );
    }
  ' "$1" "$expected_version" "$target_tree"
}

provenance_unix() {  # <label> <archive> <ssh-host|"">
  local label="$1" archive="$2" host="$3"
  local script="
    set -euo pipefail
    d=\"\$(mktemp -d)\"; mkdir -p \"\$d/home/.local/share\"
    tar -xzf \"\$1\" -C \"\$d/home/.local/share\"
    echo '$BEGIN_MARK'
    cat \"\$d/home/.local/share/ha-nova/bundle.json\"
    echo
    echo '$END_MARK'
    # Unix builds resolve their install root from HOME, so the check passes
    # only from an installed layout — calling the extracted binary in place
    # fails with 'official Cloud release provenance is not enabled'.
    HOME=\"\$d/home\" HA_NOVA_NO_CENSUS=1 \"\$d/home/.local/share/ha-nova/ha-nova\" internal-cloud-release-check
    rm -rf \"\$d\"
  "
  if [ -z "$host" ]; then
    bash -c "$script" _ "$archive" >"$work/$label.out" 2>&1
  else
    scp -q -o BatchMode=yes "$archive" "$host:ha-nova-candidate.tar.gz" \
      || die "$label: cannot copy the bundle to $host"
    # `bash -s` has no $0 slot: arguments land on $1 directly, unlike the
    # `bash -c "$script" _ "$archive"` form used for the local run.
    local status=0
    ssh -o BatchMode=yes "$host" "bash -s ha-nova-candidate.tar.gz" <<<"$script" \
      >"$work/$label.out" 2>&1 || status=$?
    # Capture BEFORE cleaning up. Called in an `|| die` list, errexit is off in
    # here, so a cleanup that succeeds would otherwise become the function's
    # status and a failed provenance run would read as a pass.
    ssh -o BatchMode=yes "$host" "rm -f ha-nova-candidate.tar.gz" >/dev/null 2>&1 || true
    return $status
  fi
}

provenance_windows() {  # <archive>
  local archive="$1"
  scp -q -o BatchMode=yes "$archive" "$WINDOWS_SSH:ha-nova-candidate.zip" \
    || die "windows: cannot copy the bundle to $WINDOWS_SSH"
  # -EncodedCommand, not -Command: a multi-line script does not survive the
  # ssh argument boundary and PowerShell answers with its usage text.
  local ps encoded
  ps="\$ErrorActionPreference='Stop'
\$ProgressPreference='SilentlyContinue'
\$d = Join-Path \$env:TEMP ('ha-nova-' + [guid]::NewGuid())
Expand-Archive -LiteralPath ha-nova-candidate.zip -DestinationPath \$d
\$root = Join-Path \$d 'ha-nova'
Write-Output '$BEGIN_MARK'
Get-Content (Join-Path \$root 'bundle.json') -Raw
Write-Output '$END_MARK'
\$env:HA_NOVA_NO_CENSUS = '1'
& (Join-Path \$root 'ha-nova.exe') internal-cloud-release-check
if (\$LASTEXITCODE -ne 0) { throw 'provenance check failed' }
Remove-Item -Recurse -Force \$d"
  encoded="$(printf '%s' "$ps" | iconv -f UTF-8 -t UTF-16LE | base64 | tr -d '\n')"
  local shell="${HA_NOVA_WINDOWS_PWSH:-powershell}"
  local status=0
  ssh -o BatchMode=yes "$WINDOWS_SSH" "$shell -NoProfile -EncodedCommand $encoded" \
    >"$work/windows.out" 2>&1 || status=$?
  ssh -o BatchMode=yes "$WINDOWS_SSH" 'cmd /c "del ha-nova-candidate.zip"' >/dev/null 2>&1 || true
  return $status
}

for platform in $platforms; do
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
  # file may have been edited in between. The envelope that gets written must
  # be the one that passed.
  validate_envelope "$ENVELOPE_FILE" "$target_tree" "$target_commit" "$relay_version" "$platforms"
  envelope="$(jq -c . "$ENVELOPE_FILE")" || die "cannot re-read $ENVELOPE_FILE"
  step "envelope (validated against this target)"
  echo "$envelope" | jq .
else
  keyrings_false="$(for p in $platforms; do printf '{"%s":false}' "$p"; done | jq -s 'add')"
  envelope="$(jq -n \
    --arg commit "$target_commit" --arg tree "$target_tree" \
    --arg relay "$relay_version" --argjson keyrings "$keyrings_false" '
    {
      schema: 2,
      commit_sha: $commit,
      tree_sha: $tree,
      relay_app: { version: $relay, source_commit: $commit, source_tree_sha: $tree },
      checks: {
        parity: false, stress_10000: false, keyrings: $keyrings,
        roles: false, domains_mfa: false, lifecycle: false,
        redirects_non_disclosure: false, installed_relay_app: false,
        routing: false, signing_and_update_matrix: false
      }
    }')"
  step "envelope TEMPLATE — every attestation is false on purpose"
  echo "$envelope" | jq .
  echo ""
  echo "  This run proved: artifact checksums, plus one internal-cloud-release-check"
  echo "  per enabled platform ($(tr '\n' ' ' <<<"$platforms")), each from an installed"
  echo "  layout. That is the execution backing for signing_and_update_matrix; every"
  echo "  other boolean (and keyrings per OS) is a qualification decision this script"
  echo "  will not make. Edit each value you can attest to true, save the file,"
  echo "  record the ledger in the PR, then re-run with: --set --envelope <file>"
fi

if [ "$MODE" = "--set" ]; then
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
