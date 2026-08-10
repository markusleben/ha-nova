#!/usr/bin/env bash
# Build the exact-target Home Assistant Cloud evidence envelope for one PR.
#
# The envelope binds to a PR's synthetic merge tree, so every merge into main
# needs a fresh one (docs/releasing.md -> "Home Assistant Cloud publication
# gate"). Doing that by hand is ~20 steps across three machines, which is why
# it kept being the thing that blocked a ready PR for days.
#
#   bash scripts/release/build-cloud-evidence.sh 541 --dry-run
#   bash scripts/release/build-cloud-evidence.sh 541
#   bash scripts/release/build-cloud-evidence.sh 541 --set \
#     --carry "docs/tests-only delta, invalidation-map None row" \
#     --relay-app "run 12345678 on 0.9.0"
#
# It fills in ONLY what it actually ran: `signing_and_update_matrix`, backed by
# one real `internal-cloud-release-check` per enabled platform. That command
# proves the signed bundle and binary provenance; it does NOT exercise native
# secret storage, so it is no evidence for `keyrings`, which needs real
# happy-path and fail-closed behaviour per OS (docs/releasing.md:313-316).
# Every other boolean is a MAINTAINER decision:
#   --carry <reason>    the risk-scoped qualifications behind parity,
#                       stress_10000, keyrings, roles, domains_mfa, lifecycle,
#                       redirects_non_disclosure and routing still apply to
#                       this delta. Paste <reason> into the PR ledger.
#   --relay-app <ref>   `installed_relay_app` is exact-target and is NEVER
#                       carried forward (docs/releasing.md:345), so it needs a
#                       reference to the verification that was actually done.
# Without both, --set refuses. An envelope is an attestation, and this script
# must not sign one for work nobody performed.
#
# It refuses rather than guesses: a platform it cannot reach, a bundle whose
# identity does not match the run, or a provenance check that does not pass is
# a hard stop. The envelope ATTESTS those runs, so an unreachable platform must
# never become a `true`.
#
# Hosts for the non-local platforms come from the environment, defaulting to
# this repo's lab:
#   HA_NOVA_LINUX_SSH   (default: ai-machine)
#   HA_NOVA_WINDOWS_SSH (default: ha-nova-win)
#
# Both are ssh DESTINATIONS, so put the address, user and any HostKeyAlias in
# ~/.ssh/config rather than here. A lab guest's address changes; an alias does
# not, and this repo must not carry one maintainer's IP addresses.
set -euo pipefail

