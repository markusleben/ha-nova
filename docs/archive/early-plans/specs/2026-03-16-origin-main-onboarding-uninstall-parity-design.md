# 2026-03-16 Origin Main Onboarding + Uninstall Parity

## Problem

Two `origin/main` user-facing behaviors are still stronger than the current Go flow:

1. Setup actively guided the user through the Home Assistant Access Token (`ha_llat`) path.
2. Uninstall clearly announced what would be removed and warned when NOVA Relay was still running in Home Assistant.

## Goal

Bring those two behaviors back in a cross-platform Go flow without regressing the newer wizard/navigation work.

## Design

- Add a small LLAT guidance choice after the relay-token step:
  - continue directly when the user already configured the relay
  - or walk them through opening HA profile + Relay settings when they need it
- Add a small uninstall preflight renderer plus a relay-running probe before deletion:
  - show the removal scope before confirm
  - after uninstall, note when the Relay app is still running in Home Assistant

## Non-Goals

- No new setup stage number
- No purge mode
- No new Relay API
