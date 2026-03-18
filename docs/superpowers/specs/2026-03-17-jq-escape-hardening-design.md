# 2026-03-17 — JQ Escape Hardening

## Goal

Keep relay filtering resilient when clients or stale guidance pass a jq regex with a bare `\.` escape.

## Problem

- Current CLI forwards `--jq` directly into gojq.
- A filter like `test("^light\.")` is invalid jq source and fails before execution.
- Some skill guidance still encourages helper-domain filters that are easy for models to regenerate with the wrong escape shape.

## Decision

1. Retry jq parsing once after normalizing bare `\.` into `\\.`.
2. Remove helper-domain regex examples from skill docs and replace them with split-domain matching.
3. Add regression coverage in Go tests and skill contracts.

## Non-Goals

- No general jq rewriter.
- No broad shell-quoting layer.
- No new adapter/plugin architecture work in this fix.
