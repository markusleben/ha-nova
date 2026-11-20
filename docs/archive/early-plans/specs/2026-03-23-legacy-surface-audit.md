# 2026-03-23 Legacy Surface Audit

## Goal

- Reduce tracked shim and legacy surface to the smallest set that still serves real product recovery or current contributor workflows.

## Decisions

- Keep `scripts/legacy-uninstall.sh` and `scripts/legacy-uninstall.ps1` because `install.sh` and `install.ps1` still need a dedicated pre-Go recovery path.
- Keep `scripts/onboarding/install-local-skills.sh`, `scripts/onboarding/bin/ha-nova`, and `scripts/dev-sync.sh` as the active repo-dev helper surface.
- Remove tracked repo files that only duplicate generated wrapper behavior for dev/compat installs.
- Do not let generated repo/dev wrappers such as `~/.config/ha-nova/version-check` count as pre-Go legacy markers in the installers.
- Prefer npm-level contributor entrypoints over teaching direct helper-script invocation in `CONTRIBUTING.md` when the behavior is the same.

## Non-Goals

- No change to the shipped end-user install/update contract.
- No attempt to migrate pre-Go installs in place.
