# 2026-03-25 Release GH_TOKEN Guard

## Problem

The final `Release` workflow failed after the `v0.3.1` tag push.
`scripts/release/verify-next-release-version.sh` uses `gh api`, but the workflow step only exported `HA_NOVA_ALLOW_EXISTING_RELEASE_TAG=1`.
In GitHub Actions, `gh` requires `GH_TOKEN`.

## Decision

Set `GH_TOKEN: ${{ github.token }}` in every workflow step that calls `scripts/release/verify-next-release-version.sh`.

## Scope

- `.github/workflows/release.yml`
- `.github/workflows/release-candidate.yml`
- `tests/onboarding/release-contract.test.ts`

## Reason

This is a live release-path failure, not an internal-only process detail.
The same helper is used in both final and RC publish flows, so both workflows must provide the token consistently.
