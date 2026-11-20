# Windows Desktop Cleanup Env Reset

Date: 2026-03-23

## Problem

The Windows desktop cleanup helper already removed HA NOVA runtime paths, client artifacts, and Credential Manager token entries.
That still left one stale-state path: persistent per-user `HA_NOVA_*` environment overrides in `HKCU\Environment`.

Those overrides can leak bundle URLs, keyring service names, or local Claude marketplace flags into the next desktop validation session and make the VM look partially preconfigured.
The same reset path also needs to clear Claude's own plugin and marketplace registry JSON, including the case where those files are corrupt and no longer parse cleanly.

## Decision

Extend the Windows cleanup helper to remove the known HA NOVA per-user environment overrides from `HKCU\Environment`.
Also clear Claude plugin and marketplace registry records, deleting the registry files outright when they are empty or corrupt.

## Scope

- `scripts/dev/windows-clean-test-state.ps1`
- `tests/onboarding/desktop-validation-contract.test.ts`

## Why

Desktop validation should start from a truly clean user context, not only a clean filesystem and credential store.
