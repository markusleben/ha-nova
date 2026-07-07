# Release Note Refresh Spec

Date: 2026-04-12
Status: completed

## Goal

Refresh the existing release-note comparison from the old provisional target `ea8a708` to the current `main` SHA and confirm whether any newer commits change the public release-note bullets.

## Scope

- Re-check the compare window from `v0.3.2` to current `main`.
- Classify newer post-`ea8a708` commits.
- Confirm whether the public release-note draft changes.
- Keep the release-note body short and user-facing.

## Current Compare Target

- Baseline release: `v0.3.2`
- Current target branch: `main`
- Current target SHA at refresh: `b5e94aa`
- Compare window: `v0.3.2..b5e94aa`

## Default Applied

- Docs-only and docs-contract-only follow-up commits do not become public release-note bullets unless they change shipped user behavior in a way users must act on.

## Deliverables

- Refreshed comparison artifact with the new SHA and updated omission matrix.
- Refreshed release-note draft if needed.
