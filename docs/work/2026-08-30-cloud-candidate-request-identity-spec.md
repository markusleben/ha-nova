# Cloud Candidate Request Identity

Status: active

## Goal

Prevent a successful or in-flight Cloud Candidate run for another pull request
or merge identity from being watched or reused, even when both candidates have
the same Git tree.

## Contract

- The request identity is the pull request number plus the complete base, head,
  synthetic merge, and workflow-tree SHAs.
- The trusted-main resolver path must validate those SHAs against one pull
  request resolution and its fetched synthetic-merge workflow tree.
- Concurrency cancellation must match the pull request, version, and complete
  request identity, so a stale request cannot cancel the exact run.
- Local run selection must match the evaluated run name, the trusted-main
  workflow head, `workflow_dispatch`, and attempt 1 before watch, download, or
  reuse.
- Candidate discovery must paginate the complete final-artifact retention
  window and fail closed if GitHub reports more filtered results than it can
  return.
- When several exact runs exist, selection uses the highest run ID. Individual
  run metadata must still match the complete selector and attempt 1 before and
  after watch, and before and after artifact download.
- Bundle version, signature, checksum, tree, and provenance checks remain
  mandatory after run selection.
- A foreign run never delays or replaces a fresh exact-identity dispatch.

## Verification

Behavior tests use raw paginated GitHub workflow-run responses, distinct real
base, head, and synthetic merge commits sharing one tree, and real bundle
archives. They cover shifted identity fields, foreign successful and in-flight
runs, an exact run behind more than 30 foreign runs, competing exact runs,
attempt changes, exact reuse, and `--set`.
