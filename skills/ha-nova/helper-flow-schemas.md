# HA NOVA Config-Entry Helper Flow Schemas

Reference for the config-entry helper family handled by `ha-nova:helper`.
This file covers the PR1 foundation slice only:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`

## Common Rules

- Canonical write identity: `entry_id`
- Canonical list/read item: `entry_id`, `domain`, `title`, `state`, `linked_entities[]`
- Create mutation path:
  - `POST /api/config/config_entries/flow` with a handler-start body
  - `POST /api/config/config_entries/flow/{flow_id}` with a form-submit body
- Read/list source:
  - WS `config_entries/get`
  - WS `config/entity_registry/list` for `linked_entities[]`
- Delete mutation path:
  - `DELETE /api/config/config_entries/entry/{entry_id}`
- Verification source of truth:
  - create success = created `entry_id` returned by the terminal flow result, or a before/after `entry_id` diff if the flow result omits it
  - the before/after fallback requires a pre-create `config_entries/get` baseline
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
