# 2026-03-22 Onboarding Lifecycle Implementation

## Goal

- Make HA NOVA onboarding and lifecycle handling single-owner, UX-first, and easier to maintain.

## Decisions

- Future-state Windows primary distribution, after public `winget` publication + proof, is `winget`.
- Current public Windows entrypoint until that publication/proof remains `install.ps1`.
- `install.ps1` stays the fallback/recovery distribution after public `winget` launch too.
- Bootstrap installers own runtime placement and PATH only.
- Go CLI owns setup, repair, migration, update delegation, uninstall delegation, and local cleanup.
- Windows config moves to `%APPDATA%\ha-nova`.
- Windows cache moves to `%LOCALAPPDATA%\ha-nova\cache`.
- Windows bundle fallback runtime moves to `%LOCALAPPDATA%\Programs\ha-nova`.
- Runtime state uses `install_source` with `bundle`, `dev`, and `winget`.
- `ha-nova update` delegates to `winget upgrade` when the install source is `winget`.
- `ha-nova check-update` follows the active install source instead of always speaking GitHub-release truth.
- `ha-nova uninstall` delegates to `winget uninstall` when the install source is `winget`.
- `ha-nova uninstall --purge` removes local config/cache/token artifacts after channel uninstall.
- Mixed Windows bundle + `winget` installs are treated as a conflict: warn in status surfaces, fail loud for update, and never guess which channel to mutate.
- Windows legacy `.dpapi` token files become migration-only. No new mirror writes.
- Shell onboarding artifacts remain cleanup targets but are no longer part of the primary product contract.
- Each release builds a `winget` manifest handoff artifact from the published Windows bundle asset.
- The old macOS shell onboarding family is removed instead of being kept in parallel as a second lifecycle contract.

## Non-Goals In This Pass

- No MSIX packaging.
- No automatic Home Assistant app removal during local uninstall.
- No installer signing implementation yet.
- No Windows ARM64 bundle in this pass.
