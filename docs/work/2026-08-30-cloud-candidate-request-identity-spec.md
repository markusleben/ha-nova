# Cloud Candidate Request Identity

Status: active

## Goal

Prevent a successful or in-flight Cloud Candidate run for another pull request
or merge identity from being watched or reused, even when both candidates have
the same Git tree.

## Contract

- The request identity is the pull request number plus the complete base, head,
  and synthetic merge SHAs.
- The trusted-main workflow resolver must derive those SHAs from one pull
  request resolution and reject any different request identity.
- Local run selection must match the evaluated run name, the trusted-main
  workflow head, `workflow_dispatch`, and attempt 1 before watch, download, or
  reuse.
- Bundle version, signature, checksum, tree, and provenance checks remain
  mandatory after run selection.
- A foreign run never delays or replaces a fresh exact-identity dispatch.

## Verification

Behavior tests use parsed GitHub run-list records and real bundle archives to
cover foreign successful and in-flight runs, two merge identities sharing one
tree, an exact successful run, and the `--set` reuse path.
