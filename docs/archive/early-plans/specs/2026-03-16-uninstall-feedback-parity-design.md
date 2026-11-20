# 2026-03-16 Uninstall Feedback Parity

## Problem

The Go uninstall path is too quiet.
Users no longer see which files, client integrations, or credential decisions were applied.

## Goal

Restore clear uninstall feedback without reintroducing shell sprawl:
- list concrete removals
- say when nothing was removed
- say when macOS intentionally keeps the relay token
- keep the final success line

## Design

- Add a tiny uninstall report collector in the CLI runtime.
- Feed it from client removal, managed PATH cleanup, file removal, and token policy.
- Render line-by-line `[ha-nova] Removed: ...` output plus a final summary.

## Non-Goals

- No interactive uninstall wizard
- No purge flag in this change
- No reinstall behavior changes
