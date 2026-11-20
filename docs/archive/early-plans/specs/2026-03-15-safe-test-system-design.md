# Safe Test System Design

> Historical design note: this design already landed as the current safe-default test contract. Remaining shell wrappers are dev/compat-only and are not first-class product test surfaces.

## Goal

Define the smallest test architecture that keeps everyday local and CI runs safe on maintainers' machines while still proving macOS and Windows release-critical behavior before publish.

## Scope

- Local default test commands
- CI default test commands
- Explicit macOS desktop validation
- Explicit Windows VM validation
- Cleanup and abort safety

## Non-Goals

- No GUI automation framework
- No always-on desktop test lane in CI
- No new generalized test orchestration layer
- No attempt to keep legacy shell onboarding as a first-class product test surface

## Problem Statement

The current repo still allows browser-opening and secure-store-touching paths to run from normal test entrypoints. That is too dangerous on a maintainer workstation. A safe test system must make side-effect-heavy validation opt-in, never default.

## Approaches Considered

### 1. Keep current structure and add more cleanup

Pros:
- Smallest code change

Cons:
- Still unsafe by default
- Easy to regress
- Cleanup after accidental side effects is not enough

### 2. Full test framework split with many new abstractions

Pros:
- Strong separation

Cons:
- Too much ceremony
- Adds maintenance overhead
- Violates KISS for the current repo size

### 3. Safe-by-default split with explicit desktop lanes

Pros:
- Small, clear, enforceable
- Keeps `verify` useful
- Desktop proof still possible when needed

Cons:
- Requires a few explicit new commands and guards

## Recommended Design

Use a two-tier test architecture:

- Tier 1: safe default
  - no browser launch
  - no real keychain/credential manager writes
  - no old shell onboarding desktop flows
  - allowed in `npm test`, `npm run verify`, and CI
- Tier 2: explicit desktop validation
  - run only on purpose
  - macOS only by a dedicated manual command
  - Windows only inside the VM, with headless and desktop lanes separated

## Core Rules

### Rule 1: Default test entrypoints must be host-safe

`npm test`, `npm run verify`, and normal GitHub CI must not:

- open a browser
- touch the real macOS login keychain
- touch the real Windows credential manager
- run old shell onboarding flows that can do either of the above

### Rule 2: Desktop validation is explicit

Release-critical desktop proof stays, but only behind dedicated commands:

- `test:desktop:macos`
- `test:desktop:windows:headless`
- `test:desktop:windows:rdp`

These are never transitively called by `npm test` or `npm run verify`.

### Rule 3: One guard mechanism, reused everywhere

Do not invent multiple overlapping safety systems. Use one small set of explicit guards:

- `HA_NOVA_NO_BROWSER=1`
- `HA_NOVA_TEST_KEYRING=1` or equivalent explicit test secret-store override
- dedicated desktop runner scripts for the real interactive paths

The same guards should be honored by both the Go runtime and any still-needed shell helpers.

### Rule 4: Old shell onboarding stops defining default quality

The old `scripts/onboarding/macos-setup.sh` style flows may stay temporarily for legacy/dev support, but they must no longer be part of the default test contract. If tested at all, they belong in explicit legacy/manual lanes only.

### Rule 5: Cleanup is first-class but small

Each explicit desktop lane needs one cleanup helper and one documented kill-switch path. No generalized daemon manager, no persistent watchers.

## Test Lanes

### Safe Default Lane

Runs on maintainers' hosts and in CI:

- TypeScript
- safe Vitest contracts
- Go CLI tests
- bundle build contracts
- installer/update logic that does not open browsers or hit real secure stores

### macOS Desktop Lane

Manual only:

- private RC bundles only
- browser enabled only when deliberately desired
- keychain writes only via explicit test isolation
- one short runbook, not part of default verify

### Windows Headless Lane

VM only:

- install
- version
- uninstall

No `setup` proof here.

### Windows Desktop Lane

VM + RDP only:

- `setup`
- `doctor`
- client artifact checks
- same-version update
- uninstall

## Definition of Done

This design is complete when:

- `npm test` and `npm run verify` are host-safe by construction
- no default test path can open a browser
- no default test path can touch real secure stores
- macOS desktop validation is a separate documented command
- Windows validation is split into headless and desktop commands
- release confidence depends on explicit desktop lanes, not accidental default runs
