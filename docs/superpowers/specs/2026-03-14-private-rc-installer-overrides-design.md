# Private RC Installer Overrides Design

## Goal

Allow maintainers to test the real `install.sh` and `install.ps1` flows against non-public bundle artifacts, without merging to `main` and without publishing a public prerelease.

## Scope

- Add maintainer-only bundle URL overrides to the Unix and Windows installers.
- Keep the normal user path unchanged: GitHub Releases latest or `HA_NOVA_VERSION`.
- Validate downloaded bundle metadata, including bundle version.
- Document the private RC path in release docs.

## Design

### Override inputs

- `HA_NOVA_BUNDLE_URL`
- `HA_NOVA_BUNDLE_SHA256_URL`

If `HA_NOVA_BUNDLE_URL` is set:
- the installer downloads that bundle directly
- checksum URL comes from `HA_NOVA_BUNDLE_SHA256_URL` or defaults to `${HA_NOVA_BUNDLE_URL}.sha256`
- the installer does not query GitHub Releases latest unless `HA_NOVA_VERSION` is also set for an explicit expected-version check

### Version source

`bundle.json` is the source of truth for the installed version during override-based installs.

Rules:
- fail if `bundle.json.version` is missing
- if `HA_NOVA_VERSION` is set, fail unless it matches `bundle.json.version`
- for normal public installs, fail unless downloaded bundle version matches the selected release version

### Non-goals

- no new public RC release flow
- no new end-user environment variables in docs
- no fallback to unsigned/unverified bundle downloads
