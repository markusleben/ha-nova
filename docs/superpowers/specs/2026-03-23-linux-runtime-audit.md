# 2026-03-23 Linux Runtime Audit

## Scope

Audit Linux onboarding/update/uninstall confidence without a live Linux machine.

Checked surfaces:
- `README.md`
- `PROJECT.md`
- `docs/releasing.md`
- `install.sh`
- `cli/paths.go`
- `cli/keyring_linux.go`
- `cli/command_update.go`
- `cli/command_doctor.go`
- `cli/command_uninstall.go`
- `cli/uninstall_test.go`
- onboarding/release contract tests

## Findings

- Active docs already describe Linux conservatively:
  - shared installer/runtime path with macOS
  - CI-smoked
  - not yet fully live-validated on a real Secret Service-backed machine
- Linux runtime code stays on the same Go-owned lifecycle path as macOS for:
  - `install.sh`
  - `ha-nova check-update`
  - `ha-nova update`
  - `ha-nova uninstall`
- Linux secure-token storage remains explicit and fail-loud through `go-keyring`, with one deliberate uninstall exception for headless Secret Service unavailability during token cleanup.
- The audit exposed a second Linux-only edge: Secret Service could disappear between token read and token delete during purge, leaving a half-uninstalled state even after runtime removal.
- Active onboarding contracts previously had explicit platform keyring checks for macOS and Windows, but not Linux.

## Decisions

- Do not over-claim Linux in public docs or release notes until a real Secret Service-backed Linux machine completes the fresh install/update/uninstall smoke.
- Add contract coverage so conservative Linux wording in `README.md`, `PROJECT.md`, and `docs/releasing.md` cannot silently drift.
- Add Linux keyring contract coverage and keep the delete-side headless Secret Service path non-fatal only for the same explicit unavailable-backend error class.

## Result

Linux currently counts as:
- code-audited
- contract-tested
- CI-smoked

Linux does not yet count as:
- fully live-validated for release proof