PR="${1:-}"; shift || true
MODE=""; CARRY=""; RELAY_APP=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run|--set) MODE="$1"; shift ;;
    --carry) [ $# -ge 2 ] || { echo "--carry needs a reason" >&2; exit 1; }; CARRY="$2"; shift 2 ;;
    --relay-app) [ $# -ge 2 ] || { echo "--relay-app needs a reference" >&2; exit 1; }; RELAY_APP="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done
REPO="${HA_NOVA_REPO:-markusleben/ha-nova}"
LINUX_SSH="${HA_NOVA_LINUX_SSH:-ai-machine}"
WINDOWS_SSH="${HA_NOVA_WINDOWS_SSH:-ha-nova-win}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

die() { KEEP_WORK=1; echo "[cloud-evidence] ERROR: $*" >&2; exit 1; }
step() { echo ""; echo "[cloud-evidence] $*"; }

[ -n "$PR" ] || die "usage: build-cloud-evidence.sh <pr-number> [--dry-run|--set]"
[[ "$PR" =~ ^[0-9]+$ ]] || die "PR must be a number, got '$PR' — it becomes an API path and a git ref"
command -v gh >/dev/null || die "gh is required"
command -v jq >/dev/null || die "jq is required"
# check_identity runs node AFTER all provenance — a missing node there reads as
# an identity mismatch, which is an attestation-critical message for a check
# that never happened.
command -v node >/dev/null || die "node is required"

active="$(gh api user --jq .login 2>/dev/null || true)"
[ "$active" = "${REPO%%/*}" ] || die "gh is authenticated as '${active:-none}', expected '${REPO%%/*}'"

# ── 1. the exact target ──────────────────────────────────────────────────────
step "resolving PR #$PR"
pr_json="$(gh api "repos/$REPO/pulls/$PR")"
state="$(jq -r .state <<<"$pr_json")"
mergeable="$(jq -r '.mergeable_state // "unknown"' <<<"$pr_json")"
merge_commit="$(jq -r '.merge_commit_sha // ""' <<<"$pr_json")"
[ "$state" = "open" ] || die "PR #$PR is $state"
case "$mergeable" in
  # `blocked` is the EXPECTED state here: cloud-source-gate is a required check
  # and it is red precisely because the envelope this script builds does not
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
[ "$(wc -l <<<"$relay_version")" = 1 ]
[ -n "$relay_version" ] || die "cannot read nova/config.yaml version at the target"
echo "  commit $target_commit"
echo "  tree   $target_tree"
echo "  relay  $relay_version"

platforms="$(git -C "$ROOT_DIR" show "$target_commit:version.json" | jq -r '.cloud_remote_platforms[]')"
[ -n "$platforms" ] \
  || die "version.json lists no enabled cloud_remote_platforms — there is nothing to verify, and an envelope built here would attest checks that never ran"
echo "  enabled platforms: $(tr '\n' ' ' <<<"$platforms")"

if [ "$MODE" = "--dry-run" ]; then
  echo ""
  echo "[cloud-evidence] dry run: target resolved, platforms reachable."
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
version_tag="${HA_NOVA_VERSION_TAG:-v$(git -C "$ROOT_DIR" show "$target_commit:version.json" | jq -r .skill_version)-rc1}"
step "candidate bundle ($version_tag)"
# Match on the run's headSha, not on a substring of its title: the title
# carries the request_id, whose short SHA is 7 chars while this compared 8, so
# it never matched and every invocation re-dispatched — and the workflow's
# cancel-in-progress then killed the run still in flight.
run_id="$(gh run list --repo "$REPO" --workflow cloud-candidate-bundle.yml \
  --json databaseId,headSha,conclusion --limit 20 \
  | jq -r --arg c "$target_commit" \
      'map(select(.conclusion=="success" and .headSha==$c)) | first | .databaseId // empty')"

if [ -n "$run_id" ]; then
  echo "  reusing successful run $run_id"
else
  request_id="pr${PR}-$(git -C "$ROOT_DIR" rev-parse --short "$target_commit")-$$"
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
      [ "$(uname -s)" = "Darwin" ] || die "darwin provenance must run on macOS; this is $(uname -s)"
      step "provenance: darwin ($arch, local)"
      provenance_unix darwin "$archive" "" || die "darwin: provenance failed — see $work/darwin.out"
      ;;
    linux)
      archive="$work/bundles/ha-nova-installer-bundle-linux-amd64.tar.gz"
      [ -f "$archive" ] || die "linux: $archive missing from the artifact"
      step "provenance: linux (ssh $LINUX_SSH)"
      ssh -o BatchMode=yes -o ConnectTimeout=8 "$LINUX_SSH" true 2>/dev/null \
        || die "linux: '$LINUX_SSH' unreachable — set HA_NOVA_LINUX_SSH or bring it up"
      provenance_unix linux "$archive" "$LINUX_SSH" || die "linux: provenance failed — see $work/linux.out"
      ;;
    windows)
      archive="$work/bundles/ha-nova-installer-bundle-windows-amd64.zip"
      [ -f "$archive" ] || die "windows: $archive missing from the artifact"
      step "provenance: windows (ssh $WINDOWS_SSH)"
      [ -n "$WINDOWS_SSH" ] \
        || die "windows: set HA_NOVA_WINDOWS_SSH. It is an ATTESTED platform — it cannot be skipped, and it must not be marked true from a machine that did not run it."
      ssh -o BatchMode=yes -o ConnectTimeout=8 "$WINDOWS_SSH" 'cmd /c "echo ok"' >/dev/null 2>&1 \
        || die "windows: '$WINDOWS_SSH' unreachable. Do not guess an address — ask the hypervisor for the guest's CURRENT one and fix the ssh alias."
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
step "envelope"
# Decide `carried` FIRST: the keyrings object below expands it, and under
# `set -u` an unset variable aborts here — after every provenance run has
# already been paid for.
if [ -n "$CARRY" ] && [ -n "$RELAY_APP" ]; then
  carried=true
