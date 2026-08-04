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

## Report Modes

Two independent dimensions; both are always visible at the top of every
report. Natural-language requests select them ("short/compact", "explained",
"full", "private", "shareable", "aggregate", localized); no CLI syntax.
Default: `Explained + Private`.

- Detail: `Compact` (overall result + three highest-priority actions) ·
  `Explained` (default: all actionable findings, explanations, small-group
  details, summarized large groups) · `Full` (every finding and group;
  selected large groups in bounded chunks).
- Privacy: `Private` · `Shareable` · `Aggregate` — semantics in
  `availability-analysis.md` → Privacy modes. When identities are hidden, say
  why and how to request private detail. Census participation never affects
  local report detail. A follow-up such as "show all entities in MQTT"
  requests that group's private detail.

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
- area registry (optional, only when human-readable area names are shown): `{"type":"config/area_registry/list"}`
- system health: `{"message":{"type":"system_health/info"},"collect_events":{"until_type":"finish","max_events":100,"timeout_ms":10000}}`

`system_health/info` is a finite event-response command. The Skill opts into generic Relay event collection through `collect_events`; the Relay only forwards the WS message and enforces `until_type`, `max_events`, and `timeout_ms`. A compatible relay returns `data.events` containing `initial`, zero or more `update`, and `finish` events. If the relay returns `data: null`, `UNSUPPORTED_WS_TYPE`, `VALIDATION_ERROR`, or another unsupported-event response, continue and say system-health details need a relay at the enforced floor — every supported relay has event collection, so this points at an outdated App.

## Data Shapes

Envelope parsing follows `skills/ha-nova/relay-api.md` → Standard Envelope. Normalize each saved relay result separately before summarizing:
- REST `/api/config`, `/api/components`, `/api/states`: use `.data.body`
- WS `repairs/list_issues`: use `.data.issues`
- WS `config_entries/get`: use `.data` as an array
- WS `config/entity_registry/list`: use `.data` as an array
- WS `config/device_registry/list`: use `.data` as an array
- WS `config/area_registry/list`: use `.data` as an array; use only `area_id` and `name`
- WS `system_health/info`: use `.data.events` as an array; event kind is `.type`

Private detail uses explicit, minimal source fields — never more:
- state rows: `entity_id`, `state`, `last_changed`, `attributes.restored`, `attributes.friendly_name`, structured battery/SOC metadata
- entity registry: `entity_id`, `config_entry_id`, `device_id`, `platform`, `name`, `original_name`, `area_id`
- device registry: `id`, `name_by_user`, `name`, `area_id`
- config entries: existing state/domain fields plus `title`, sanitized before display

Optional display metadata that is unavailable is marked unavailable; it never
turns an otherwise complete result into a system failure.

Do not run one combined `jq` normalizer across config, components, states, repairs, integrations, and system health. Their envelopes differ. Normalize each source file into a small source-specific shape first, then combine those normalized summaries.

Before accepting a source, require `ok:true`, a 2xx REST `data.status`, and valid types for every required row field. If a shape differs, mark that source unavailable and continue. Do not show parser or `jq` errors; mention only the affected source in coverage.

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
availability. It owns state categories, the assignment precedence, exact
joins, grouping and detail budgets, finding priority, privacy modes, and
display-name rules.

## Report Order (table-first, cause before symptom)

1. Overall status, active modes, checked time, one plain-language conclusion
2. Prioritized action summary
3. Active repairs and maintenance actions
4. Integration findings with their owned entity impact
5. Remaining entity availability findings
6. Low battery/SOC values
7. System status
8. Ignored, disabled, transitional, and other non-actionable context
9. Source coverage and reconciliation details
10. One safest next step

Every non-empty block uses the same shape: localized block label; one- or
two-sentence assessment BEFORE the table; one primary compact table when at
least two comparable rows exist; one optional action sentence after it.
Tables hold comparable facts — explanations, uncertainty, and instructions
stay outside cells; friendly name and exact ID may share one cell; a single
finding may stay a compact text record; use a stacked record fallback when
four short columns would be too wide. Severity never depends on color alone:
pair `🔴`/`🟠`/`🟡` with a plain-language title or status ("act", "check",
"information").

## Behavior

- **Attribution honesty.** Without an exact join, state attribution unavailable. In `Private` mode Integrations may show a sanitized config-entry title; `Shareable`/`Aggregate` never show title/account identity.
- **Overall separates availability from findings.** The lead distinguishes
  whether core and required sources are available from whether findings need
  attention. A running core never turns active findings into "everything is
  fine"; a large entity-state count is never described as a core outage.
  Overall stays `limited` for missing required sources; `attention` only for
  an active repair, an attention-state integration, an explicit system-health
  failure, or a structured low battery/SOC finding. Unexplained availability
  findings get their own prominent review block but do not change the enum.
  Restored, stateless, ignored, disabled, and transitional context alone
  never creates attention.
