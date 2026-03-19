# HA NOVA Config-Entry Helper Flow Schemas

Reference for the config-entry helper family handled by `ha-nova:helper`.
This file covers the PR1 foundation slice only:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`

This file is an observed field inventory for PR1, not a complete validation schema.
Use it to confirm supported domains, field names, and flow shape.
If enum semantics, required/optional behavior, or cross-field rules are uncertain, fail loud instead of guessing.

## Common Rules

- Canonical write identity: `entry_id`
- Canonical list/metadata-read item: `entry_id`, `domain`, `title`, `state`, `linked_entities[]`
- Create mutation path:
  - `POST /api/config/config_entries/flow` with a handler-start body
  - `POST /api/config/config_entries/flow/{flow_id}` with a form-submit body
  - capture `flow_id` from the start response before submitting the form step
- Read/list source:
  - WS `config_entries/get`
  - WS `config/entity_registry/list` for `linked_entities[]`
- Delete mutation path:
  - `DELETE /api/config/config_entries/entry/{entry_id}`
- Verification source of truth:
  - create success = `entry_id` from the terminal flow result confirmed in the after-read, or a constrained before/after `entry_id` diff if the flow result omits it
  - the before/after fallback requires a pre-create `config_entries/get` baseline
  - the before/after fallback passes only when exactly one new `entry_id` appeared and that new entry is consistent with the requested `domain` and `title`
  - if the fallback diff is empty, plural, or metadata-inconsistent, fail loud as ambiguous create verification
  - delete success = entry absent in `config_entries/get`
  - linked entity appearance/disappearance is secondary evidence only

Observed locally on Markus's HA on 2026-03-19:

- all six create flows started at `step_id: user`
- all six returned `last_step: true` on the first form
- raw WS `config_entries/flow` did not succeed in this session
- relay `/core` to `/api/config/config_entries/flow` returned the expected form payloads

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
- Observed flow shape:
  - `step_id: user`
  - `last_step: true`
- Existing local entries observed:
  - yes
  - `supports_options: true`
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
- Observed flow shape:
  - `step_id: user`
  - `last_step: true`
- Existing local entries observed:
  - no
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry after create

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
- Observed flow shape:
  - `step_id: user`
  - `last_step: true`
- Existing local entries observed:
  - yes
  - `supports_options: true`
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry

## min_max

- Handler: `min_max`
- Observed create fields:
  - `name`
  - `entity_ids`
  - `type`
  - `round_digits`
- Observed flow shape:
  - `step_id: user`
  - `last_step: true`
- Existing local entries observed:
  - yes
  - `supports_options: true`
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
- Observed flow shape:
  - `step_id: user`
  - `last_step: true`
- Existing local entries observed:
  - no
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry after create

## tod

- Handler: `tod`
- Observed create fields:
  - `name`
  - `after_time`
  - `before_time`
- Observed flow shape:
  - `step_id: user`
  - `last_step: true`
- Existing local entries observed:
  - no
- Linked entity resolution:
  - resolve by matching `config_entry_id` in entity registry after create

## PR1 Limits

- Update is out of scope in this file and in this slice.
- Multi-step domains are intentionally excluded from this file:
  - `group`
  - `statistics`
  - `history_stats`
- Unsupported/other config-entry helper families remain in `ha-nova:fallback` for now.
