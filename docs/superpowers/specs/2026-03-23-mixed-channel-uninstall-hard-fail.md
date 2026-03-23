# Mixed-Channel Uninstall Hard Fail

Date: 2026-03-23

## Problem

Windows mixed-channel machines already fail loud for `ha-nova update`, but `ha-nova uninstall` still warned and then mutated whichever channel happened to be current.
That could remove shared local state while leaving the other install channel behind.

## Decision

Treat mixed bundle + `winget` uninstall the same way as mixed update.
Fail loud before mutating any runtime or local state.

## Scope

- `cli/command_uninstall.go`
- `cli/uninstall_test.go`

## Why

Mixed-channel states are ambiguous by definition.
The safe rule is to stop and force the operator to remove one channel first.
