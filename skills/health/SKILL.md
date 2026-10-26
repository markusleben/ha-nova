---
name: health
description: Use when checking Home Assistant home status, repairs, system health, unavailable/unknown entities, low batteries, or component/config summaries through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Home Status

## Scope

Home status checks; ignoring a repair is the only write:
- repairs/deprecation issues
- integration entries that are not loaded
- config and loaded component summary
- unavailable/unknown entity summary
- low battery/SOC summary
- best-effort system health info

Not in scope:
- repair fix flows
- statistics repair, purge, registry cleanup (`ha-nova:maintenance`)
- restart, reload, update, backup, or service calls
- YAML/filesystem diagnostics
- historical timelines (use `ha-nova:history`)
- root-causing one concrete incident (`ha-nova:diagnose`) — health reports current status, not why one thing failed
- how long an entity has been unavailable, and whether it is a cleanup candidate (`ha-nova:maintenance` long-unavailable report)

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`
Read `data.version` from the health response.

## Report Modes

Both dimensions are always visible; natural-language requests select them, no CLI syntax.
Default: `Explained + Private`.

- Detail: `Compact` (overall result + three highest-priority actions) ·
  `Explained` (default: all actionable findings, explanations, small-group
  details, summarized large groups) · `Full` (every finding and group;
  selected large groups in bounded chunks).
- Privacy: `Private` · `Shareable` · `Aggregate` — semantics in
  `availability-analysis.md` → Privacy modes. When identities are hidden, say
  why and how to request private detail. A follow-up such as "show all
  entities in MQTT" requests that group's full detail in the ACTIVE privacy
  mode — a detail or ID follow-up never switches modes; private identities
  take an explicit request naming the mode.

## Relay Contract

File-based reads: `ha-nova relay core --method GET --path <path> --out <result-file>` for `/api/config`, `/api/components`, `/api/states`.
- `ha-nova relay ws --data-file <payload-file> --out <result-file>`
- `ha-nova relay jq --file <result-file> --jq-file <filter-file>`

For WS payloads:
- repairs: `{"type":"repairs/list_issues"}`
- integration entries: `{"type":"config_entries/get"}`
- entity registry: `{"type":"config/entity_registry/list"}`
- device registry: `{"type":"config/device_registry/list"}`
- area registry (optional, only when human-readable area names are shown): `{"type":"config/area_registry/list"}`
- system health: `{"message":{"type":"system_health/info"},"collect_events":{"until_type":"finish","max_events":100,"timeout_ms":10000}}`

`system_health/info` returns `data.events`: `initial`, zero or more `update`, then `finish`. The Relay enforces `until_type`, `max_events`, and `timeout_ms`. On `data: null`, `UNSUPPORTED_WS_TYPE`, `VALIDATION_ERROR`, or unsupported events, continue; say system-health details need a relay at the enforced floor.

## Data Shapes

Envelope parsing follows `../ha-nova/relay-api.md` → Standard Envelope. Normalize each saved relay result separately before summarizing:
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
- device registry: `id`, `name_by_user`, `name`, `area_id`, `parent_device_id`, `config_entry_id` — effective area is the row's own `area_id`, else the parent's (`../ha-nova/membership-resolution.md` → Device rows)
- config entries: existing state/domain fields plus `title`, sanitized before display

Normalize each source separately: REST, WS lists, repairs, and system-health events have different envelopes. Combine only validated summaries.

Before accepting a source, require `ok:true`, a 2xx REST `data.status`, and valid types for every required row field. If a shape differs, mark that source unavailable and continue. Do not show parser or `jq` errors; mention only the affected source in coverage.

Use `--jq-file` for non-trivial filters; prefer field equality and type checks over shell-escaped regex.

System Health event payloads are mixed-shape. For events from `.data.events[]`:
- read the event kind from `.type`
- before reading `.data.<domain>.info` (initial events nest per integration domain) or any nested field, require `(.data | type) == "object"`
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

Each block: localized label, assessment BEFORE the table, optional action after.
Tables compare facts; explanations, uncertainty, and instructions stay outside cells.
A shown ID shares its name's cell. Single findings or wide rows use stacked records.
Severity never depends on color alone:
pair `🔴`/`🟠`/`🟡` with a plain-language title or status ("act", "check",
"information").

## Behavior

- **Attribution honesty.** Without an exact join, state attribution unavailable. In `Private` mode Integrations may show a sanitized config-entry title; `Shareable`/`Aggregate` never show title/account identity.
- **Overall separates availability from findings.** Source availability is separate
  from actionable findings; neither a running core nor entity counts establish health.
  Overall stays `limited` for missing required sources; `attention` only for
  an active repair, an attention-state integration, an explicit system-health
  failure, or a structured low battery/SOC finding. Unexplained availability
  findings get their own prominent review block but do not change the enum.
  Non-actionable context alone never creates attention.
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
  (3) explicit replaceable evidence — a battery-type/battery-replaced style
  typed attribute or a device explicitly modeled as battery-powered →
  replaceable device battery; (4) otherwise — including bare
  `device_class: battery`, which indicates a level, not replaceability →
  unclassified low percentage. Structured evidence only —
  never from an entity name. Render the four classes separately;
  do not imply a battery replacement unless the entity is clearly a device battery — never recommend replacement for a phone/watch; never call vehicle SOC 0% a Home Assistant failure without connection context.
- **System and coverage stay compact and honest.** System health: failed object-shaped `update` events first, then max 3 object-shaped `initial.data` highlights; ignore scalars and `finish`. System: HA version/mode,
  explicit system-health failures, storage/DB signals
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

### Ignore a Repair

Resolve `domain` and `issue_id` from `repairs/list_issues`. One WS item `{"type":"repairs/ignore_issue","domain":"<d>","issue_id":"<id>","ignore":true}` through `ha-nova plan raw --file <requests.json>`, then `ha-nova apply` per `next_step`; `false` shows it again. Ignoring hides, never fixes. Verify by re-reading its `ignored` flag.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked or names clash, no ids, "entity", modes, Relay/HA URLs, snapshots, raw JSON. Use the Preview Card (`../ha-nova/output-cards.md`).
Report modes stay visible. Every truncation follows → Progressive Detail:
total/shown/omitted counts and a precise request for the full set — never a
bare "N more".

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

Keep Home Assistant state values such as `unavailable`, `unknown`, `setup_error`, `setup_retry`, and `not_loaded` literal when they are evidence, with a short localized explanation when helpful; a shown entity ID stays literal. Do not dump raw JSON, full logs, or full unrequested inventories —
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

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing speculative.
- Never call repair fix flows or delete issue commands.
- Never restart/reload Home Assistant from this skill.
- Never call update, backup, or service actions.
