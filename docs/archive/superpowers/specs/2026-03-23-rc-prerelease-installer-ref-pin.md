# RC Prerelease Installer Ref Pin

Date: 2026-03-23

## Problem

RC prerelease notes and docs still taught tester one-liners that fetched `install.sh` / `install.ps1` from a moving branch ref.
If that branch moved after RC publication, testers could run newer bootstrap code against older prerelease assets and get a false green installer proof.

## Decision

Pin RC installer one-liners to the exact publishing commit SHA or immutable tag.
Do not use a moving branch ref for prerelease installer validation guidance.

## Scope

- `.github/workflows/release-candidate.yml`
- `docs/releasing.md`
- `tests/onboarding/release-contract.test.ts`

## Why

The prerelease gate must prove the exact installer/bootstrap code that produced the published RC assets, not whatever happens to live on a branch later.
