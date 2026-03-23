# 2026-03-23 macOS Private RC Mock-Version Unbound Fix

## Problem

The private macOS RC helpers ran with `set -u` but still read `MOCK_REPORTED_VERSION` without a default expansion.

Effect:
- `scripts/dev/macos-private-rc-setup-all.sh` could abort before the real setup lane started
- `scripts/dev/macos-private-rc-client.sh` had the same latent failure mode
- the private RC suite could fail even though the built bundle and smoke lane were healthy

## Decision

Use `${MOCK_REPORTED_VERSION:-}` in both helpers before deriving the version from the local bundle.

Add a desktop-validation contract assertion so future edits keep the safe default expansion.

## Why

The private RC helpers should derive their reported version automatically unless an operator intentionally overrides the bundle source. `set -u` must not turn that default path into a false negative.
