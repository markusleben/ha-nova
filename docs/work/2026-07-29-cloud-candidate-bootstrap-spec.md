# Cloud Candidate Bootstrap

Status: implemented; awaiting GitHub review.

## Problem

Cloud activation requires real-device evidence from an official bundle bound
to the exact pull-request merge commit. The existing Release Candidate
workflow verifies that evidence before it builds official bundles, so it
cannot produce the first candidate needed to collect the evidence.

## Required behavior

- Add one manually dispatched workflow that builds but never publishes.
- Run the workflow only from the current `main` branch.
- Accept one open same-repository pull request targeting the current `main`.
- Resolve and bind GitHub's exact synthetic pull-request merge commit, tree,
  base parent, and head parent.
- Require the current `main` commit to be an ancestor of the pull-request head,
  so the head-bound checks cover the exact candidate tree.
- Require the latest run of every current protected check on the pull-request
  head except `cloud-source-gate` to pass. That one check is expected to fail
  until this candidate produces evidence. Bind check runs to the exact
  workflow and event, expected App, pull request, current base/head, and merge
  target; reject all same-name commit statuses and revalidate the complete
  check state before returning.
- Keep paginated GitHub API payloads out of process arguments so normal check
  history cannot exceed the operating system's argument-size limit.
- Require a real clean Codex bot result bound to the current pull-request head;
  an advisory workflow timeout is insufficient. Reject requested changes,
  unresolved threads, and later Codex findings.
- Re-run the trusted source policy without external evidence and accept only
  the expected evidence-pending state.
- Use trusted build and signing scripts from `main`; treat the candidate
  checkout only as source and bundle data.
- Keep shared trusted shell checks compatible with the macOS runner's Bash
  3.2.
- Build official-tag Linux, Windows, and signed macOS binaries, then sign
  exact-tree install-bundle provenance with protected production secrets.
- Sign with the exactly validated Developer ID common name, temporarily make
  only its keychain user-searchable, and restore the prior keychain search
  list during cleanup.
- Smoke the exact raw binaries natively on Linux, macOS, and Windows before
  provenance signing; raw binaries must reject missing Cloud provenance.
  Windows negative smokes must accept only the documented exit code and exact
  complete message, then clear that expected native rejection before the
  workflow step ends.
- After all native smokes, revalidate the complete reviewed state, build the
  final hash-bound signed bundles, verify every archive's binary identity and
  signed platform/architecture provenance, then revalidate again immediately
  before upload.
- Serialize duplicate dispatches for one pull request.
- Retain Cloud-ineligible raw transport artifacts for one day and final
  candidate bundles for seven days.
- Never create or move a tag, release, draft release, deployment, or public
  asset.
- Expose no Cloud-runnable bundle before smoke tests and final revalidation.
- Never execute candidate code in an artifact-producing or secret-bearing job;
  the first positive signed-runtime check uses the downloaded final artifact
  in the real-device matrix.
- Fail closed on a moved pull request, stale base, missing merge ref, source
  mismatch, failed check, invalid version, or missing signing secret.
- Preserve evidence across the required squash merge only when the resulting
  full Git tree is identical to the tested synthetic merge tree.

## KISS boundary

Reuse the existing RC binary, Darwin signing, bundle, provenance, and smoke
contracts. Add no service, persistent state, retry loop, release mode, or
automatic dispatch. One explicit run creates one immutable candidate.

## Acceptance

- Static contracts reject publication commands and untrusted script execution.
- Resolver tests cover valid source selection plus repository, branch, base,
  merge-ref, parent, and check failures.
- Existing release workflows retain their evidence-first publication gate.
- The explicit dispatch by the exact maintainer account is the solo-project
  approval when GitHub reports `REVIEW_REQUIRED`; requested changes and
  unresolved threads still fail closed.
- The complete local release-contract suite passes before one reviewed
  bootstrap workflow run is allowed.
- The live main-protection check passes when invoked with `/bin/bash` on
  macOS.
