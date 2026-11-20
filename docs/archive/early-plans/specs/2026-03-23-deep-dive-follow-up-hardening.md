# 2026-03-23 Deep-Dive Follow-Up Hardening

## Goal

Close the release-bound gaps found during the post-`origin/main` deep dive:

- keep failed/interrupted Windows background-uninstall state visible until real recovery succeeds
- keep the visible Windows install root intact on late runtime-removal failures
- export `HA_NOVA_DEV_ROOT` from repo-dev wrappers
- bring final `release.yml` Windows validation up to RC quality

## Decisions

- Windows failed/interrupted uninstall markers are no longer cleared merely because the runtime path disappeared.
- `install.ps1` may clear a stale uninstall marker only when no runtime remains, no recorded on-disk residue remains, and the Windows user PATH no longer references the install root.
- Windows bundle runtime cleanup now removes the install root directly; it no longer renames the visible path away before deletion.
- Repo-dev wrapper execution exports `HA_NOVA_DEV_ROOT` so the compiled CLI stays source-aware in contributor flows.
- Final tagged release smoke now:
  - waits for detached Windows uninstall completion
  - fails if `uninstall-status.json` remains
  - re-validates the Windows `winget` manifest on Windows and fails on warnings

## Verification

- `go test` for Windows uninstall marker/runtime tests
- `vitest` for release contracts and wrapper contracts
- full `npm run verify`
