# Spec: Update Detection Truth + Gemini Windows Release Claims

## Problem

- Release-facing docs still describe Gemini on Windows as experimental, but local validation has now confirmed Gemini works on Windows.
- Update behavior is easy to overstate: the common Go updater is shared across clients, but the automatic in-session `UPDATE AVAILABLE` banner is currently Claude-specific via SessionStart.
- Release notes and post-release docs must distinguish:
  - common update mechanism
  - automatic startup notice surface
  - exact validated Windows client lanes for this release

## Decisions

- Treat Gemini as a validated Windows client lane for this release.
- Keep Codex and OpenCode on Windows as experimental until explicitly smoke-validated.
- Describe automatic update detection truthfully:
  - all installs use `ha-nova check-update` / `ha-nova update`
  - Claude can surface the same update notice automatically via SessionStart
  - other clients currently use the shared CLI updater path without an equivalent startup banner claim

## Files

- `README.md`
- `PROJECT.md`
- `.gemini/INSTALL.md`
- `docs/releasing.md`
- `skills/ha-nova/update-guide.md`
- `tests/onboarding/client-install-docs-contract.test.ts`
- `tests/onboarding/release-contract.test.ts`
- `tests/skills/ha-nova-contract.test.ts`

## Verification

- Focused Vitest contracts for release/docs/update guidance
- `bash scripts/check-docs.sh`
