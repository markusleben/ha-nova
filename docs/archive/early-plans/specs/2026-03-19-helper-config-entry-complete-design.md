# Helper Config-Entry Completion Design

Date: 2026-03-19
Issue: `#81`

## Goal

Complete `ha-nova:helper` support for all 9 config-entry helper domains named in `#81`:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`

## Done Criteria

- list/read/create/update/delete contracts cover all 9 domains
- update is documented only where locally proven on Markus's HA
- multi-step create/update flows are documented only where locally proven on Markus's HA
- unsupported update/reconfigure cases fail loud instead of guessing
- `ha-nova:fallback` no longer owns those 9 domains
- helper docs/tests/architecture stay internally consistent
- local live validation covers real create/read/update/delete for every delivered domain

## Constraints

- relay stays dumb
- no server/runtime code changes
- skills/docs/tests only
- config-entry verification remains config-entry-first
- no hidden scope creep into unrelated helper families

## Validation Plan

1. Probe actual create/update flow shape on Markus's HA for all 9 domains.
2. Use the observed capability matrix as the source of truth.
3. Update skill docs/contracts to match only what was proven.
4. Re-run repo tests.
5. Re-run local live CRUD validation with cleanup.

## Observed Capability Matrix

Live-proven create/update/delete on Markus's HA:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`

Important live notes:

- create/update/delete succeeded for all 9 through relay `/core`
- all 9 created loaded entries with `supports_options: true`
- field-level update verification required reopening the options flow and reading `description.suggested_value`
- `group` create starts as a menu and then switches to a subtype-specific final form
- `history_stats` enforces the HA two-key window invariant across `start`, `end`, and `duration`
