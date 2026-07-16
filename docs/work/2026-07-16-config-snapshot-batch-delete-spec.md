# Spec: Config Snapshot Batch Delete

Status: active
Date: 2026-07-16

## Problem

Config snapshot deletion supports only one typed confirmation per blob. Cleaning several obsolete snapshots therefore repeats the same preview/confirmation cycle even though every target uses the same generic Relay blob-store operation.

## Decision

- The `ha-nova` context skill owns listing and deleting config snapshot blobs; restoring a snapshot still routes to the skill that owns the captured Home Assistant item.
- Explicitly opt exact config snapshot blob deletion into `skills/ha-nova/batch-safety.md` as the `config-snapshots` resource family with the config-item cap of 20.
- Snapshot categories may share one manifest because category is blob metadata, not a different mutation family. The manifest may contain only literal snapshot files from a fresh list and only `action:delete` operations.
- Execute one `POST /backups` delete per file in deterministic order, fail fast, and verify each deleted file is absent from a fresh list before continuing.
- Preserve one unselected snapshot as an unrelated-resource invariant when available. A deleted snapshot has no rollback, but deleting it never changes the underlying Home Assistant config.
- Keep prune separate: never turn a category, age, prefix, or `keep_named:false` selector into a post-confirmation batch expansion.

## Acceptance

- One manifest and one `confirm:batch-delete-config-snapshots-<count>-<digest>` code can authorize up to 20 exact snapshot files across categories.
- Missing, changed, or newly selected files invalidate the preview; no wildcard or late selector expansion is allowed.
- Execution is sequential, per-file verified, fail-fast, and reports succeeded / failed / not attempted.
- Single-snapshot delete and prune behavior remain unchanged.
- Context dispatch, capability matrix, and contract tests pin the opt-in.