else
  carried=false
fi
# Every platform above passed, which is what backs `signing_and_update_matrix`.
# `keyrings` is a DIFFERENT contract — native secret storage — so it is carried
# like the others, never derived from a provenance run.
keyrings="$(for p in $platforms; do printf '{"%s":%s}' "$p" "$carried"; done | jq -s 'add')"

envelope="$(jq -n \
  --arg commit "$target_commit" --arg tree "$target_tree" \
  --arg relay "$relay_version" --argjson keyrings "$keyrings" \
  --argjson carried "$carried" '
  {
    schema: 2,
    commit_sha: $commit,
    tree_sha: $tree,
    relay_app: { version: $relay, source_commit: $commit, source_tree_sha: $tree },
    checks: {
      parity: $carried, stress_10000: $carried, keyrings: $keyrings,
      roles: $carried, domains_mfa: $carried, lifecycle: $carried,
      redirects_non_disclosure: $carried, installed_relay_app: $carried,
      routing: $carried, signing_and_update_matrix: true
    }
  }')"
echo "$envelope" | jq .
echo ""
echo "  VERIFIED HERE: signing_and_update_matrix — one real"
echo "                 internal-cloud-release-check per enabled platform"
echo "                 ($(tr '\n' ' ' <<<"$platforms")), each from an installed layout."
if [ "$carried" = true ]; then
  echo "  CARRIED:       parity, stress_10000, keyrings, roles, domains_mfa,"
  echo "                 lifecycle, redirects_non_disclosure, routing"
  echo "                 (keyrings is native-secret behaviour per OS — the"
  echo "                  provenance runs above are NOT evidence for it)"
  echo "                 reason: $CARRY"
  echo "  RELAY APP:     $RELAY_APP"
  echo "                 (installed_relay_app is exact-target and never carried;"
  echo "                  this reference must name a real verification)"
else
  echo "  NOT ATTESTED:  everything else is false. Pass --carry <reason> and"
  echo "                 --relay-app <ref> once you have made those decisions."
fi

if [ "$MODE" = "--set" ] && [ "$carried" != true ]; then
  die "refusing --set: the envelope would attest checks nobody verified. Supply --carry <reason> and --relay-app <ref>, or set the secret by hand."
fi
if [ "$MODE" = "--set" ]; then
  step "writing HA_NOVA_CLOUD_GATE_EVIDENCE_JSON to the production environment"
  gh secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --repo "$REPO" --env production --body "$envelope" \
    || die "cannot write the secret — admin auth required"
  echo "  written. Rerun CI on #$PR, then cloud-source-gate should pass."
else
  echo ""
  echo "[cloud-evidence] not written. Re-run with --set, or paste the JSON above into"
  echo "                 the production environment secret HA_NOVA_CLOUD_GATE_EVIDENCE_JSON."
fi

echo ""
echo "[cloud-evidence] The check booleans above carry the risk-scoped qualifications"
echo "                 from docs/work/2026-07-30-cloud-release-evidence-risk-scope-spec.md."
echo "                 Inspect the qualification-to-target diff before merging, and record"
echo "                 the ledger in the PR — the verifier does not make that decision."
