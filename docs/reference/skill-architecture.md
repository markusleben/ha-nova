# HA NOVA Skill Architecture

## Overview

HA NOVA uses a flat skill layout with one context skill and 29 independent sub-skills under `skills/`.

The repo skill tree is the single source of truth. Client installers adapt that same tree to each client's packaging rules:
- Claude: plugin marketplace payload
- Codex / OpenCode: nested skill tree
- Google Antigravity: flat copied skill directories
- Hermes: namespaced nested copied skill tree with directory names aligned to installed skill IDs

Installed skill tree:
```
skills/
  ha-nova/SKILL.md              (context skill — stable top-level entrypoint)
  ha-nova/session-bootstrap.md  (shared first-use update/census contract)
  ha-nova/relay-api.md          (reference doc)
  ha-nova/best-practices.md     (reference doc)
  ha-nova/payload-schemas.md    (reference doc)
  ha-nova/helper-schemas.md     (reference doc — helper type payloads)
  ha-nova/output-rules.md       (reference doc — shared user-facing output rules)
  ha-nova/config-body-filter.jq (shared jq asset — canonical REST config-body extractor)
  ha-nova/bulk-patterns.md      (reference doc — bulk selectors, workset, aggregate audit rules)
  ha-nova/template-guidelines.md (reference doc — when to use templates vs native primitives)
  ha-nova/safe-refactoring.md   (reference doc — rename, delete, orphan cleanup workflows)
  ha-nova/automation-patterns.md (reference doc — native HA constructs vs templates)
  ha-nova/write-safety.md       (reference doc — pre-write diff + durable update-revert; SSOT for write/ + helper/)
  ha-nova/batch-safety.md       (reference doc — scoped batch manifest for destructive multi-target operations)
  ha-nova/grouped-change-set.md (reference doc — one confirmation for a fully previewed non-destructive change set)
  ha-nova/config-snapshots.md   (reference doc — targeted config-snapshot capture/restore on the relay blob store)
  ha-nova/input-capability-preflight.md (reference doc — verify input-device gestures before planning a remap)
  ha-nova/consumer-discovery-preflight.md (reference doc — find an input's consumers before repurposing it)
  ha-nova/agents/               (agent templates: resolve, apply)
  read/SKILL.md                         (ha-nova:read — automation/script list/get/trace)
  write/SKILL.md                        (ha-nova:write — automation/script create/update/delete)
  helper/SKILL.md                       (ha-nova:helper — helper CRUD: list/read/create/update/delete)
  integration-setup/SKILL.md            (ha-nova:integration-setup — add integrations and continue pending reauth flows)
  dashboard/SKILL.md                    (ha-nova:dashboard — storage dashboards, Lovelace resources, card operations)
  scene/SKILL.md                        (ha-nova:scene — storage-scene list/read/create/update/delete)
  organize/SKILL.md                     (ha-nova:organize — areas/floors/labels/categories/entity+device metadata)
  history/SKILL.md                      (ha-nova:history — bounded history/logbook/statistics reads)
  health/SKILL.md                       (ha-nova:health — read-only home status, repairs, system health)
  calendar/SKILL.md                     (ha-nova:calendar — bounded calendar reads and single-event writes)
  todo/SKILL.md                         (ha-nova:todo — to-do list items + Local To-do lifecycle)
  backup/SKILL.md                       (ha-nova:backup — backup status/create/inspect/delete; restore stays in HA UI)
  updates/SKILL.md                      (ha-nova:updates — pending updates, release notes, feature-gated installs)
  energy/SKILL.md                       (ha-nova:energy — energy analysis + gated source/device config)
  energy/energy-reference.md            (reference doc — prefs schemas, KPI formulas, analysis recipes)
  maintenance/SKILL.md                  (ha-nova:maintenance — statistics repair, recorder purge, registry cleanup)
  maintenance/maintenance-reference.md  (reference doc — issue matrix, repair payloads, orphan gates)
  review/SKILL.md                       (ha-nova:review — config quality review + collision scan)
  entity-discovery/SKILL.md             (ha-nova:entity-discovery — entity lookup)
  service-call/SKILL.md                 (ha-nova:service-call — services, events/webhooks, alarm/lock runtime control)
  fallback/SKILL.md                     (ha-nova:fallback — mandatory fallback for relay-ready features)
  onboarding/SKILL.md                   (ha-nova:onboarding — onboarding + diagnostics)
  diagnose/SKILL.md                     (ha-nova:diagnose — failure root-cause: traces, logs, bounded windows)
  media/SKILL.md                        (ha-nova:media — media player control, browsing, grouping, TTS announce)
  notify/SKILL.md                       (ha-nova:notify — notify targets, mobile-app sends, persistent notifications)
  camera/SKILL.md                       (ha-nova:camera — snapshots the agent can look at, stream URLs, record)
  mqtt/SKILL.md                         (ha-nova:mqtt — bounded topic windows, discovery/debug info, guarded publish)
  assist/SKILL.md                       (ha-nova:assist — utterance testing, pipelines, voice exposure, engine inventory)
  admin/SKILL.md                        (ha-nova:admin — persons, zones, tags, user accounts)
  yaml-config/SKILL.md                  (ha-nova:yaml-config — YAML-only configuration through opt-in file access)
  external-sources/SKILL.md             (ha-nova:external-sources — read-only queries against InfluxDB and friends)
```

## Discovery Model

The canonical skill entrypoints remain `skills/*/SKILL.md`.

