# Hermes Platform Evidence Docs Spec

Date: 2026-04-20
Status: implemented

## Goal

Reflect Hermes support and Linux secure-storage recovery with an explicit support-evidence model instead of scattered install notes or broad platform claims.

## Scope

- update the active next-release notes draft
- tighten README and `.hermes/INSTALL.md` around supported path vs current proof
- add a living Hermes platform validation reference doc
- add a structured community validation issue template
- align the Linux live-validation helper and release runbook on a generic default setup command
- lock the new posture with onboarding/release contract tests
- record the opinionated defaults in `docs/choices.md` and `docs/breadcrumbs.md`

## Non-Goals

- no new runtime behavior
- no retroactive edits to already-published historical release bodies
- no promise that every Linux Secret Service backend already has the same inline recovery coverage
- no host-specific validation data in the repo

## Planned Changes

1. Keep Hermes support visible in README/release notes, but state the current maintainer-validated Linux proof explicitly.
2. Rewrite `.hermes/INSTALL.md` around an evidence ladder: `Supported path`, `Maintainer-validated`, `Community validation`, `Known limitation`, `Planned / not yet validated`.
3. Add `docs/reference/hermes-platform-validation.md` as the active per-platform truth source.
4. Add `.github/ISSUE_TEMPLATE/hermes-platform-validation.yml` so community reports arrive in a structured, privacy-safe format.
5. Make `scripts/smoke/linux-headless-setup-check.sh` default to generic `ha-nova setup`, with explicit `HA_NOVA_LIVE_SETUP_CMD` override for Hermes proofs.
6. Update release/onboarding contract tests so future edits cannot silently drift back to vague or over-broad claims.

## Exit Criteria

- active release-notes draft names Hermes support and Linux GNOME Keyring recovery as user-facing features without overstating cross-platform proof
- README and `.hermes/INSTALL.md` distinguish support intent from current maintainer evidence
- active docs include a living Hermes validation matrix plus a community validation intake path
- the Linux SSH/headless helper no longer assumes Hermes by default
- targeted onboarding/release contract tests pass on the new wording and helper defaults
