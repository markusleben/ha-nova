---
name: health
description: Use when checking Home Assistant home status, repairs, system health, unavailable/unknown entities, low batteries, or component/config summaries through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Home Status

## Scope

Read-only home status checks:
- repairs/deprecation issues
- integration entries that are not loaded
- config and loaded component summary
- unavailable/unknown entity summary
- low battery/SOC summary
- best-effort system health info

Not in scope:
- repair/fix/ignore actions
- statistics repair, purge, registry cleanup (`ha-nova:maintenance`)
- restart, reload, update, backup, or service calls
- YAML/filesystem diagnostics
- historical timelines (use `ha-nova:history`)

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`
Read `data.version` from the health response.

## Relay Contract

Use file-based relay requests:
- `ha-nova relay core --method GET --path /api/config --out <result-file>`
- `ha-nova relay core --method GET --path /api/components --out <result-file>`
- `ha-nova relay core --method GET --path /api/states --out <result-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>`
- `ha-nova relay jq --file <result-file> --jq-file <filter-file>`

For WS payloads:
- repairs: `{"type":"repairs/list_issues"}`
- integration entries: `{"type":"config_entries/get"}`
- system health: `{"message":{"type":"system_health/info"},"collect_events":{"until_type":"finish","max_events":100,"timeout_ms":10000}}`

`system_health/info` is a finite event-response command. The Skill opts into generic Relay event collection through `collect_events`; the Relay only forwards the WS message and enforces `until_type`, `max_events`, and `timeout_ms`. A compatible relay returns `data.events` containing `initial`, zero or more `update`, and `finish` events. If the relay returns `data: null`, `UNSUPPORTED_WS_TYPE`, `VALIDATION_ERROR`, or another unsupported-event response, continue and say system-health details need Relay 0.2.3 or newer.

## Data Shapes

Envelope parsing follows `skills/ha-nova/relay-api.md` → Standard Envelope. Normalize each saved relay result separately before summarizing:
- REST `/api/config`, `/api/components`, `/api/states`: use `.data.body`
- WS `repairs/list_issues`: use `.data.issues`
- WS `config_entries/get`: use `.data` as an array
- WS `system_health/info`: use `.data.events` as an array; event kind is `.type`

Do not run one combined `jq` normalizer across config, components, states, repairs, integrations, and system health. Their envelopes differ. Normalize each source file into a small source-specific shape first, then combine those normalized summaries in prose.

Use type checks in `jq` filters before indexing arrays or objects. If a shape differs, mark that source unavailable and continue. Do not show intermediate parser or `jq` errors to the user; mention only the affected source in coverage.

Use `--jq-file` for non-trivial filters. Avoid complex inline jq, especially regex that must be shell-escaped. Prefer simple field equality and type checks over regex whenever Home Assistant attributes already provide structured data.

System Health event payloads are mixed-shape. For events from `.data.events[]`:
- read the event kind from `.type`
- before reading `.data.info` or any nested field, require `(.data | type) == "object"`
- failure can be signaled on the event itself (`success:false` or `error`) or inside object-shaped `.data` (`.data.success == false` or `.data.error`)
- scalar `.data` values such as strings, numbers, booleans, or null are informational only; do not count them as failed checks and do not let them break jq filters
- failed update detection must consider only `update` events whose event-level fields or object-shaped `.data` explicitly indicate failure

Low-battery detection must be structured:
- primary numeric detector: entities whose `attributes.device_class == "battery"` and numeric state is below 20
- primary state detector: entities whose `attributes.device_class == "battery"` and state is `low`
- do not use shell-escaped regex on `entity_id` for the main battery filter
- name/entity text such as `battery` or `batterie` may be used only as secondary context after the structured detector, never as the main signal

## Flow

1. Read `/api/config`, `/api/components`, and `/api/states`.
2. Read `repairs/list_issues` and `config_entries/get` through WS.
3. If the relay version is below `0.2.3`, do not call `system_health/info`; say system-health details need Relay 0.2.3 or newer and include the current relay version.
4. Otherwise read `system_health/info` through WS and parse `data.events` when available.
5. Summarize:
   - overall: `ok` only when all read sources are available and there are no active repairs, non-disabled not-loaded integrations, important unavailable/unknown examples, or low battery/SOC findings; `limited` when a source is unavailable; otherwise `attention`
   - coverage: checked timestamp plus source status for config, components, states, repairs, integrations, and system health
   - repairs: active issue count; top 3 by severity/created date with integration/domain, severity, issue title/translation key when available
   - integrations: count entries whose `state` is not `loaded`; treat entries with `disabled_by` set as intentionally disabled context, not attention items; show up to 5 non-disabled `setup_error`, `setup_retry`, `migration_error`, then `not_loaded` entries with domain, title, state, and sanitized reason
   - config/components: HA version, location name, time zone, component count, notable missing core pieces if visible
   - unavailable/unknown: raw counts by domain plus up to 5 attention examples; deprioritize noisy/stateless domains `button`, `event`, `scene`, and `stt` for examples
   - low battery/SOC: numeric `device_class: battery` under 20%, plus `device_class: battery` state entities that are `low`; do not imply a battery replacement unless the entity is clearly a device battery
   - system health: summarize failed object-shaped `update` events first, then max 3 useful highlights from object-shaped `initial.data`; ignore scalar payloads and the `finish` event
6. Bind each conclusion to the data source used.

Sanitize integration reasons before showing them:
- remove IP addresses, hostnames, URLs, tokens, and long raw exception text
- prefer `error_reason_translation_key` when present
- if the raw reason is technical or sensitive, say `technical setup error` and name the state instead

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

These names are semantic output slots, not literal headings. Do not mix English labels with localized prose unless the label is a Home Assistant state/value.

- `Status`
- `Repairs`
- `Entities`
- `Integrations`
- `System`
- `Next step`

Keep it compact. Compute overall internally as `ok`, `attention`, or `limited`, but show the overall value as a localized human phrase, not the raw enum. Include localized labels for overall status, checked time, and source coverage in `Status`. Keep Home Assistant state values such as `unavailable`, `unknown`, `setup_error`, `setup_retry`, and `not_loaded` literal when they are evidence, with a short localized explanation when helpful. Do not dump raw JSON, full logs, full entity lists, full component lists, or full integration-entry lists.

Choose one safe `Next step`:
- repairs first
- then integration `setup_error`/`setup_retry`
- then low battery/SOC
- then important unavailable/unknown examples
- then source limitation such as outdated relay
- otherwise say no immediate action found

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- Read-only skill. No writes.
- Never call repair/fix/ignore/delete issue commands.
- Never restart/reload Home Assistant from this skill.
- Never call update, backup, or service actions from this skill.
- If a status surface is unavailable, mark that part unavailable and continue.
