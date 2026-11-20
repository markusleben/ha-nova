# 2026-03-23 Release Readiness and Public Winget Gates

## Goal

- Finish the active repo cleanup around the new Go-owned lifecycle and make the Windows public-launch sequence explicit and hard-gated.

## Decisions

- Keep the active cleanup scope on product/docs/tests/workflows/reference docs, not archival specs.
- Treat the remaining shell-adjacent scripts as a narrow dev/compat surface, not a second product lifecycle.
- Keep final release notes in `.goreleaser.yml` and RC notes in `release-candidate.yml` aligned on the same lifecycle truth.
- Treat RC + private validation as the pre-publish gate.
- Treat `release.yml` smoke as post-publish confirmation only.
- Require warning-free `winget validate` plus a published-source fresh-VM proof before any public Windows doc flip.
- Split first-public install/check-update/uninstall proof from later `winget upgrade` continuity proof.

## Non-Goals

- No immediate public-doc switch to `winget`.
- No historical spec cleanup in this pass.
