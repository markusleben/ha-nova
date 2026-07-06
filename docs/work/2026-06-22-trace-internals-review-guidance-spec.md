# Trace Internals Review Guidance Spec

Date: 2026-06-22

## Problem

Live review tests still report minor jq friction after the trace helper improvements. The remaining issue is not run selection or trace detail retrieval; it happens when agents inspect raw `trace.trace` internals and assume each node is a plain object instead of an array of event records.

## MVP

Update the review skill so agents:

- use `ha-nova trace latest/list/get --json` as the default trace path
- treat normalized summary fields as sufficient for most review findings
- inspect raw trace internals only when step-level evidence is required
- type-check raw trace nodes before reading `path`, `result`, `changed_variables`, or `error`
- avoid large jq projections over raw trace internals as the standard review path

## Non-goals

- No new trace analysis engine.
- No additional Relay behavior.
- No automatic automation triggering.

## Verification

- Add a skill contract test for defensive raw trace guidance.
- Run focused review/trace contracts, docs verification, safe-core, and full verify.
