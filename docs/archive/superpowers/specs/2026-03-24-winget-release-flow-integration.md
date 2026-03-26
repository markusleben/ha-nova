# Spec: Winget Release Flow Integration

Date: 2026-03-24

## Summary

Keep `winget` integrated into the release system as a strict hybrid flow:
- GitHub RC/final workflows always build and validate the `winget` handoff artifact
- the first public `winget-pkgs` submission stages only from the exact final tagged release assets
- public Windows docs stay on `install.ps1` until the package is merged, publicly visible, and proven on a fresh Windows VM

## Rules

1. `winget validate` in RC/final workflows proves only handoff artifact quality, not a live public package.
2. The first public `winget-pkgs` submission must be staged from the exact final tagged release assets, not from local `dist/` or RC artifacts.
3. Public Windows doc flip authority belongs only to a successful fresh-VM published-source proof:
   - `winget install`
   - `ha-nova check-update`
   - `winget uninstall`
4. If the package is public but the fresh-VM proof fails, do not promote `winget`; keep `install.ps1` as the documented primary path.
5. Future automation stays disabled until:
   - public install proof is true
   - later public upgrade continuity proof is true
   - `release/winget-publication-state.json` explicitly enables automation
6. Keep at most one outstanding public `winget-pkgs` submission at a time.

## Implementation Notes

- `scripts/release/prepare-winget-pkgs-submission.sh` defaults to `WINGET_STAGE_SOURCE=release_asset`
- `WINGET_STAGE_SOURCE=local_dist` remains available only for rehearsal/local validation
- `release/winget-publication-state.json` is the repo-owned automation latch and proof record
