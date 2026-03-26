# 2026-03-23 Active Onboarding Contract Cleanup

## Goal

- Remove active doc drift after the Windows lifecycle / `winget` handoff refactor.

## Decisions

- Per-client install docs must describe the current Windows truth precisely:
  - public entrypoint stays `install.ps1`
  - a `winget` manifest is generated for each release
  - the public `winget` package is not live until that manifest is published and proven on a fresh Windows VM
- `CONTRIBUTING.md` must name the only remaining `scripts/onboarding/` helpers explicitly instead of implying the old shell onboarding family still exists.

## Non-Goals

- No retroactive rewrite of historical specs/plans.
- No new lifecycle behavior changes in this pass.
- Historical future-state docs must not be read as the current public Windows contract while `install.ps1` is still the live public path.