- **Repairs name their exact targets.** Count active issues; group only
  matching domain, `translation_key`, and remediation while retaining
  `issue_id`s internally; never group missing translation keys or distinct
  actions. Normalize detail only from validated repair fields (`domain`,
  `issue_id`, `translation_key`, safe `translation_placeholders`, severity,
  created/remediation metadata) — never fuzzy-match update entities to invent
  targets or versions. HACS restart-required rows show the exact repository
  display name, installed version, target version when available, and whether
  one shared restart resolves several rows; never collapse two distinct
  targets into "two updates require a restart". Missing target/version data
  renders "not supplied by source". Active and ignored repairs stay separate;
  an ignored warning is context; never invent a "resolved" category. Show top 3 by severity/created date in `Compact`; `Explained` shows every active repair.
- **Low values keep their semantics.** Structured metadata is the primary
  detector: numeric `device_class: battery` under 20, plus `device_class:
  battery` state `low`. Classify with this ordered mapping, first match wins: (1) owning
  integration/platform `mobile_app` → rechargeable phone/watch battery;
  (2) an explicit vehicle or storage component/typed attribute → vehicle/storage SOC;
  (3) `device_class: battery` on a non-mobile device → replaceable device battery;
  (4) otherwise → unclassified low percentage. Structured evidence only —
  never from an entity name. Render the four classes separately;
  do not imply a battery replacement unless the entity is clearly a device battery — never recommend replacement for a phone/watch; never call vehicle SOC 0% a Home Assistant failure without connection context.
- **System and coverage stay compact and honest.** System health: failed object-shaped `update` events first, then max 3 object-shaped `initial.data` highlights; ignore scalars and `finish`. System: HA version/mode,
  loaded component count, explicit system-health failures, storage/DB signals
  (neutral unless a documented threshold or trend supports an assessment); do
  not expose the installation/location name. Coverage sits near the end:
  each source available/limited/unavailable plus exact registry join
  coverage; never call device-registry records "broken devices".
- **Missing data is explicit.** Use "not evaluated", "not supplied by
  source", or "source unavailable" — never zero, "no impact", or a guessed
  cause. `last_changed` is the current state row's age, never the full outage
  duration; never invent last valid values, trends, or automation
  dependencies.
- Sanitize integration reasons before showing them:
  remove IP addresses, hostnames, URLs, tokens, and long raw exception text; never render
  `error_reason_translation_key` verbatim — map recognized generic keys to
  safe localized phrases, otherwise say `technical setup error` plus the
  literal state.

## Flow

1. Read `/api/config`, `/api/components`, and `/api/states`.
2. Read `repairs/list_issues` and `config_entries/get` through WS.
3. When at least one state is `unavailable` or `unknown`, attempt both full registry reads from Availability Analysis; read the area registry only when area names will be shown. If a registry source fails, continue with available evidence and state the attribution limitation.
4. If a relay-outdated warning appeared this session, skip `system_health/info`; say system-health details need the relay updated to the enforced floor and include the current relay version.
5. Otherwise read `system_health/info` and parse `data.events`.
6. Build the report per Report Order, Availability Analysis, and the Behavior rules; bind each conclusion to the data source used.

## Output Format

Apply `skills/ha-nova/output-rules.md` (including → Progressive Detail: every
truncation carries total/shown/omitted counts and a precise request for the
full set — never a bare "N more").

These names are semantic output slots, not literal headings. Do not mix English labels with localized prose unless the label is a Home Assistant state/value.
Deterministic internal sorting happens before localization; localize every
generic label, ordinal, classification, overall phrase, mode name, and next
step at runtime. No installation-specific text is hard-coded:

- `Status` (answer-first lead. Compute overall internally as `ok`, `attention`, or `limited`, but show the overall value as a localized human phrase, not the raw enum — plus active modes, checked time, and the one-sentence conclusion; all source coverage lives in `Coverage`, never here)
- `Actions` (prioritized action summary)
- `Repairs`
- `Integrations`
- `Entities`
- `Batteries`
- `System`
- `Context` (ignored/disabled/transitional/non-actionable)
- `Coverage`
- `Next step`

Keep Home Assistant state values such as `unavailable`, `unknown`, `setup_error`, `setup_retry`, and `not_loaded` literal when they are evidence, with a short localized explanation when helpful; entity IDs stay literal. Do not dump raw JSON, full logs, or full unrequested inventories —
detail follows the mode and the budgets in `availability-analysis.md`.

Choose one safest `Next step` from the same ranked finding ledger as
`Actions` (finding priority, Availability Analysis). When ranked evidence
does not separate candidates, break ties in this order:
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
