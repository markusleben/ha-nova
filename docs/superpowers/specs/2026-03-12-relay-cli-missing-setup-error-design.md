# Relay CLI Missing-Setup Error

> Historical note: this spec discusses the legacy `~/.config/ha-nova/relay` entrypoint. The current public-facing command is `ha-nova relay ...`; the legacy path remains only as a migration shim.

Date: 2026-03-12

## Goal

Make `~/.config/ha-nova/relay ...` fail with a user-oriented setup hint when HA NOVA is installed but not onboarded yet.

## Problem

- After `install-local-skills.sh`, the relay CLI exists.
- Before `ha-nova setup`, `~/.config/ha-nova/onboarding.env` is intentionally missing.
- Current error is technically correct but too raw:
  - `error: missing ~/.config/ha-nova/onboarding.env`

## Decision

- Keep the exit code non-zero.
- Change the missing-config error to explain the state and next step:
  - `error: HA NOVA is not set up yet. Run: ha-nova setup`
- Do not change the missing-Keychain-token path in this step.

## Scope

- `scripts/relay.sh`
- focused regression test for the relay CLI missing-setup path
