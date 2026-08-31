# Sensitive Workflow Bootstrap Spec

Status: active

## Goal

Permit one explicitly approved content-only change to one existing
Cloud-sensitive workflow while preserving the default deny policy and binding
the approval and evidence to the exact pull-request state.

## Plan checklist

- [x] **KISS — pass:** Extend the existing resolver, workflow-tree verifier,
  target gate, and evidence builder; add no workflow, service, secret, label,
  framework, or release path.
- [x] **YAGNI — pass:** Permit exactly one existing sensitive workflow content
  change. Additions, deletions, renames, mode changes, multiple workflow
  changes, forks, merge-queue targets, reruns, and stale state remain denied.
- [x] **DRY — pass:** Reuse `resolve-cloud-candidate-source.sh` for pull-request,
  review, check, pagination, identity, and final-ref validation;
  `verify-cloud-workflow-uses-only.mjs` for workflow-tree comparison;
  `verify-cloud-target-source-gate.sh` for candidate and broker routing; and
  `verify-cloud-release-gate.sh` for evidence validation.
- [x] **Fail-fast — pass:** Every absent, malformed, stale, or mismatched tuple,
  API result, ref, workflow delta, or evidence identity exits non-zero.
- [x] **Retry-safe — pass:** The canonical approval ID is deterministic for one
  exact state. Repeating that dispatch adopts the same successful candidate;
  any PR, base, head, merge, or workflow-tree movement creates a different ID.

## Tightened plan

- Keep the current default deny and existing non-sensitive `uses:`-only path.
- Read candidate authority only from `GITHUB_EVENT_PATH.inputs.request_id`.
- Bind approval to
  `pr<PR>-<BASE_SHA>-<HEAD_SHA>-<MERGE_SHA>-<WORKFLOWS_TREE_SHA>`.
- Allow the candidate resolver to select the one-sensitive-workflow verifier
  only after the canonical approval matches.
- Allow the broker/release path only when schema-valid evidence names the exact
  synthetic merge commit and its complete source tree; identical-tree or stale
  evidence is insufficient.
- Keep merge-queue sensitive changes fail-closed.

## Existing code reused

- Candidate identity, maintainer identity, exact checks, Codex verdict,
  pagination, and final ref revalidation:
  `scripts/release/resolve-cloud-candidate-source.sh`.
- Workflow entry, path, mode, and action-pin validation:
  `scripts/release/verify-cloud-workflow-uses-only.mjs`.
- Trusted target fetch and candidate/broker split:
  `scripts/release/verify-cloud-target-source-gate.sh`.
- Exact evidence schema, commit, tree, and Relay App identity:
  `scripts/release/verify-cloud-release-gate.sh` and
  `scripts/release/cloud-evidence-envelope.sh`.
- Deterministic dispatch and artifact reuse:
  `scripts/release/build-cloud-evidence.sh`.

## New behavior

1. One strict workflow-diff mode for exactly one existing sensitive workflow
   content change.
2. One canonical approval-ID check at candidate resolution.
3. One exact-commit-and-tree evidence mode for the trusted broker path.

## Risk

- **One-way doors:** None in this local change. No remote state is mutated.
- **Recommended order:** Validate tuple and workflow shape first; validate exact
  evidence second; revalidate API and refs immediately before completion.
