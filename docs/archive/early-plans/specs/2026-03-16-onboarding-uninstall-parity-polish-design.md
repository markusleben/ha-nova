# 2026-03-16 Onboarding + Uninstall Parity Polish

## Problem

Two user-facing parity gaps remain versus `origin/main`:

1. The Go setup wizard explains the relay token well enough, but it does not actively walk the user through creating and pasting the Home Assistant Long-Lived Access Token before verification.
2. The Go uninstall reports removals afterwards, but it still lacks the clear preflight summary and the "relay is still running in Home Assistant" note that made the old shell flow easier to trust.

## Goal

Close those two UX gaps without adding a larger architectural layer.

## Constraints

- KISS
- DRY
- no new generalized workflow engine
- preserve current Go runtime structure
- work on macOS and Windows

## Decision

Add two small targeted behaviors:

1. **Active LLAT walkthrough in Step 2**
   - after relay-token handling, the wizard explicitly explains the HA access token flow
   - opens the HA security page
   - opens the NOVA Relay settings page
   - tells the user exactly where `ha_llat` goes
   - then continues to verification

2. **Uninstall preflight + relay-running note**
   - before confirmation, print a short "This will remove:" summary
   - after removal, if the relay still answered `/health` before deletion, print a short note that the Home Assistant App is still installed/running

## Non-Goals

- no new setup phase number
- no new relay health endpoint
- no cross-platform daemon/service manager
- no client-registry work in this change
