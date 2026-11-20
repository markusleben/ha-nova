# Release Notes Structure

Date: 2026-03-18
Status: implemented

## Goal

Make public release notes readable for end users, not just maintainers.

## Structure

Every public release should keep the same order:

1. `Why This Release Exists`
2. `What You Get`
3. `Install or Update`
4. `Upgrade Notes`
5. grouped change list

Grouped change list:

- `New Features`
- `Bug Fixes`
- `UX, Docs, and Refactors`
- `Internal Maintenance`

## Implementation

- encode the fixed top structure in `.goreleaser.yml`
- group changes by conventional commit type
- keep release process docs aligned in `docs/releasing.md`
- prompt PR authors for user-facing release inputs in `.github/PULL_REQUEST_TEMPLATE.md`
