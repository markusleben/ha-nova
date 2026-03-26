# Windows Uninstall Console Cleanup Design

Date: 2026-03-17

## Problem
- Windows uninstall prints the final `HA NOVA removed` line, but the shell prompt may not return cleanly.
- The likely culprit is the temp self-delete child staying attached to the same console/job after the helper finishes.

## Goal
- Keep the visible uninstall helper attached long enough to print the final result.
- Launch the temp helper cleanup in a detached, hidden background process so the shell can return immediately after the helper exits.

## Minimal Fix
- Keep the existing helper executable model.
- Give the visible helper a new process group.
- Move the temp self-delete command to a detached, hidden, no-handle-inheritance launch profile.
- Do not change user-facing uninstall semantics.

## Tests
- Lock the launch profiles in unit tests.
- Lock helper vs cleanup command output-stream behavior.
