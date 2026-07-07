# 2026-04-13 Review Lane 3 Update Flow Spec

## Goal

Inspect installed-client update/setup repair behavior with focus on Claude marketplace sync and persisted install state.

## Questions

- Which files own installed-client sync during `ha-nova update` and `ha-nova setup`?
- Does HA NOVA automatically repair a stale Claude detach state?
- Can Claude remain marked installed in HA NOVA state after the Claude plugin or marketplace is removed externally?
- If so, is that intentional retry state or a real bug for end-user update/setup behavior?

## Scope

- `cli/command_update.go`
- `cli/command_setup.go`
- `cli/setup_interactive.go`
- `cli/state.go`
- `cli/client_status.go`
- `cli/client_detection.go`
- `cli/client_install.go`
- `cli/client_claude.go`
- `cli/claude_marketplace.go`
- relevant tests/docs for Claude stale-state and marketplace sync

## Deliverable

Review findings with likely root cause, current repair behavior, and fix options with file references.
