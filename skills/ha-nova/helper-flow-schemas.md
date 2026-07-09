# HA NOVA Config-Entry Helper Flow Schemas

Reference for the config-entry helper family handled by `ha-nova:helper`.
This file covers the full supported config-entry family (10 domains):

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`
- `template`

This file is an observed field inventory, not a complete validation schema.
Use it to confirm supported domains, flow shape, update expectations, and verification notes.
If enum semantics, required/optional behavior, or cross-field rules are uncertain, fail loud instead of guessing.

## Common Rules

- Canonical write identity: `entry_id`
- Canonical list/read item:
  - `entry_id`
  - `domain`
  - `title`
  - `state`
  - `linked_entities[]`
  - `supports_options`
- Read/list source:
  - WS `config_entries/get`
  - WS `config/entity_registry/list` for `linked_entities[]`
- Current editable options readback:
  - start an options flow when `supports_options: true`
  - treat the returned current step as the current editable options snapshot
  - if an exposed field has no `description.suggested_value`, treat the current value as unavailable instead of guessed
  - do not invent fields not exposed in the current step
- Create mutation path:
  - `POST /api/config/config_entries/flow` with a handler-start body
  - iterate `POST /api/config/config_entries/flow/{flow_id}` until terminal `create_entry`
  - capture `flow_id` from the start response before submitting later steps
  - handler-start body = start-flow payload only
  - form-submit body = current step fields only
- Update mutation path:
  - `POST /api/config/config_entries/options/flow` with `{"handler":"<entry_id>"}`
  - iterate `POST /api/config/config_entries/options/flow/{flow_id}` until terminal `create_entry`
  - if the entry exposes no options flow on the running HA version, fail loud as unsupported update
  - if a requested field is exposed but lacks `description.suggested_value`, fail loud instead of guessing the current value
  - carry forward unchanged required fields from the current options snapshot when building the submit body
- Delete mutation path:
  - `DELETE /api/config/config_entries/entry/{entry_id}`
- Verification source of truth:
  - create success = `entry_id` from the terminal flow result confirmed in the after-read, or a constrained before/after `entry_id` diff if the flow result omits it
  - the before/after fallback requires a pre-create `config_entries/get` baseline
  - the before/after fallback passes only when exactly one new `entry_id` appeared and that new entry is consistent with the requested `domain` and `title`
  - if the create diff is empty, plural, or metadata-inconsistent, fail loud as ambiguous create verification
  - update success = the same `entry_id` still exists in `config_entries/get` and a reopened options flow shows the requested changed fields in `description.suggested_value`
  - if a requested changed field is exposed in the verification step but lacks `description.suggested_value`, fail loud as unverifiable update on this HA version
  - delete success = entry absent in `config_entries/get`
  - linked entity appearance/disappearance is secondary evidence only

Observed locally on a real HA instance on 2026-03-19:

- the original 9 domains: all create flows succeeded through relay `/core` (2026-03-19)
- the original 9 domains: all create flows produced loaded entries with `supports_options: true`; `template` was observed separately (see its section)
- raw WS `config_entries/flow` did not succeed in this session
- field-level update verification required reopening the options flow
- several domains reject partial required-field submits; helper update must merge requested changes over the current options snapshot

## utility_meter

- Handler: `utility_meter`
- Observed create fields:
  - `name`
  - `source`
  - `cycle`
  - `offset`
  - `tariffs`
  - `net_consumption`
  - `delta_values`
  - `periodically_resetting`
  - `always_available`
- Observed create flow shape:
  - `step_id: user`
  - `last_step: true`
- Observed update fields:
  - `source`
  - `periodically_resetting`
  - `always_available`
- Update notes:
  - `source` and `periodically_resetting` were required in the options step
  - `cycle`, `offset`, and tariff configuration were not exposed in the observed options step
  - if the user requests a non-exposed field, fail loud as unsupported update for that field on this HA version
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## derivative

- Handler: `derivative`
- Observed create fields:
  - `name`
  - `source`
  - `round`
  - `time_window`
  - `unit_prefix`
  - `unit_time`
  - `max_sub_interval`
- Observed create flow shape:
  - `step_id: user`
  - `last_step: true`
- Observed update fields:
  - `source`
  - `round`
  - `time_window`
  - `unit_prefix`
  - `unit_time`
  - `max_sub_interval`
- Update notes:
  - partial submit without required carry-forward fields failed locally
  - resubmit `source`, `round`, `time_window`, and `unit_time` when changing any required value
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## integration

- Handler: `integration`
- Observed create fields:
  - `name`
  - `unit_prefix`
  - `unit_time`
  - `source`
  - `method`
  - `round`
  - `max_sub_interval`
- Observed create flow shape:
  - `step_id: user`
  - `last_step: true`
- Observed update fields:
  - `source`
  - `method`
  - `round`
  - `max_sub_interval`
- Update notes:
  - `unit_time` was set at create but was not exposed in the observed options step
  - treat the current options step, not the create step, as the authoritative mutable field set
  - if the user requests a non-exposed field, fail loud as unsupported update for that field on this HA version
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## min_max

- Handler: `min_max`
- Observed create fields:
  - `name`
  - `entity_ids`
  - `type`
  - `round_digits`
- Observed create flow shape:
  - `step_id: user`
  - `last_step: true`
- Observed update fields:
  - `entity_ids`
  - `type`
  - `round_digits`
- Update notes:
  - all three observed fields remained editable in one options step
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## threshold

- Handler: `threshold`
- Observed create fields:
  - `name`
  - `hysteresis`
  - `lower`
  - `upper`
  - `entity_id`
- Observed create flow shape:
  - `step_id: user`
  - `last_step: true`
- Observed update fields:
  - `hysteresis`
  - `lower`
  - `upper`
  - `entity_id`
- Update notes:
  - partial submit without `entity_id` failed locally
  - carry forward `entity_id` when changing thresholds
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## tod

- Handler: `tod`
- Observed create fields:
  - `name`
  - `after_time`
  - `before_time`
- Observed create flow shape:
  - `step_id: user`
  - `last_step: true`
- Observed update fields:
  - `after_time`
  - `before_time`
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## statistics

- Handler: `statistics`
- Observed create flow shape:
  1. `step_id: user`
     - `name`
     - `entity_id`
  2. `step_id: state_characteristic`
     - `state_characteristic`
  3. `step_id: options`
     - read-only `entity_id`
     - read-only `state_characteristic`
     - `sampling_size`
     - `max_age`
     - `keep_last_sample`
     - `percentile`
     - `precision`
- Observed update fields:
  - read-only `entity_id`
  - read-only `state_characteristic`
  - `sampling_size`
  - `max_age`
  - `keep_last_sample`
  - `percentile`
  - `precision`
- Update notes:
  - `entity_id` and `state_characteristic` stayed read-only in the options step
  - mutable verification was proven for `sampling_size` and `precision`
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## group

- Handler: `group`
- Observed create flow shape:
  1. `step_id: user` menu
     - submit `next_step_id`
     - observed menu options:
       - `binary_sensor`
       - `button`
       - `cover`
       - `event`
       - `fan`
       - `light`
       - `lock`
       - `media_player`
       - `notify`
       - `sensor`
       - `switch`
       - `valve`
  2. subtype-specific final form
     - observed live subtype: `sensor`
     - observed fields for `sensor`:
       - `name`
       - `entities`
       - `hide_members`
       - `type`
- Observed update fields for the live `sensor` subtype:
  - `entities`
  - `hide_members`
  - `ignore_non_numeric`
  - `type`
- Update notes:
  - end-to-end CRUD was proven locally for the `sensor` subtype only
  - the options step is subtype-specific (`step_id` matched the subtype)
  - always trust the live current options step over a hardcoded subtype schema
  - if another subtype exposes a different live step than expected, anchor to the live step fields or fail loud instead of guessing
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## history_stats

- Handler: `history_stats`
- Observed create flow shape:
  1. `step_id: user`
     - `name`
     - `entity_id`
     - `type`
  2. `step_id: state`
     - read-only `entity_id`
     - `state`
  3. `step_id: options`
     - read-only `entity_id`
     - read-only `state`
     - read-only `type`
     - `start`
     - `end`
     - `duration`
     - `state_class`
- Observed update fields:
  - read-only `entity_id`
  - read-only `state`
  - read-only `type`
  - `start`
  - `end`
  - `duration`
  - `state_class`
- Update notes:
  - HA enforced a two-key window invariant across `start`, `end`, and `duration`
  - a valid submit must use exactly two of those three keys
  - live success was proven with `start + end`
  - if the requested change switches to a different valid pair, drop the previous third key explicitly before submit
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## template

- Handler: `template`
- Observed create flow shape (menu-driven, like `group`):
  1. `step_id: user`, `type: menu` — `menu_options` is the entity-type list (observed: `alarm_control_panel`, `binary_sensor`, `button`, `cover`, `device_tracker`, `event`, `fan`, `image`, `light`, `lock`, `number`, `select`, `sensor`, `switch`, `update`, `vacuum`, `weather`); submit `{"next_step_id":"<type>"}`
  2. `step_id: sensor` (verified subtype), `type: form`:
     - `name` (required, text)
     - `state` (required, template)
     - `unit_of_measurement` (optional, select)
     - `device_class` (optional, select)
     - `state_class` (optional, select)
     - `device_id` (optional, device)
     - `advanced_options` (optional, expandable — present on the create form too)
- Observed update fields (options flow, `step_id: sensor`):
  - `state`
  - `unit_of_measurement`
  - `device_class`
  - `state_class`
  - `device_id`
  - `advanced_options`
  - `name` is NOT editable via the options flow — renames go through the entity registry (`ha-nova:organize`)
- Subtype notes:
  - end-to-end support verified for `sensor`; other subtypes must anchor to the live step schema instead of guessed fields
  - the `state` value is a Jinja template string; a broken template creates the entry but the entity renders `unavailable` — post-write verification must read the rendered state
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry
