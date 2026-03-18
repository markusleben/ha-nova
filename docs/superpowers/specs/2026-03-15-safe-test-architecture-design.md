# Safe Test Architecture Design

## Goal

Define the smallest test architecture that keeps a maintainer host safe by default while still proving HA NOVA's installer and setup behavior on macOS and Windows before release.

## Problem

The current test surface mixes safe automation and host-affecting desktop flows:

- default `npm test` can still reach legacy macOS shell onboarding paths
- desktop-oriented flows can open browsers unexpectedly
- macOS secure-token flows can touch the real login keychain
- aborted runs can leave background child processes alive

This is too risky for normal local verification.

## Design Principles

- Safe by default
- Desktop validation explicit, never implicit
- One artifact build path
- One runner per validation lane
- No surprise browser or keychain access in default verification
- Windows desktop proof only in the VM

## Approaches Considered

### 1. Keep one big test suite and harden every path

Pros:
- fewer entrypoints

Cons:
- too easy to miss one host-affecting path
- default local verification stays risky
- high maintenance cost

### 2. Split safe verification from explicit desktop validation

Pros:
- smallest reliable boundary
- easy to explain
- matches the real risk split

Cons:
- adds a few explicit commands

### 3. Move all desktop proof to fully automated GUI infrastructure

Pros:
- more automation

Cons:
- unnecessary complexity
- fragile
- slow to maintain

## Recommended Design

Use approach 2.

Define four lanes:

1. `verify-safe`
- default local/CI gate
- no browser
- no real keychain
- no desktop shell onboarding

2. `macos-desktop`
- explicit maintainer-only lane
- manual or semi-manual
- exactly one real browser/keychain proof path

3. `windows-headless`
- VM over SSH only
- install/version/uninstall only

4. `windows-desktop`
- VM over RDP only
- setup/client/update proof

## Required Boundaries

### Default verification boundary

`npm run verify` must include only:

- TypeScript checks
- Go CLI tests
- safe Vitest suites
- contract tests
- non-browser/non-keychain runner checks

It must exclude:

- legacy `macos-setup.sh` execution
- real browser opens
- real keychain writes
- real desktop client/plugin actions

### Browser boundary

All product and legacy setup flows must support a hard disable switch for browser launch during automation.

Recommended contract:
- `HA_NOVA_NO_BROWSER=1`

Default test runners always set it.

### Secret-storage boundary

Automated macOS validation must not touch the maintainer's real HA NOVA keychain entry.

Allowed options:
- dedicated test keyring service override
- mocked storage path in legacy shell tests

Not allowed:
- implicit writes to the normal login-keychain service during default verification

### Legacy-shell boundary

Legacy shell onboarding remains useful only as a compatibility/dev surface.

It must not define the default product verification contract anymore.

## Minimal Runner Structure

Keep exactly these explicit entrypoints:

- `npm run verify`
- `npm run release:rc:local`
- `scripts/dev/macos-private-rc-smoke.sh`
- `scripts/dev/macos-private-rc-setup-all.sh`
- `scripts/dev/macos-private-rc-client.sh <client>`
- `scripts/dev/windows-clean-test-state.ps1`
- `scripts/dev/windows-private-rc-install.ps1`
- `scripts/dev/windows-desktop-setup.ps1 -Client <client> ...`

Optional helper:
- one kill/cleanup script for orphaned local test processes

Do not add more workflow layers or GUI tooling.

## Release Proof Model

Before release:

- safe verification must pass
- private RC artifacts must build once
- macOS explicit lane must pass
- Windows headless lane must pass
- Windows desktop lane must pass for every publicly supported Windows client

## Definition of Done

This design is complete when:

- `npm run verify` is host-safe
- desktop/browser/keychain tests are explicit only
- legacy macOS shell tests no longer run inside the default gate
- Windows desktop proof is VM-only
- docs describe only the reduced lane structure
