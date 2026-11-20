# Windows Uninstall And Step-4 Feedback Design

## Problem

Two UX gaps remain:

1. Windows uninstall only prints `uninstall started`, not a clear final success/failure signal.
2. Setup Step 4 (`Installing HA NOVA skills`) has no visible progress feedback, although the old release used a spinner for long-running work.

## Goal

- Windows uninstall must surface a clear final outcome when possible.
- Claude plugin removal during uninstall must behave deterministically:
  - skip quietly when already absent
  - fail loud when removal of a present plugin actually fails
- Step 4 must show the same class of progress feedback as discovery and host checks.

## Decision

- Keep the async Windows helper model.
- Add a final success line from the helper: `HA NOVA removed`.
- Tighten Claude uninstall logic around actual installed state.
- Add a spinner/non-TTY progress line for Step 4 skill installation.

## Scope

- `cli/clients.go`
- `cli/commands.go`
- `cli/setup_progress.go`
- `cli/setup_interactive.go`
- focused tests only
