# Windows Uninstall Detached Helper

Date: 2026-03-23

## Problem

Windows uninstall previously launched a helper process that stayed attached to the current console so it could print the final removal lines.
That made the UX feel fragile: if the user closed PowerShell immediately, uninstall reliability depended on console lifetime rather than only on the helper process.

## Decision

Launch the Windows uninstall helper detached from the terminal session.
After spawn, print a short confirmation that background uninstall continues and it is safe to close the terminal.

## Scope

- `cli/windows_process_launch.go`
- `cli/command_uninstall.go`
- helper launch tests

## Why

For uninstall, reliability matters more than live console output from the helper.
The terminal should not be part of the removal contract.
