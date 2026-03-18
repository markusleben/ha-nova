# WS Readiness Parity Design

## Problem

The Go-based setup flow can still mislead users during Step 3.

- `GET /health` reports `ha_ws_connected=false` until the relay has actually opened an upstream Home Assistant WebSocket.
- The relay runtime currently uses a lazy WebSocket client, so `ha_ws_connected=false` does not automatically mean that the saved `ha_llat` is wrong.
- The old release handled this correctly by:
  - checking `/health`
  - then probing `/ws` with a ping
  - then showing LLAT-specific guidance only when the `/ws` response proved it

This parity was partially lost during the Go migration.

## Root Cause

There are two separate concerns:

1. **Relay runtime semantics**
   - `ha_ws_connected` is a passive snapshot of the current upstream WS client state.
   - With lazy connection startup, it can be `false` even when the configured `ha_llat` is valid.

2. **CLI readiness interpretation**
   - Setup and doctor need an *effective readiness* decision, not only a passive snapshot.
   - The old release solved that by treating `/ws` ping as the confirmation path when `/health` was still false.

There is no direct proof in the repo of a silent `ha_llat` persistence bug. Current startup logs only prove that a non-empty LLAT reached the runtime bootstrap; they do not prove the full saved-options path is perfect in every case. The decisive user-facing bug here is still the CLI readiness interpretation.

## Decision

- Keep relay startup lazy.
- Keep `/health` as a passive snapshot endpoint.
- Restore old-release readiness semantics in the CLI:
  - `/health` first
  - `/ws` ping fallback when `ha_ws_connected=false`
  - LLAT-specific wording only when `/ws` proves `LLAT is required`
- Use one shared readiness helper for setup, doctor, and resume-state detection so the behavior cannot drift again.
- Treat the pair `health=false` + `ws ping ok` as the decisive proof for the lazy-WS false-negative case.
- Keep the Windows inline `relay ws -d ...` issue as a separate CLI bug track. It must not block readiness parity, but it must be fixed before we can call Windows diagnostics clean.

## Scope

### In scope

- Shared CLI helper for relay readiness diagnostics
- Setup Step 3 parity
- `ha-nova doctor` parity
- resume / already-set-up parity via `detectSetupState`
- post-onboarding skill-call confidence via the same readiness semantics
- Regression tests for:
  - health false + ws ping ok
  - health false + ws ping LLAT error
  - health false + generic ws failure
- regression coverage for resume-state using the same readiness helper
- separate regression coverage for Windows inline relay JSON payloads

### Out of scope

- Replacing lazy WS startup with eager connection
- Adding new relay endpoints
- Changing App option persistence
- Expanding the setup flow beyond readiness parity
- Adding broad network scanning or other onboarding UX changes unrelated to readiness truth

## Desired Behavior

### Setup

- If `/health` says `ha_ws_connected=true`: success
- If `/health` says `ha_ws_connected=false`:
  - try `/ws` ping
  - if ping succeeds: treat the relay as connected
  - if ping fails with `LLAT is required`: say exactly that
  - otherwise show generic degraded guidance

### Doctor

- Same readiness logic as setup
- Same LLAT-specific/generic distinction
- No unconditional LLAT blame

### Resume / Already Set Up

- `detectSetupState` must not mark the system degraded only because passive `/health` still says `ha_ws_connected=false`
- If `/health` is false but `/ws` ping succeeds, resume state is still considered WS-ready
- Skip summaries and "already set up" banners must therefore use the same truth model as setup and doctor

### Later Skill Calls

- Later skill calls depend on the same local relay readiness, config, token storage, and client-installed skills
- We do not need separate readiness semantics inside each skill
- We do need one post-onboarding smoke check per supported platform/client lane to prove that a real skill call works after setup:
  - macOS: one real skill call after normal onboarding
  - Windows: one real skill call after normal onboarding on the supported client lane(s)

## Testing

- Go unit tests for the shared readiness helper
- Go setup tests for:
  - ws ping success fallback
  - LLAT-specific degraded guidance
- Doctor tests aligned to the same readiness rules
- Resume-state tests aligned to the same readiness rules
- Windows relay CLI tests for inline `-d/--data` JSON handling
- Full `go test ./...`
- Full `npm run verify`
