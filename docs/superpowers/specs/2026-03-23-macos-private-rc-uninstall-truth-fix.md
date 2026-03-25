# 2026-03-23 macOS Private RC Uninstall Truth Fix

## Problem

The macOS private-RC helpers still asserted that `ha-nova uninstall --yes` removes the whole config directory.

Effect:
- private RC lanes could fail after a healthy uninstall
- helper expectations drifted away from the current product contract

## Decision

Keep the macOS private-RC standard-remove lanes aligned with the shipped uninstall semantics:
- runtime removed
- cache removed
- `state.json` removed
- `config.json` retained after `uninstall --yes`

Add desktop-validation contract assertions for those expectations.

## Why

Release helpers must test the real user contract, not a superseded cleanup model. Full config removal belongs to separate `--purge` validation, not to standard-remove lanes.
