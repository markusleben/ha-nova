# 2026-03-25 - winget runner schema compatibility

## Context

The GitHub `windows-latest` post-publish smoke job still runs `winget validate` in an environment that does not accept our generated `1.12.0` schema headers warning-free.

Observed behavior:

- `1.12.0` + `yaml-language-server` header produced `Schema Header URL does not match expected pattern`
- removing the header produced `Schema header not found`

## Decision

Pin generated HA NOVA `winget` manifests to schema `1.9.0` for now and emit the matching `yaml-language-server` schema header again.

Reason:

- HA NOVA only uses fields supported by manifest schema `1.9.0`
- official `winget-pkgs` docs and package examples still use `1.9.0` for the same installer constructs
- this is the smallest release-safe change that keeps the GitHub release smoke warning-free

## Follow-up

Revisit the default manifest schema only after the GitHub `windows-latest` release smoke lane proves a newer schema version warning-free.
