# Hermes Hardening Spec

Date: 2026-04-17

## Goal

Close the concrete Hermes review findings without widening scope:

- prevent native Windows `dev-sync` failures for Hermes
- detect incomplete Hermes bundles as detached/incomplete
- fail loud when Hermes source payload is incomplete during install
- clarify the WSL2-only Hermes-on-Windows workflow in shared docs
- add regression coverage for Hermes install, doctor, uninstall, and onboarding contracts

## Non-Goals

- no new Hermes provider capabilities beyond the already-added client support
- no relay changes
- no redesign of non-Hermes client health checks

## Planned Changes

1. Add a Hermes bundle completeness helper and use it for status/detection.
2. Make Hermes install fail when the shipped source context skill is missing.
3. Teach `dev-sync` to skip Hermes on native Windows with an explicit WSL2 note.
4. Extend macOS RC validation and Windows cleanup helpers for Hermes-specific paths.
5. Tighten README / Hermes overlay / shared update guide wording for WSL2.
6. Add regression tests for:
   - incomplete Hermes bundle not counting as ready
   - Hermes doctor repair hint
   - Hermes uninstall cleanup
   - Hermes missing source payload failure
   - Hermes onboarding/install shell coverage, including native Windows rejection
