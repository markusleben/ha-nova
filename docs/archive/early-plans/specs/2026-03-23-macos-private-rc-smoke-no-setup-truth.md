# 2026-03-23 macOS Private RC Smoke No-Setup Truth

## Problem

The macOS smoke lane only validates install -> version -> uninstall without running `setup`.

That means no Home Assistant config should exist before or after uninstall.

## Decision

Keep `macos-private-rc-smoke.sh` aligned with its own narrower lane:
- runtime removed
- cache removed
- `config.json` absent
- `state.json` absent

Do not reuse the setup-all standard-remove expectations in the smoke lane.

## Why

The smoke helper proves installer/runtime behavior only. Config-retention assertions belong to setup-aware lanes, not to the no-setup smoke path.
