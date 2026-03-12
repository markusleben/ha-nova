# Spec: Post-Merge Codex Findings Cleanup

Date: 2026-03-12

## Problem

PR #75 left two Codex findings functionally unaddressed in the shipped code:

1. Gemini readiness checks can treat a flat-copy install as complete even if required companion markdown files are missing.
2. Local sync behavior around Gemini roots is inconsistent with the current install layout and legacy migration path.

## Decision

Ship a small follow-up fix:

- make Gemini setup readiness validate required companion markdown files per sub-skill
- make `dev-sync` refresh file-based clients via the local installer again, with:
  - Codex/OpenCode guarded by symlink markers
  - Gemini detected via current `~/.gemini/skills/...` and legacy `~/.agents/skills/...`

## Scope

- `scripts/onboarding/macos-lib.sh`
- `scripts/dev-sync.sh`
- focused onboarding regression tests

## Non-Goals

- no new installer UX work
- no broader refactor of onboarding state handling
