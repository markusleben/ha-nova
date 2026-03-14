# RC Prerelease Publish Design

## Summary

The existing `release-candidate.yml` workflow should stay the single RC workflow.
It will gain an optional manual publish mode that creates a GitHub prerelease for `vX.Y.Z-rcN` and uploads the install bundles plus checksum sidecars.

## Goals

- Keep the current RC rehearsal flow unchanged by default.
- Add the smallest public-asset path needed to test `install.sh` / `install.ps1` against real GitHub release assets.
- Avoid creating a second RC workflow.
- Avoid touching the final `release.yml` publish contract.

## Non-Goals

- No final-release changes.
- No signing/notarization changes.
- No raw-binary RC publishing requirement unless the installer/update path needs it.

## Design

### Workflow Inputs

Add two manual inputs to `release-candidate.yml`:

- `publish_release` (`boolean`, default `false`)
- `version_tag` (`string`, default empty)

`publish_release=false` preserves current behavior.

`publish_release=true` requires a prerelease-style tag such as `v0.1.13-rc1`.

### Build and Smoke

Keep the existing RC build and bundle smoke jobs unchanged in spirit:

- `npm run verify`
- `goreleaser build --snapshot --clean`
- `build-install-bundle.sh`
- bundle smoke on macOS/Linux/Windows

The optional publish job must run only after bundle smoke succeeds.

### Publish Behavior

When `publish_release=true`:

- create or update a GitHub prerelease for `version_tag`
- upload `dist/install-bundles/*`
- mark it as prerelease
- do not use `goreleaser release`
- do not affect the final `vX.Y.Z` release path

This is enough for public installer testing because the installers consume the bundle assets and checksum sidecars directly.

### Docs

Update `docs/releasing.md` to explain:

- normal RC rehearsal
- optional public RC prerelease publish
- how to test `install.sh` / `install.ps1` against `HA_NOVA_VERSION=vX.Y.Z-rcN`

## Risks

- Maintainers may accidentally publish a non-rc tag from the RC workflow.
  - Mitigation: validate `version_tag` and fail unless it matches an `-rc` tag pattern.

- Maintainers may assume RC prerelease equals final release.
  - Mitigation: docs must clearly state RC prerelease exists only for public installer testing.
