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

Read and follow `../ha-nova/session-bootstrap.md`.
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
- entity registry: `{"type":"config/entity_registry/list"}`
- device registry: `{"type":"config/device_registry/list"}`
- system health: `{"message":{"type":"system_health/info"},"collect_events":{"until_type":"finish","max_events":100,"timeout_ms":10000}}`

`system_health/info` is a finite event-response command. The Skill opts into generic Relay event collection through `collect_events`; the Relay only forwards the WS message and enforces `until_type`, `max_events`, and `timeout_ms`. A compatible relay returns `data.events` containing `initial`, zero or more `update`, and `finish` events. If the relay returns `data: null`, `UNSUPPORTED_WS_TYPE`, `VALIDATION_ERROR`, or another unsupported-event response, continue and say system-health details need a relay at the enforced floor — every supported relay has event collection, so this points at an outdated App.

## Data Shapes

Envelope parsing follows `skills/ha-nova/relay-api.md` → Standard Envelope. Normalize each saved relay result separately before summarizing:
- REST `/api/config`, `/api/components`, `/api/states`: use `.data.body`
- WS `repairs/list_issues`: use `.data.issues`
- WS `config_entries/get`: use `.data` as an array
- WS `config/entity_registry/list`: use `.data` as an array; availability joins use only `entity_id`, `config_entry_id`, `device_id`, and `platform`
- WS `config/device_registry/list`: use `.data` as an array; use only `id` to validate a device attribution
- WS `system_health/info`: use `.data.events` as an array; event kind is `.type`

Do not run one combined `jq` normalizer across config, components, states, repairs, integrations, and system health. Their envelopes differ. Normalize each source file into a small source-specific shape first, then combine those normalized summaries in prose.

Before accepting a source, require `ok:true`, a 2xx REST `data.status`, and
valid types for every required row field. If a shape differs, mark that source
unavailable and continue. Do not show parser or `jq` errors; mention only the
affected source in coverage.

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

## Availability Analysis

Read `availability-analysis.md` before classifying config-entry states or
availability. It owns state categories, exact joins, thresholds, the shared
ledger, device clusters, ordering, caps, and privacy.

## Flow

1. Read `/api/config`, `/api/components`, and `/api/states`.
2. Read `repairs/list_issues` and `config_entries/get` through WS.
3. When at least one state is `unavailable` or `unknown`, attempt both full registry reads from Availability Analysis. If either registry source fails or has a different shape, continue with the available evidence and state the attribution limitation.
4. If a relay-outdated warning appeared this session, skip `system_health/info`; say system-health details need the relay updated to the enforced floor and include the current relay version.
5. Otherwise read `system_health/info` through WS and parse `data.events` when available.
6. Summarize:
   - overall: `limited` when a required source is unavailable; otherwise `attention` only for an active repair, a config-entry attention state defined by Availability Analysis, low battery/SOC, or an explicit failed system-health check; otherwise `ok`. Availability classification alone never changes overall.
   - coverage: checked timestamp plus source status for config, components, states, repairs, integrations, and system health
   - repairs: count active issues; group only matching domain, `translation_key`, and remediation while retaining `issue_id`s internally. Never group missing translation keys or distinct actions. Show top 3 by severity/created date.
   - integrations: apply Availability Analysis state categories and order; show up to 5 attention entries with safe domain, state, and sanitized reason — never title/account name. Joined impact is ONE finding owned by `Integrations`; suppress it from `Entities`. Without an exact join, state attribution unavailable.
   - config/components: HA version, time zone, component count, notable missing core pieces; do not expose the installation/location name
   - unavailable/unknown: follow Availability Analysis; report raw entity-state counts, restored/current split, classification, coverage, and the capped shared ledger; never list entity examples or imply device/problem counts
   - low battery/SOC: numeric `device_class: battery` under 20%, plus `device_class: battery` state entities that are `low`; do not imply a battery replacement unless the entity is clearly a device battery
   - system health: failed object-shaped `update` events first, then max 3 object-shaped `initial.data` highlights; ignore scalars and `finish`
7. Bind each conclusion to the data source used.

Sanitize integration reasons before showing them:
- remove IP addresses, hostnames, URLs, tokens, and long raw exception text
- never render `error_reason_translation_key` verbatim; map only recognized
  generic keys to safe localized phrases
- for unknown keys or raw technical/sensitive reasons, say
  `technical setup error` and name the state instead

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

These names are semantic output slots, not literal headings. Do not mix English labels with localized prose unless the label is a Home Assistant state/value.
Deterministic internal sorting happens before localization; localize every
generic label, ordinal, classification, overall phrase, and next step to the
user's language at runtime.

- `Status`
- `Repairs`
- `Entities`
- `Integrations`
- `System`
- `Next step`

These slots are the Report shape (output-rules.md): `Status` is the answer-first lead, `Next step` closes. Keep it compact. Compute overall internally as `ok`, `attention`, or `limited`, but show the overall value as a localized human phrase, not the raw enum. Include localized labels for overall status, checked time, and source coverage in `Status`. Keep Home Assistant state values such as `unavailable`, `unknown`, `setup_error`, `setup_retry`, and `not_loaded` literal when they are evidence, with a short localized explanation when helpful. Do not dump raw JSON, full logs, full entity lists, full component lists, or full integration-entry lists.

Choose one safe `Next step`:
- repairs first
- then any config-entry attention/failure state from Availability Analysis
- then low battery/SOC
- then failed system health
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
