# Spec: Blocked Files in Local Verify

Date: 2026-03-25

## Problem

CI already rejects tracked local-only files like `docs/choices.md` and `docs/breadcrumbs.md`.
Local `npm run verify` did not run the same guard, so a force-add could still reach a PR and fail only in GitHub.

## Decision

Add a local blocked-files verification step to the canonical `npm run verify` command.

## Scope

- Add `scripts/release/verify-blocked-files.sh`
- Keep the blocked list aligned with CI repo-hygiene
- Make `npm run verify` fail locally before push if blocked files are tracked

## Non-Goals

- This does not replace `.gitignore`
- This does not replace CI repo-hygiene
- This does not add a Git hook requirement
