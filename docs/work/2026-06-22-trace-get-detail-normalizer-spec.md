# Trace Get Detail Normalizer Spec

Date: 2026-06-22

## Problem

Agents still inspect `trace/get` responses with ad-hoc jq projections. Home Assistant trace details expose summary fields under `.data`, while `.data.trace` contains step data and may be null. This caused repeated local parser friction even after `ha-nova trace list` fixed run selection.

## MVP

Add `ha-nova trace get <automation.entity_id|script.entity_id> <run_id> --json`.

It must:

- resolve `entity_id` to the registry `unique_id`
- call `trace/get` with the real `run_id`
- return a stable JSON summary: `item_id`, `timestamp`, `last_step`, `state`, `script_execution`, `error`
- preserve the raw `.data` payload as `trace`
- avoid `trace/list` for direct detail reads

## Non-goals

- No trace analysis engine.
- No automatic service calls or automation triggering.
- No Relay business logic.

## Verification

- Go regression test for normalized trace detail.
- Skill contract test documents `trace get`.
- Live read-only smoke against `automation.nachtladung_prepare`.
