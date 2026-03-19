# Design: Helper Config-Entry Foundation

**Date:** 2026-03-19
**Status:** Approved

## Summary

Implement the first slice of `#81` by extending `ha-nova:helper` with a new config-entry helper family for six single-step domains:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`

This slice delivers:

- list
- read
- create
- delete
- minimal config-entry post-write review

It does **not** deliver config-entry helper update yet.

## Observed Local Capability Matrix

Observed on Markus's HA via `ha-nova relay` on 2026-03-19:

| Domain | Flow start path | `step_id` | `last_step` | Fields |
|---|---|---|---|---|
| `utility_meter` | `/api/config/config_entries/flow` | `user` | `true` | `name`, `source`, `cycle`, `offset`, `tariffs`, `net_consumption`, `delta_values`, `periodically_resetting`, `always_available` |
| `derivative` | `/api/config/config_entries/flow` | `user` | `true` | `name`, `source`, `round`, `time_window`, `unit_prefix`, `unit_time`, `max_sub_interval` |
| `integration` | `/api/config/config_entries/flow` | `user` | `true` | `name`, `unit_prefix`, `unit_time`, `source`, `method`, `round`, `max_sub_interval` |
| `min_max` | `/api/config/config_entries/flow` | `user` | `true` | `name`, `entity_ids`, `type`, `round_digits` |
| `threshold` | `/api/config/config_entries/flow` | `user` | `true` | `name`, `hysteresis`, `lower`, `upper`, `entity_id` |
| `tod` | `/api/config/config_entries/flow` | `user` | `true` | `name`, `after_time`, `before_time` |

Additional observations from `config_entries/get`:

- existing local `utility_meter`, `integration`, and `min_max` entries report `supports_options: true`
- the local proof for update semantics is deferred to a later slice
- linked entities can be resolved through `config/entity_registry/list` by matching `config_entry_id`

## Key Decisions

- `ha-nova:helper` becomes a two-family skill:
  - storage-based family
  - config-entry family
- config-entry canonical identity is `entry_id`
- create/delete mutations use relay `/core`
- list/read uses WS `config_entries/get` and entity-registry joins
- create success must prove a new `entry_id`, either from the terminal flow result or a before/after `config_entries/get` diff
- the diff fallback requires a pre-create `config_entries/get` snapshot
- flow start and flow submit use different payload schemas and must not reuse one body file contract
- delete success stays config-entry-first, entity-second
- fallback keeps unsupported and not-yet-delivered config-entry helper families

## Out of Scope

- config-entry helper update flows
- multi-step config-entry helper domains:
  - `group`
  - `statistics`
  - `history_stats`
- new helper-review taxonomy beyond the minimal post-write contract
