# 2026-03-22: WinGet Release Artifact and Legacy Shell Removal

## Goal

Ship a real Windows release handoff for `winget` without reviving a second product-owned installer stack, then remove the obsolete shell onboarding path that no longer defines user UX.

## Decisions

- Keep Windows public install docs on `install.ps1` until the community `winget` package is actually live.
- Generate the `winget` manifest archive from the shipped Windows installer bundle, not from the PowerShell bootstrap script.
- Upload that manifest archive with RC/final releases so maintainers can submit the exact shipped payload to `winget-pkgs`.
- Remove the old shell onboarding/doctor/uninstall family now that Go owns the product lifecycle.
- Keep only the repo/dev helpers that still serve an active purpose:
  - `scripts/onboarding/install-local-skills.sh`
  - `scripts/onboarding/bin/ha-nova`
  - `scripts/legacy-uninstall.sh`
  - `scripts/legacy-uninstall.ps1`

## Deliverables

- `scripts/release/build-winget-manifest.sh`
- release and RC workflows upload the manifest archive and checksum
- release-contract coverage for the manifest archive handoff
- obsolete shell onboarding files and shell-only tests removed
- docs/contracts updated to match the new lifecycle truth
