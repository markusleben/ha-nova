# 2026-03-18: Release Docs Windows Parity

## Problem

The current worktree has enough Windows support to justify a larger release, but the release-facing markdown still mixes platform support, client-lane validation, and experimental caveats inconsistently.

## Goals

- Make release-facing docs agree on what Windows support means in this release.
- Keep platform support and per-client validation clearly separated.
- Ensure Windows install docs are actionable, not prose-only.
- Add explicit PR/release-note guidance for maintainers so future platform releases do not drift again.

## Approach

- Update `README.md`, `PROJECT.md`, and client install docs to use one support story.
- Add the missing Windows PowerShell install command and ARM64 caveat to the client install docs.
- Extend `docs/releasing.md` with a release-notes section and a stricter docs gate.
- Add the same support caveats to the generated GitHub release header.
