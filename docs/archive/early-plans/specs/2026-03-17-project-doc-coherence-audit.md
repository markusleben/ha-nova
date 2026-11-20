# Project Doc Coherence Audit

Date: 2026-03-17

## Goal

Align active product docs and skill wording with the current local worktree before rollout.

## Problems

- `PROJECT.md` still described an older Claude path and older skill inventory.
- Windows support language drifted between `README.md` and per-client install docs.
- `docs/releasing.md` implied a second mandatory client-setup path after install.
- Active fallback wording still mixed `App` terminology with legacy `addon` phrasing.

## Scope

- Fix active-path documentation only.
- Add or adjust contract tests for the corrected product truth.

## Non-Goals

- No large pre-release refactor of oversized Go CLI files.
- No changes to historical plan docs unless they break active contracts.

## Verification

- Targeted docs contract tests
- `npm run verify`
