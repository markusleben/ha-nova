# 2026-03-16 macOS Uninstall No Keychain Prompt

## Problem

The Go uninstall currently calls `security delete-generic-password` on macOS.
That can trigger an interactive Keychain permission/password dialog during `ha-nova uninstall --yes`.

## Legacy Baseline

`origin/main` shell uninstall deletes the macOS Keychain token.
So parity means delete-on-uninstall, not token retention.

## Goal

Restore old-release behavior on macOS while removing guesswork:
- uninstall removes HA NOVA files and client integrations
- uninstall deletes the relay token like `origin/main`
- uninstall explains what was removed clearly

## Design

- Keep token deletion on Linux, Windows, and macOS aligned.
- Improve uninstall reporting so users can see the token deletion happened.
- Investigate prompt sources separately from delete semantics.

## Non-Goals

- No new purge flag in this change
- No change to setup/doctor token reads
- No change to Windows/Linux uninstall semantics
