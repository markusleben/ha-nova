# 2026-03-18 Release-Day Finalization

## Goal

Ship the current worktree today through the existing PR/release pipeline without reopening scope.

## Decisions

- Release version: `0.2.0`
- Release path: existing checksum-based CI/release flow
- Issue linkage: close only `#91` from this PR

## Why

- `v0.1.12` already exists, so a fresh tag is required
- The worktree is a larger product/runtime/platform release, not a patch-only delta
- Adding more release machinery today would increase risk without improving end-user UX
