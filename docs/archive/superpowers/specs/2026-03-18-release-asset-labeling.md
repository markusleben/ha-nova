# Spec: Release Asset Labeling

Date: 2026-03-18

## Problem

GitHub release assets previously used names like:

- `ha-nova-macos-arm64.tar.gz`
- `ha-nova-windows-amd64.zip`

Those names looked end-user-ready and made it too easy to assume the archives should be downloaded and launched manually from the release page.

That is not the supported UX.
The supported UX is:

- fresh install via `install.sh` / `install.ps1`
- existing installs via `ha-nova update`

## Decision

Rename installer payload assets to:

- `ha-nova-installer-bundle-macos-amd64.tar.gz`
- `ha-nova-installer-bundle-macos-arm64.tar.gz`
- `ha-nova-installer-bundle-linux-amd64.tar.gz`
- `ha-nova-installer-bundle-linux-arm64.tar.gz`
- `ha-nova-installer-bundle-windows-amd64.zip`

Keep raw GoReleaser binaries unchanged.

## Rationale

- The filename itself now teaches that the archive is an installer payload.
- The release page warning and the filename reinforce the same message.
- Installers and `ha-nova update` continue to consume the assets automatically.
- Maintainer/debug use stays possible without changing the runtime model.

## Required Consistency

When asset names change, update all of these together:

- `install.sh`
- `install.ps1`
- `cli/release.go`
- `scripts/release/build-install-bundle.sh`
- RC / release workflows
- private RC / validation helper scripts
- release contracts
- `README.md`
- `docs/releasing.md`
