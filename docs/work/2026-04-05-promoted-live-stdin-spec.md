# Promoted Live Harness stdin Fix

Date: 2026-04-05

## Problem

The promoted live harness can leave `codex exec` attached to inherited stdin.
That can produce `Reading additional input from stdin...` noise and delay or hang scenario completion even after the real HA proof work is done.

## Decision

Set `stdin=subprocess.DEVNULL` for the `codex exec` subprocess in the promoted live harness.

## Scope

- `scripts/e2e/codex-ha-nova-promoted-live-e2e.py`
- one contract assertion that locks this in

## Why

- small
- deterministic
- no product-surface change
- matches the harness intent: prompt-only execution, no extra stdin stream
