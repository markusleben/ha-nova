# Onboarding Obsolete Removal Design

## Goal
Remove obsolete onboarding paths before public release. Keep only the MVP command surface for macOS onboarding.

## Scope
- Keep: `setup`, `doctor`, `env`
- Remove: `start`, `codex`, `claude`
- Keep local skill installation via `install-local-skills.sh` for client parity.
- Remove obsolete wrapper `install-codex-skill.sh` and use one installer entrypoint.

## Decisions
- No backward-compatibility aliases for removed onboarding commands.
- No launcher commands inside onboarding script; normal client startup remains external.
- Docs and tests must match the reduced command surface exactly.

## Implementation
1. Split monolithic onboarding script into:
   - `scripts/onboarding/macos-lib.sh`
   - `scripts/onboarding/macos-setup.sh`
   - `scripts/onboarding/macos-doctor.sh`
   - `scripts/onboarding/macos-env.sh`
2. Convert `scripts/onboarding/macos-onboarding.sh` to a thin dispatcher for only `setup|doctor|env`.
3. Remove `scripts/onboarding/install-codex-skill.sh` and move npm command to shared installer.
4. Update `.codex/INSTALL.md`, onboarding docs, choices, breadcrumbs, and contract tests.

## Verification
- `bash scripts/onboarding/macos-onboarding.sh --help`
- `bash scripts/onboarding/macos-onboarding.sh doctor || true`
- `npm test -- tests/onboarding/macos-onboarding-script-contract.test.ts`
- `shellcheck scripts/onboarding/*.sh`