- Claude loads HA NOVA through the installed plugin marketplace payload.
- Codex and OpenCode load the nested `ha-nova` skill tree directly.
- Google Antigravity receives flat copied skill directories because it only supports one skill level.
- Antigravity sub-skills are installed with namespaced identifiers such as `ha-nova-entity-discovery` so the flat folder names and activation names stay aligned.
- Hermes receives a nested copied `ha-nova` bundle under `~/.hermes/skills/ha-nova`, with namespaced sub-skill directories such as `ha-nova-entity-discovery/` whose directory names and frontmatter names stay identical.

The context skill (`ha-nova`) stays the stable entrypoint; sub-skills remain independently discoverable by description and naming.

## Repo-local Hook Note

`hooks/session-start` still exists as a repo-local development helper, but it is **not** the production installation model for Claude. Production Claude installs use a versioned local release snapshot under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`. The flat local staged marketplace layout stays a repo-local development tool only.

## Agent vs Inline Decision Rule

When building a new skill, decide execution model by these criteria:

**Use agents when ALL of these apply:**
- 5+ relay calls in a single operation
- Multi-step deterministic logic (resolve with fallback, write with normalization)
- Nested payload structures requiring comparison/normalization (e.g., trigger/triggers aliasing)
- Domain reload required after write

**Use inline when ANY of these apply:**
- 1-4 relay calls per operation
- Flat payloads (no nested triggers/conditions/actions)
- Direct user interaction needed between steps (preview → confirm → execute)
- No payload normalization quirks

Current mapping:

| Skill | Model | Why |
|-------|-------|-----|
| read | inline | 1-2 calls, direct output |
| write | **agents** | 5-7 calls, entity resolution fallback, singular/plural normalization, domain reload |
| helper | inline | response-driven relay flows, direct preview/confirm loop, no agent-only normalization requirement |
| integration-setup | inline | response-driven config flows with direct preview/confirm and HA UI handoff for secrets/external steps |
| dashboard | inline | read → merge → preview → full-save → readback verify, all user-facing |
| scene | inline | 2-4 calls, flat entities payload, read → merge → preview → full-save → readback verify |
| organize | inline | field-level registry mutations with direct preview/readback |
| history | inline | read-only bounded timeline lookups |
| health | inline | read-only status aggregation, best-effort diagnostics |
| calendar | inline | bounded REST reads plus feature-gated service/WS event writes |
| todo | inline | service-based item CRUD with feature gate, single-step list flow |
| backup | inline | WS status/generate/delete with initiation-vs-completion polling |
| updates | inline | entity-based overview, feature-gated install with entity-poll verification |
| energy | inline | statistics-based analysis, prefs read → merge → preview → full-save → validate verify |
| maintenance | inline | grouped issue triage, code-gated destructive repairs with per-item verification |
| review | inline | analysis is client-side, relay calls are reads only |
| entity-discovery | inline | 1-2 calls, search + return |
| service-call | inline | direct preview/confirm; any listener scan stays read-only and user-facing |
| fallback | inline | research + web search + experimental relay calls (write-guarded) |
| onboarding | inline | diagnostics only |
| diagnose | inline | evidence gathering + reasoning, one gated debug escalation |
| media | inline | feature-gated service calls with state read-back |
| notify | inline | target discovery + one previewed send |
| camera | inline | one binary fetch, feature-gated services |
| mqtt | inline | one bounded window or one guarded publish |
| assist | inline | read/test flows plus full-object pipeline writes |
| admin | inline | registry-style writes with impact advisory and hard user guards |
| yaml-config | inline | read -> File-Change Preview -> write -> check_config -> reload -> verify |
| external-sources | inline | read-only, and the query does not go through the relay |

**Rule of thumb:** If a `service-call` could do it, it's inline. If it needs what `write` needs (resolve + normalize + reload), use agents.

## Write Architecture

`ha-nova:write` uses a deterministic four-phase flow:

1. Resolve (Agent)
- load env
- fetch states
- resolve entities and target id
- check existence + current config
- evaluate best-practice cache status

2. Preview + Decide (Main Thread)
- build final payload
- lead the preview with a terminal-friendly Changes slot; full YAML only on `show yaml` (see `ha-nova/write-safety.md` → Pre-Write Diff)
- update: pre-write impact advisory via `search/related` at preview time (review/ Step 2)
- show compact preview blocks
- ask one decision question only if ambiguous
- confirmation tier:
  - create/update: natural confirmation bound to active preview
  - delete: typed confirmation code `confirm:<token>`
  - pre-preview wording such as "implement the plan", "do it", or "go ahead" authorizes draft/check/preview work only; if the previewed payload, target, or manifest changes, confirmation expires

3. Apply + Verify (Agent)
- write via relay `/core`
- read-back verification
- normalized compare (`trigger(s)`, `condition(s)`, `action(s)`)
- structured error result on partial or failed verification

4. Review (inline, do NOT invoke `ha-nova:review` as separate skill)
- post-write config quality checks, collision scan, conflict analysis
- findings are advisory (write already succeeded)
- update: capture a durable revert snapshot and offer `revert` (see `ha-nova/write-safety.md` → Update-Revert); creates clean up through the normal delete flow; deletes have no HA NOVA revert

Fallback:
- if agent dispatch unavailable, execute same phases inline serially.

## Config Snapshot Architecture

The `ha-nova` context skill owns generic config snapshot listing and deletion
through `skills/ha-nova/config-snapshots.md`; restore delegates to the skill
that owns the captured Home Assistant item because restore fidelity and write
verification are family-specific. Exact multi-file deletion is the sole
context-owned destructive batch family: `config-snapshots`, capped at 20,
sequential, fail-fast, and verified per blob. The Relay remains an opaque blob
store and gains no Home Assistant business logic.

## Read Architecture

`ha-nova:read` is intentionally direct/low-overhead:
- no subagent dispatch for routine reads
- `/ws config/entity_registry/list_for_display` for list operations
- `/core` config reads for single-item get operations
- one blocking question only if target ambiguity remains
- multi-target scope is inventory-only; use the shared bulk selector rules from `skills/ha-nova/bulk-patterns.md`
- room/area bulk resolution is area-first: resolve the area, then use `search/related` on the area response object instead of assuming compact-registry `ai` is populated
- do not dump full YAML for many targets in one response

## Dashboard Architecture

`ha-nova:dashboard` owns safe storage-dashboard work:
- list dashboards
- read one dashboard config
- list Lovelace resources
- inspect dashboard structure (views, cards, badges, header cards)
- create a storage dashboard shell
- update dashboard metadata
- create/update/delete Lovelace resources
- add/update/move/delete cards inside existing views
- delete a storage dashboard

Write contract:
- resolve `dashboard_id`, `url_path`, and `mode` through `lovelace/dashboards/list`
- only `mode=storage` may be created/updated/deleted here
- metadata changes go through `lovelace/dashboards/create|update|delete`
- metadata update sends `dashboard_id` plus only changed metadata fields: `title`, `icon`, `show_in_sidebar`, `require_admin`
- `dashboard_id` is the mutation identifier for `update|delete`; `url_path` is the config identifier for `lovelace/config|save`
- resource CRUD goes through `lovelace/resources|create|update|delete`
- content edits always read the current full config first
- resolve the exact target card/badge/header by view + title/entity/type/position before changing it
- merge in memory
- preview the merged result
- save through `lovelace/config/save` with the full config only
- read back and verify the intended change plus unrelated-view survival

Still excluded:
- broad raw Lovelace editing without a concrete requested change
- view create/delete/reorder
- non-storage dashboard writes/deletes
- freeform new custom-card creation
- energy dashboard preferences

## Organize Architecture

`ha-nova:organize` owns metadata-first Home Assistant organization:
- areas / floors / labels / categories CRUD
- entity registry metadata updates
- entity category assignment/removal by scope
- device registry metadata updates

Mutation rules:
- exact target resolution first
- every `config/category_registry/*` call includes the exact `scope`
- rich metadata stays first-class:
  - areas: `floor_id`, `icon`, `picture`, `aliases`
  - floors: `level`, `icon`, `aliases`
  - labels: `color`, `icon`, `description`
  - categories: `icon`
- entity/device label updates may replace, add, remove, or clear labels
- field-level preview before write
- destructive area/floor/label/category delete requires impact preview + typed confirmation code
- read back the changed registry fields after every mutation

Still excluded:
- entity removal
- device config-entry detachment
- device category assignment
- zones / persons / tags

## History Architecture

`ha-nova:history` is a bounded read-only timeline skill:
- entity history via `/api/history/period`
- human-readable timeline via `/api/logbook`
- long-term trends via `recorder/statistics_during_period`
- summary-first answers

Rules:
- always use a bounded time window
- prefer concise summaries over raw dumps
- reject or narrow oversized requests

## Health Architecture

`ha-nova:health` is a read-only home-status skill:
- repairs/deprecation issues through `repairs/list_issues`
- integration setup/load status through `config_entries/get`
- system health through generic bounded WS event collection for `system_health/info`
- config/components through `/api/config` and `/api/components`
- unavailable/unknown and low-battery summaries through `/api/states`

Rules:
- no repair/fix/ignore actions
- no restart/reload/service calls
- check `ha-nova relay health` and skip `system_health/info` when the relay is below the enforced floor (`min_relay_version`)
- summarize by source and bind conclusions to evidence
- keep Home Status table-first and cause-oriented: overall state + visible Detail×Privacy modes (default `Explained + Private`), prioritized actions, sanitized integration reasons (#440)
- label unavailable/unknown totals as entity-state counts, never device/problem counts
- when availability states exist, best-effort join full entity/device registries and config entries for restored/current, integration, config-entry-state, and device-attribution coverage
- use one finding ledger across Entities and Integrations; a failed entry owns its joined impact once under Integrations ("associated states" wording)
- assign every raw availability row to exactly one of six categories (sum equals the raw total); render groups under the Explained budget of 50 entity-detail rows (1-10 full detail, 11-50 five examples, >50 summarized) — in `Private` mode exact entity IDs, friendly names, and sanitized config-entry titles are legitimate output, while `Shareable`/`Aggregate` hide identity; secrets, addresses, and hosts stay out in every mode
- report known device-registry records plus entity-state row coverage, never an inferred exact device total
- aggregate and cap privacy-safe device-subcluster sizes independently of integration attribution; device IDs remain hidden tie-breakers only
- treat setup/unload progress as context, and only the explicit config-entry failure set as attention
- when availability rows exist, missing entity/device registry sources make coverage limited and are named separately
- treat availability classification as context only; it never changes overall status without an existing attention source
- deprioritize noisy/stateless domains (`button`, `event`, `scene`, `stt`) for attention while retaining their contextualized counts
- localize output slot headings and labels; keep HA state values literal when used as evidence

## Calendar Architecture

`ha-nova:calendar` owns bounded calendar reads and single-event writes:
- list calendars through `/api/calendars`
- read events through `/api/calendars/{entity_id}?start=<timestamp>&end=<timestamp>`
- create through `calendar.create_event`; update/delete through WS `calendar/event/update|delete`
- capability gates use `supported_features` bits 1/2/4 before create/delete/update
- update/delete identity is `(uid, recurrence_id)`; recurring scope is one occurrence (`""`) or this-and-future (`THISANDFUTURE`)
- update sends a full merged event object, never a partial patch

Rules:
- default to the next 7 days
- always use a bounded event window
- resolve ambiguous calendar names before querying events
- create/update use natural bound confirmation; delete uses the typed confirmation code
- drift-check immediately before the write; verify through bounded REST read-back and never auto-retry a write
- recurring creation stays in the Home Assistant UI because `calendar.create_event` has no recurrence field

## Integration Setup Architecture

`ha-nova:integration-setup` owns UI-configurable integration add and pending reauthentication flows:
- add starts through REST `POST /api/config/config_entries/flow` after resolving an exact handler from `/api/config/config_entries/flow_handlers`
- reauthentication continues an existing `context.source == "reauth"` flow discovered through WS `config_entries/flow/progress`; it never synthesizes a reauth flow
- menu/form steps use only the live response schema and require a preview-bound confirmation before each submit
- credential-bearing, external/OAuth, or progress add steps started through the Relay are canceled and restarted in the Home Assistant UI; user-started flows are omitted from `config_entries/flow/progress`, and the Relay cannot supply the frontend-origin header
- credential-bearing, external/OAuth, or progress steps on pre-existing reauth flows hand off to the matching Home Assistant UI card; secrets never enter chat and the reauth flow stays preserved
- config-entry existence/state is primary verification evidence; linked devices/entities are secondary
- agent-created canceled add flows are deleted; Home Assistant-created reauth flows are preserved

## Service Call Architecture

`ha-nova:service-call` owns ordinary HA service calls plus four guarded runtime families:
- custom event firing through `POST /api/events/{event_type}`
- known JSON-webhook triggering through `POST /api/webhook/{webhook_id}`
- alarm-panel runtime services with feature bits 1/2/4/8/16/32
- lock runtime services, with `lock.open` gated by feature bit 1

Event/webhook rules:
- resolve an exact event type or an exact automation with a static webhook ID; never probe either endpoint
- scan readable automation configs for every matching current/legacy trigger and classify literal event-data filters; compare with `GET /api/events` for unclassified event listeners
- use WS `webhook/list` with `--out` into client-private scratch storage to check registration, POST support, and `local_only`; the full response never reaches stdout, user output, or persistent storage
- preview payload fields, known listeners, unknown-listener limits, and inherited action risk before bound confirmation
- event success proves bus acceptance only; webhook HTTP 200 is deliberately opaque and does not prove registration, locality, or handler success
- verify known automation runs against pre-call `last_triggered`/trace baselines; never auto-retry either runtime action

Alarm/lock rules:
- inspect the exact state and `supported_features` immediately before preview
- codes/PINs never enter chat or Relay payloads; alarm arming hands off whenever `code_arm_required` is true, while other code-bearing actions use `code_format`
- unlocking/opening a lock and disarming an alarm use the typed high-consequence confirmation
- security-state verification is transition-aware and never auto-retries

## Review Architecture

`ha-nova:review` is a self-contained read-only reviewer:
- Config quality: safety (S-01..S-03), reliability (R-01..R-28), performance (P-01..P-05), style (M-01..M-05; M-04 retired, moved to R-20), script-specific (F-01..F-09), helper-specific (H-01..H-15), scene (SC-01..SC-07), dashboard (D-01..D-07), cross-item (HX-01..HX-05), YAML sensors (TS-01..TS-07)
- `R-25` is pasted-YAML only (legacy template platform syntax, removed in HA 2026.6); `M-05` is a modernize advisory for pre-2024.10 automation keys
- Collision scan: `search/related` on top 3 target entities
- Conflict analysis: 3-step test (polarity → temporal → guard conditions)
- Explorative questions: standalone automation/script reviews add a gated edge-case pass for complex behavior
- Suggestion synthesis: standalone single-target review splits uncertainty into **Questions to consider** and keeps only confident recommendations in **Suggestions**
- Remove/simplify ideas pass a design-intent gate before they can become confident suggestions
- Confident suggestions are ranked by intervention depth: fix existing → simplify existing → extend existing → add new
- User-facing review text never shows internal rule codes; findings use a short descriptive title plus `Why` and `Fix`, and clean states stay generic
- `R-17` is intra-config only; collision scan stays cross-item conflict work, not overwrite/rebound detection
- `R-18` is same-mapping only; it checks storage-sensitive sibling-variable references inside one `variables:` block, not cross-scope references
- `R-19` is branch-structure reachability only; it covers direct `trigger.id` checks in a terminal bare `else` after entity-state `if` / `elif` guards, without intent inference
- `R-23` catches boolean-like templates compared to string boolean literals such as `"True"` / `"False"`
- `R-24` is a low-severity capacity-source advisory when a capacity-like variable reads `available_energy`
- Known safe/problem pattern matching from `skills/review/checks.md`
- resolved targets `== 1`: stable 8-section single-target output (`Review target`, `Findings`, `Collision check`, `Conflicts`, `Questions to consider`, `Suggestions`, `Summary`, `Instant help`)
- resolved targets `> 1`: switch to aggregate multi-target mode automatically, materialize and trim the current workset before any per-item reads, audit max 5 items in stable order, aggregate findings by pattern, and report `matched / audited / remaining`
- bulk mode disables Quick-Fix; it stays strictly read-only
- post-write review stays compact and keeps the advisory-only `Findings` / `Collision check` / `Advisory` structure

## Helper Architecture

`ha-nova:helper` now has two explicit helper families:

- **Storage-based family**
  - Types: `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
  - Transport: WS (`{type}/create`, `{type}/update`, `{type}/delete`)
  - Identity: `{type}_id` from `{type}/list`, not entity_id
  - Write verify: `{type}/list`
  - Review: H-01..H-11 helper-specific checks + collision scan via `search/related`
  - No domain reload needed

- **Config-entry family**
  - Types: `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`
  - Read/list: WS `config_entries/get` + WS `config/entity_registry/list`
  - Readback: current editable options snapshot when `supports_options: true`; metadata-only fallback otherwise
  - Mutation transport: relay `/core`
  - Create: config-entry flow loop, including menu/form step iteration
  - Update: options-flow loop with required-field carry-forward from the current editable options snapshot
  - Identity: `entry_id` is canonical; linked `entity_id` values are derived only
  - Write verify: config-entry layer first for identity/existence, reopened editable options snapshot for field-level update verification
  - Review: minimal config-entry post-write contract (plus H-12/H-13/H-15 where readable), not H-01..H-11
  - `group` remains menu-driven; end-to-end support is proven for the `sensor` subtype, and other subtypes must stay anchored to the live step schema instead of guessed fields

Still excluded from `ha-nova:helper`:
- `trend`
- `random`
- `filter`
- `generic_thermostat`
- `switch_as_x`
- `generic_hygrostat`

## Diagnose Architecture

`ha-nova:diagnose` is the failure-root-cause skill (read-only apart from one gated mutation):
- traces first (`ha-nova trace latest <entity_id> --json`, plus `trace list` / `trace get`) for automation/script symptoms
- WS `system_log/list` as the primary log source; `/api/error_log` only where the log file exists (404 on HA OS/Supervised since 2025.11)
- bounded logbook/history windows around the incident (default ±30 min)
- `POST /api/template` to probe suspect conditions against live state
- WS `diagnostics/list` + `/api/diagnostics/config_entry/<entry_id>` when an integration is the suspect

Rules:
- starts from a concrete symptom; current-status questions go to `ha-nova:health`, plain timelines to `ha-nova:history`
- recency honesty: error/system logs only reach back to the last Core restart
- the single mutation is a temporary `logger.set_level` debug escalation — preview, confirm, and always schedule the reset in the same interaction
- conclusions bind to evidence (trace step, log line, state sequence); otherwise present ranked hypotheses plus the deciding probe
- fixes hand off to `ha-nova:write` / `ha-nova:helper` / `ha-nova:service-call`

## Media Architecture

`ha-nova:media` owns media player work:
- `supported_features` bitmask gate BEFORE any action (browse 131072, grouping 524288, stop 4096, select source 2048, ...) — the full MediaPlayerEntityFeature table lives in the skill
- transport/volume/source via `media_player.*` services; verify by re-reading the entity state
- browsing: WS `media_player/browse_media` (player library) and `media_source/browse_media` + `media_source/resolve_media` (HA media sources); `media_content_type` and `media_content_id` are a pair
- grouping via `media_player.join|unjoin`, verified through `group_members`
- TTS announce via `tts.speak` (+ WS `tts/engine/list`), with the legacy `tts.*_say` fallback named explicitly

Rules: never invent a `media_content_id`; volume jumps and announcements are disruptive side effects and need an explicit confirmation.

## Notify Architecture

`ha-nova:notify` owns notification delivery:
- two surfaces, explicitly disambiguated: notify ENTITIES (`notify.send_message`) for plain messages and UI groups; legacy `notify.mobile_app_<device>` services for everything with a `data` payload (actionable buttons, tag replace/clear, sticky, channel/importance, url, image)
- discovery from `/api/services` (notify domain) plus the entity registry — never guess a device name
- persistent notifications via WS `persistent_notification/get` + `persistent_notification.create|dismiss`
- honesty: a 200 means Home Assistant accepted the send, never that the phone displayed it
- actionable callbacks are out of reach (they arrive as `mobile_app_notification_action` events, which need a subscription): hand off to `ha-nova:write` for the automation pattern

## Camera Architecture

`ha-nova:camera` owns camera access and is the first consumer of the relay's binary path (guaranteed at the enforced relay floor, `version.json` → `min_relay_version`):
- frames via `GET /api/camera_proxy/<entity_id>` with `--out-binary` ONLY (`--out`/`--jq` would write the JSON envelope instead of an image)
- stream URL via WS `camera/stream` (needs the STREAM feature bit)
- `camera.snapshot` / `camera.record` write on the HA host and need `allowlist_external_dirs` — previewed and confirmed
- frames are private data: client-private scratch only, never the workspace, and no claim about image content beyond what the frame shows

## MQTT Architecture

`ha-nova:mqtt` owns MQTT work and is the first consumer of envelope window mode (guaranteed at the enforced relay floor):
- listening is a bounded WINDOW (`mqtt/subscribe` inside `collect_events` with `on_limit: "return"`), never a stream; the relay unsubscribes when it closes
- an empty window is a real answer ("nothing published"), reported as one — an `UPSTREAM_WS_TIMEOUT` (subscription never established) is a different finding
- discovery/debug via WS `mqtt/device/debug_info` (device_id from the registry, never guessed)
- publishing is guarded: retained messages and command/`set` topics take the typed confirmation code, because they persist on the broker or actuate hardware

## Assist Architecture

`ha-nova:assist` owns Home Assistant's voice assistant:
- utterance testing through `POST /api/conversation/process` — the flagship capability, and a LIVE command: it executes what it understands, so anything state-changing is previewed and confirmed like a service call
- pipelines via WS `assist_pipeline/pipeline/*` (update resends every settings field addressed by `pipeline_id`; delete is code-gated because a satellite may depend on it)
- voice exposure via WS `homeassistant/expose_entity[/list]`
- engine inventories: `tts/engine/list`, `stt/engine/list`, `conversation/agent/list`, `wake_word/info`
- `assist_pipeline/run` stays out of reach: it is an audio subscription, not request/response

## Admin Architecture

`ha-nova:admin` owns persons, zones, tags, and user accounts:
- persons/zones/tags via WS `person/*`, `zone/*`, `tag/*` (updates resend every mutable field, addressed by the `*_id` key)
- zones are presence infrastructure: every zone change runs `search/related` first and names the automations that depend on it, in the preview
- users via WS `config/auth/*` — the strictest writes in HA NOVA: owner, system-generated, and the relay's own account are refused outright; everything else needs the typed confirmation code
- passwords and auth providers stay in the Home Assistant UI on purpose

## YAML Config Architecture

`ha-nova:yaml-config` owns configuration that has no API, through the relay's opt-in file access. The current release enforces Relay >= 0.8.0:
- read -> File-Change Preview (effect sentences + the changed section only, never a unified diff) -> confirm -> `write_file` (automatic `.bak`) -> `POST /api/config/core/check_config` -> targeted reload -> verify the entity in `/api/states`
- an invalid `check_config` restores the `.bak` BEFORE reporting, and never reloads
- whole-file replacement: never write a file that was not read first
- when `file_access` is off (the default), the skill degrades to producing the exact YAML block plus the two commands to apply it — a fully supported path, not a failure
- HA NOVA's own additions live in `/config/ha_nova/`, included once from `configuration.yaml`

## External Sources Architecture

`ha-nova:external-sources` covers data Home Assistant writes out but cannot read back (InfluxDB is the case that matters):
- the relay is NOT the transport: it cannot reach other hosts by design, so the query goes out with the client's own HTTP tooling
- credentials come from the user's environment (`HANOVA_INFLUXDB_*`), never from chat
- read-only by contract: no INSERT/DELETE/DDL is ever constructed
- the premise is corrected first: the `influxdb` integration is write-only from Home Assistant's side
- the durable fix for a recurring query is an `influxdb` sensor via `ha-nova:yaml-config`

## Fallback Architecture

`ha-nova:fallback` is the mandatory safety fallback for HA features without a dedicated skill:
- Covers: blueprints, device config-entry detach, unsupported config-entry helper families
- Three-tier capability map: Covered (redirect to existing skill), Relay-Ready (experimental relay calls), External (web search)
- All inline, no agents — research + web search + experimental relay calls
- Safety: all experimental relay calls follow Write Safety by Endpoint Type guardrails (full-overwrite, field-level replace, merge, delete)

## Dev Installer Contract

The remaining shell-adjacent scripts are a development/compatibility surface, not a second product lifecycle.

Active dev helpers:
- `scripts/onboarding/install-local-skills.sh`
- `scripts/onboarding/bin/ha-nova`
- `scripts/dev-sync.sh`

Rules for this helper family:
- no end-user installer contract
- no product lifecycle logic beyond runtime discovery and forwarding
- no Git/network self-update flow
- keep behavior narrow: local skill refresh, local cache refresh, runtime forwarding, or pre-Go compatibility only

`scripts/onboarding/install-local-skills.sh` is the main repo-local installer helper.
`npm run dev:sync` / `scripts/dev-sync.sh` is the canonical repo-local refresh helper once a local install already exists.

It handles repo-local skill refreshes for development and validation:
- source skill tree: `skills/` (repo-local, flat layout)
- client-specific install strategies:
  - **Claude Code:** for repo-local development only, stages a local marketplace root under `~/.config/ha-nova/claude-marketplace`, registers it with `claude plugin marketplace add`, then installs/reinstalls `ha-nova@ha-nova`
  - **Codex CLI:** symlink on Unix, copy fallback on Windows at `~/.agents/skills/ha-nova`
  - **OpenCode:** symlink on Unix, copy fallback on Windows at `~/.config/opencode/skills/ha-nova`
  - **Google Antigravity:** Flat copy `~/.gemini/config/skills/ha-nova/SKILL.md` plus `~/.gemini/config/skills/ha-nova-*/SKILL.md` sub-skills (1-level limit), with namespaced sub-skill names matching those folder names
  - **Hermes Agent:** Namespaced nested copy under `~/.hermes/skills/ha-nova/ha-nova-*`, with sub-skill directory names and frontmatter names both using the same `ha-nova-*` identifier
- cleans up legacy flat skill directories (old `ha-nova-*` prefixed dirs)
- supports targets: `codex`, `claude`, `opencode`, `antigravity`, `hermes`, `all`; `gemini` is accepted as a legacy alias for Antigravity

The other helper roles are intentionally smaller:
- `scripts/onboarding/bin/ha-nova` forwards repo-local setup/update/check-update calls into the Go runtime
- repo/dev compatibility wrappers such as `~/.config/ha-nova/version-check` are generated from `scripts/onboarding/install-local-skills.sh` or `scripts/dev-sync.sh`, not tracked as standalone repo scripts
- `scripts/dev-sync.sh` refreshes detected local client installs and Claude cache state during development

The end-user installer contract is:
- `install.sh` / `install.ps1` bootstrap the runtime, handle legacy gating, and hand off into `ha-nova setup`
- `ha-nova setup` owns product setup, migration, and client attachment
- bundled Claude installs attach to a versioned local release snapshot under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`; the flat `~/.config/ha-nova/claude-marketplace` root stays repo/dev-only

