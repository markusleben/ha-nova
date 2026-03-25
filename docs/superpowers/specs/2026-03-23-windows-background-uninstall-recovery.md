# 2026-03-23 — Windows Background Uninstall Recovery

## Summary

Windows background uninstall now persists a small recovery marker at `%LOCALAPPDATA%\ha-nova\uninstall-status.json`.

The detached helper writes:
- `running` before mutable cleanup starts
- `failed` with step + error summary if cleanup stops early
- transient `success` just before the marker is removed

## Why

Detached uninstall is the right UX for Windows because users may close PowerShell immediately after `ha-nova uninstall --yes`.

Without a persistent recovery marker, late helper failures become invisible and the next install/update can layer over a half-removed machine state.

## Active Contract

- Bundle and `winget` Windows uninstalls now keep the runtime callable until the final runtime-removal step.
- `ha-nova doctor` blocks on failed or interrupted background uninstall and prints the exact recovery command.
- `ha-nova update` blocks on the same recovery state instead of mutating a dirty install.
- `install.ps1` refuses to layer a fresh install over an active/failed background uninstall.
- Recovery stays mode-aware:
  - standard: `ha-nova uninstall --yes`
  - purge: `ha-nova uninstall --yes --purge`

## Notes

- This is intentionally Windows-only in phase 1.
- No popup/toast was added yet; durable status + `doctor` recovery is the primary UX surface.
