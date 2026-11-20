# Setup Page-Flow Parity Design

## Problem

The Go wizard currently re-renders new headers under old content. The legacy release cleared the terminal between major setup pages, so the wizard felt like discrete pages instead of a growing log.

## Goal

- Restore page-like wizard behavior for interactive terminal users.
- Preserve non-interactive/test output as plain accumulated logs.

## Decision

- Clear the terminal only when rendering setup headers on a real TTY.
- Keep tests and non-TTY runs unchanged.

## Scope

- `cli/setup_ui.go`
- focused UI test only