## Skill Section Template (v2)

Canonical structure for all sub-skills, enforced by `tests/skills/skill-template-contract.test.ts`. Follow this when creating or auditing skills.

**Canonical H2 order** (domain-specific sections may appear in the `[domain]` slots; the canonical sections must appear in this relative order):

```
Scope → Bootstrap (once per session) → Relay Contract → [domain] → Flow → [domain]
  → Error Handling (optional, always directly before Output Format)
  → Output Format → Safety → Guardrails (optional) → References (optional)
```

**Required for ALL sub-skills:**
- **Scope** — what this skill does + inverse scope (what it does NOT do, which skill to use instead)
- **Bootstrap (once per session)** — exact heading; shared
  `../ha-nova/session-bootstrap.md` pointer, relay CLI verification, and
  onboarding fallback. The shared contract synchronously checks HA NOVA and
  the selected server's Relay update state before the first HA task, then
  keeps any callouts post-task and once-per-session.
- **Relay Contract** — the file-based `ha-nova relay` command contract this skill uses
- **Flow** — step-by-step operations with relay commands
- **Output Format** — first line starts with ``Apply `skills/ha-nova/output-rules.md` `` ; then what the user receives
- **Safety** — risk mitigations, confirmation rules, relay-only constraint

**Required for behavior-config-persisting skills** (write, helper):
- **Post-Write Review** — mandatory inline review phase after every create/update/delete (a Flow phase, not a separate H2)
- **References** — links to schema docs, relay API, review checks

