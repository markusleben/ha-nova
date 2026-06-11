# 2026-04-13 Claude Availability Troubleshooting Spec

- Goal: find why HA NOVA is not available in Claude on this macOS machine.
- Method: inspect local HA NOVA install, Claude integration files, plugin/skill paths, and CLI availability.
- Scope: diagnosis first; no changes unless a clear fix is identified.

## Findings

- The local HA NOVA runtime is still `0.3.2`.
- HA NOVA's own state says Claude is installed in `plugin` mode.
- Claude itself does not currently list the `ha-nova` marketplace or the `ha-nova@ha-nova` plugin.
- This means the old HA NOVA install cannot surface its own update notice inside Claude, because Claude is not loading it at all.
- The current issue is not only "old version installed" but also local Claude plugin-state drift between HA NOVA state and Claude state.

## Root Cause Summary

- HA NOVA treated Claude plugin install success too loosely.
- The Go installer verified marketplace registration before plugin install, but not again after plugin install.
- Claude plugin record detection also accepted blank `installPath` values as "installed".
- That combination made it easier for stale or partially broken Claude state to look valid enough for persistence.

## Fix Applied

- Claude install success now requires both:
  - the `ha-nova` marketplace still present after sync
  - the `ha-nova@ha-nova` plugin still present after sync
- Blank Claude `installPath` values no longer count as installed.
- Added regressions for:
  - marketplace disappearing after plugin install
  - blank `installPath`

## Refinement Pass

- Six focused review lanes converged on a KISS-safe direction:
  - keep `update` repair-oriented; do not silently prune Claude from HA NOVA state
  - avoid a new Claude state model or background telemetry system
  - tighten the existing install truth instead of adding a new subsystem
- Additional hardening applied:
  - fallback `update -> install` now uses the same rollback-safe verifier path
  - unparseable Claude plugin registry no longer counts as installed
  - added regression for fallback install when marketplace disappears
