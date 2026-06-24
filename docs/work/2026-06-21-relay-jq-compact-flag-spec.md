# Relay JQ Compact Flag Spec

Status: active

Date: 2026-06-21

## Problem

Live testing showed an agent tried `ha-nova relay jq -c`. The Relay jq wrapper rejected it because it only supported `-r`, `-e`, `--file`, and `--jq-file`.

This is avoidable friction: `-c` is common jq muscle memory, and HA NOVA already emits compact JSON for non-raw values.

## Goals

- Accept `ha-nova relay jq -c` without changing output.
- Keep `ha-nova relay jq` intentionally small.
- Document the supported flag subset so agents do not invent broader GNU jq behavior.

## Non-Goals

- Do not implement full jq CLI compatibility.
- Do not change relay `/ws` or `/core` filtering behavior.
- Do not add shell pipelines or external jq guidance.

## Implementation

- Treat `-c` as a no-op compact compatibility flag in `runJQ`.
- Update usage text.
- Add a Go regression test.
- Pin the supported flag subset in the Relay API contract and skill contract tests.

## Verification

- Focused Go tests for relay jq.
- Focused skill contract tests.
- Docs and safe-core verification.
- Dev sync after passing tests.