`integration-setup` persists config entries through Home Assistant-owned flows. It verifies the resulting config entry instead of applying automation/helper quality checks.

**Optional:**
- **Error Handling** — error classification + remediation (recommended for external calls); when present it sits directly before Output Format
- **Guardrails** — hard limits and constraints (e.g. "never use raw `get_states`")
- **Latency Policy** — when to optimize for speed

**Declared deviations** (the only allowed ones):
- `onboarding` — heading `## Bootstrap` (it repairs the relay; "once per session" would be wrong), applies the shared session bootstrap only when the CLI exists, and has no `Relay Contract` section (the diagnostics skill's whole body is remediation commands)
- `fallback` — heading `## Bootstrap (before Home Assistant tasks)`; the shared session bootstrap applies before Home Assistant work, while Roadmap/External guidance needs no Relay probe

**Forbidden heading variants** (normalized in 2026-07, must not return): `## Output Rules`, `## Safety Baseline` (sub-skills; the context skill keeps its own), `## Safety Guardrails`, `## Agent Flow`.

**Terminology rule:** prose says "App(s)"; `add-on` / `addon` may appear only inside inline code or fenced blocks as literal API identifiers (`include_all_addons`, `failed_addons`, `<addon_slug>`, ...) or as backticked search-query strings.

**Portability rule:** if a referenced shared file is unavailable in an install, do not guess its content — ask the user to re-run `ha-nova setup`. Exception: the existing `config-body-filter.jq` "recreate exactly" blocks (a one-line filter, self-recreation is strictly better).

**The context skill (`ha-nova/SKILL.md`) is exempt** from the sub-skill template: it is a router, not an operation skill. Its required anchors stay pinned by `tests/skills/ha-nova-contract.test.ts`.

### Safety Core (canonical text)

Every mutation-capable sub-skill opens its `## Safety` section with this block, byte-identical (linter-enforced; the linter extracts this fenced block as the SSOT). It carries the bootstrap-independent guarantees — preview binding, delete confirmation-code gating, and the fallback write gate — so a bare agent that never auto-loads the context skill still gets them:

```text
- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.
```

Read-only sub-skills open `## Safety` with this block instead:

```text
- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.
```

Skill-specific safety bullets follow the core block; bullets that merely restate a core line are removed, domain nuances (confirmation tiering, no-revert notes, session-cleanup rules) stay.

A skill may declare an explicit, named exception to a single core bullet directly below the core block — it must reference the core rule it narrows ("Declared exception to the core ... rule above") so a bare agent never sees two contradicting instructions. Current declared exceptions: `todo` item removes (`todo.remove_item`, `todo.remove_completed_items`) stay at natural preview confirmation while list deletion keeps the typed confirmation code; `integration-setup` may delete only an unfinished add flow that it started when a credential, external/OAuth, or progress step requires a UI restart.

## Post-Write Review Standard

Unified spec for post-write review. Both `write` and `helper` skills reference this.

After any mutation (automation, script, or helper):
1. Re-read written config via relay
2. Apply `skills/review/checks.md` → Application (family matrix + evidence boundaries):
   - **Automations:** S + R + P + M checks. If actions reference helpers, also H checks on those helpers.
   - **Scripts:** S + R + P + M + F checks. If actions reference helpers, also H checks.
   - **Helpers:**
     - storage-based family: H checks only
     - config-entry family: minimal config-entry review contract + collision scan on linked entities
   - Traverse all `variables:` blocks, not just the top-level block.
   - Storage-sensitive checks such as `R-18` may still be reported from the persisted read-back config even when the rest of the config matches the draft. Do not suppress them purely as pre-write dedup.
   - If persisted `R-18` remains after a write, add a manual next step to inspect traces after the next real run. Do not auto-trigger the config or auto-read traces from post-write review.
   - All other checks, including `R-19`, `R-23`, and `R-24`, follow normal pre-write/post-write dedup. The explicit persisted-repeat exception stays unique to `R-18`.
   Focus on 🔴 findings. Report 🟠🟡 findings as advisory.
3. Collision scan: `search/related` for top target entities, max 3 related configs (standalone review uses max 5)
4. Output format — apply `skills/ha-nova/output-rules.md`. Use semantic slots, not literal Markdown headings, in terminal-like clients. Report only what has substance; the scans still run, only their empty output is suppressed:
   - **Findings**: 🔴🟠🟡 findings with short descriptive titles plus `Why` / `Fix` — only when there are real issues.
   - **Collision check**: only when related items exist (list them + the conflict verdict).
   - **Advisory**: 🟠🟡 findings — only when non-empty.
   - Omit any section with nothing to report — never print an empty "none" bucket. When all are empty, collapse to one scope-honest confirmation line (write-safety → Verification Honesty; never a bare "verified").
   - Do not emit `Questions to consider`, `Suggestions`, or `Instant help` in post-write mode.

After the review, the `write` skill offers a structured test plan (feasibility, one recommended option, single bound confirmation) per `skills/ha-nova/test-run.md` (Phase 5: Test Offer) — offer only; execution follows `ha-nova:service-call` → Automation And Script Runtime Calls. The `helper` skill keeps the plain Verification Honesty offer.

## Adding a New Skill — Checklist

When creating a new skill under `skills/{name}/SKILL.md`:

1. Skill file follows Skill Section Template (see above)
2. `skills/ha-nova/SKILL.md` — add to Dispatch table + add disambiguation examples
3. `skills/{name}/SKILL.md` — reference `skills/ha-nova/output-rules.md` in its output section
4. `skills/ha-nova/SKILL.md` — add domain to Response Format if needed
5. `skills/review/SKILL.md` — keep entrypoint/flow aligned; add or update detailed rules in `skills/review/checks.md`
6. `docs/reference/skill-architecture.md` — add to skill tree + add Architecture section
7. `docs/reference/skill-architecture.md` — add to Agent vs Inline table
8. `scripts/onboarding/install-local-skills.sh` — verify dynamic discovery picks up new skill
9. `PROJECT.md` — update the active inventory; defer `README.md` feature claims to the release-prep PR
10. `version.json` — bump only in the release-prep PR that publishes the skill
11. For file-based clients, re-run `npm run dev:install:<client>-skill` and start a new session. Use `npm run dev:sync` only when you need the Claude cache sync helper or already have a repo-local install to refresh.

## Review Check Single Source of Truth

`skills/review/SKILL.md` is the stable review entrypoint for standalone reviews (workflow, output shape, collision/conflict analysis).
`skills/review/checks.md` is the authoritative, self-contained source for the detailed review catalog (S/R/P/M/F/H/SC/D/HX/TS) plus its `## Application` section (family matrix, evidence boundaries, live-helper evidence).
There is deliberately no review agent template: `write` and `helper` run their post-write review inline against `skills/review/checks.md` only — they no longer load the standalone review workflow.
When adding or modifying checks, update `skills/review/checks.md` first and keep `skills/review/SKILL.md` aligned as the facade/workflow file.

## Review Check Taxonomy

Review checks use the format `{CATEGORY}-{NN}`:
- `S` = Safety
- `R` = Reliability
- `P` = Performance
- `M` = Style
- `F` = Script-specific
- `H` = Helper-specific
- `SC` = Scene-specific
- `D` = Dashboard-specific
- `HX` = Cross-item
- `TS` = YAML-sensor-specific

`NN` is the running rule number inside that family. Severity is separate from the code.

Examples:
- `R-10` = the 10th reliability rule
- `H-09` = the 9th helper-specific rule

These codes are contributor-facing/internal only. User-facing output must use localized descriptive titles instead of exposing codes.

## Safety Baseline

Global safety expectations:
- no guessed ids
- preview before any write
- delete requires typed confirmation code
- pre-preview approval is never write confirmation; live writes require confirmation after the concrete preview/diff/payload/manifest is shown
- multi-target writes require a grouped manifest only where the owning skill already supports multi-target writes; otherwise process targets sequentially
- structured failure output: what failed / why / next step
- diagnostics only after real capability failure
- claim-evidence binding: verify data-target match before presenting conclusions (see context skill)

## Operational Goal

Minimize context and maintenance overhead while preserving strict write safety:
- flat skill layout for direct discovery
- context skill remains the stable top-level entrypoint
- centralized relay contract
- explicit phase boundaries
- deterministic preview/confirm/apply behavior
