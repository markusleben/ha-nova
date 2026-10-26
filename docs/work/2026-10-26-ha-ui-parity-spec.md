# HA UI parity: gap analysis and order (2026-10-26)

Status: active

Goal (maintainer, 2026-10-26): everything the Home Assistant web UI can do, HA NOVA can do too — and more. Baseline: 0.28 plus `main` fd51d9e2 (device delete, #873). Parents: `docs/work/2026-10-25-chat-parity-spec.md` (chat apps do what the CLI does), `docs/work/2026-10-17-confirmation-hardening-spec.md` (raw allowlist), `docs/work/2026-10-26-device-delete-spec.md`.

## Method

- HA UI inventory: Home Assistant 2026.10 ([release post](https://www.home-assistant.io/blog/2026/10/07/release-202610/)) and the frontend's data modules, which name the exact WS type or REST path behind each button (links per row below).
- HA NOVA side: `skills/**/*.md`, the dispatch table in `skills/ha-nova/SKILL.md`, the raw allowlist `cli/relay_raw_allow.go`, `docs/reference/ha-api-matrix.md`, the capability map in `skills/fallback/SKILL.md`.
- Routes: since chat parity, every CLI route is also the chat route (`ha_plan kind raw`, `ha_read`, typed tools: `cli/mcp_skill.go:20-60`, `cli/plan_raw.go:15-21`). One column covers both.
- Status words: **covered**, **partial**, **external** (with the stated reason), **missing**. Tier per `docs/reference/safety.md`: ordinary / preview / typed. Effort: **S** = one allowlist line plus skill text; **M** = a new skill section or verb; **L** = a Relay change.

## 1. Inventory and mapping

### Settings → Devices & services

| HA UI | HA NOVA today | Status |
|---|---|---|
| Integration: add, options, reconfigure, reauth, reload | `integration-setup`; flows raw (`cli/relay_raw_allow.go:20-22`), reload via `ha-nova call homeassistant.reload_config_entry` | covered |
| Integration: remove | `integration-setup`, typed raw `DELETE …/entry/{id}` with owned devices/entities and consumer preview | covered (0.30) |
| Integration: enable/disable (WS `config_entries/disable`, [config_entries.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/config_entries.ts)) | `integration-setup` → Enable, Disable, Rename, Ignore (raw plan; an entry with a lock, alarm panel, or access cover is refused) | covered (0.29) |
| Integration: rename, system options (polling, new entities) (WS `config_entries/update`) | `integration-setup` | covered (0.29) |
| Integration: subentries add/edit/delete (WS `config_entries/subentries/*`, REST `…/subentries/flow`) | `integration-setup`, existing live-schema loop; deletes typed, edits previewed | covered (0.30) |
| Integration: ignore a discovered one (WS `config_entries/ignore_flow`) | `integration-setup` | covered (0.29) |
| Integration: download diagnostics, logs | `diagnose` (`/api/diagnostics/…`, `system_log/list`) | covered |
| Device: rename, area, labels, disable | `organize` (`config/device_registry/update`, `cli/relay_raw_allow.go:72`) | covered |
| Device: delete (WS `config/device_registry/remove`, HA 2026.9+) | `organize`, typed raw plan (#873: `cli/relay_raw_allow.go:73`; texts updated in `docs/reference/ha-api-matrix.md`, `skills/fallback/SKILL.md` capability map, `skills/device/SKILL.md`, `skills/maintenance/SKILL.md`, `skills/ha-nova/SKILL.md` dispatch) | covered |
| Device: automations, scenes, scripts from the device page | `write`, `scene` (`device_automation/*`) | covered |
| Entity settings: name, id, icon, area, labels, category, show-as, precision, unit, hide, disable, aliases, voice exposure | `organize`, `assist` (`config/entity_registry/update`) | covered |
| Helpers (9 storage, 16 config-entry families) | `helper` | covered (0.29: all 16 config-entry families) |
| Areas, floors, labels, categories, zones, tags, people | `organize`, `admin` (collections `cli/relay_raw_allow.go:109`) | covered; area and person picture upload (REST `/api/image/upload`, multipart) missing |
| Users: list, create, delete | `admin` | covered |
| Users: rename, admin flag, active (WS `config/auth/update`, [user.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/user.ts)) | `admin` (typed for access fields; owner, system, and Relay accounts refused) | covered (0.29) |
| Users: password, MFA, auth providers ([auth.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/auth.ts)) | `skills/admin/SKILL.md:18,54` "stay in the Home Assistant UI on purpose" | external — reason holds (a person's secret) |

### Automations, scenes, scripts, blueprints

| HA UI | HA NOVA today | Status |
|---|---|---|
| Create, edit, delete, duplicate, enable, disable, run; traces; "Triggered by" (2026.10) | `write`, `read`, `service-call`, `ha-nova trace`, `diagnose` | covered |
| Blueprints: import, save, instantiate | `write`, preview raw import/save and ordinary automation/script planning | covered (0.30) |
| Blueprints: delete (WS `blueprint/delete`, [blueprint.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/blueprint.ts)) | `write`, typed raw plan | covered (0.29; owner promoted 0.30) |
| Scenes (storage), scene editor apply | `scene`, `service-call` | covered |

### Settings → System

| HA UI | HA NOVA today | Status |
|---|---|---|
| General: name, location, units, time zone, currency, language, URLs (WS `config/core/update`, `config/core/detect`, [core.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/core.ts)) | `admin` → General settings (typed for time zone and units) | covered (0.29) |
| Network, Storage, Hardware, host/Supervisor/App logs (WS `supervisor/api`, `network/configure`, [hassio/supervisor.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/hassio/supervisor.ts), [network.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/network.ts)) | `/api/hassio/*` answers HTTP 403, and WS `supervisor/api` is refused too: Supervisor's App WS proxy drops every `supervisor/*` and `hassio/*` type, whatever the role (verified 0.31, batch 3b) | disk total/used and OS/Supervisor versions covered (`health`, `system_health/info`); HA's own adapters (WS `network`) one allowlist line away (S); the rest needs a Relay route or role: `docs/work/2026-10-26-supervisor-role-spec.md` (host info with today's role over a new Relay route; logs, storage breakdown, host network changes need `manager`) |
| Apps: install, uninstall, configure, store ([hassio/addon.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/hassio/addon.ts)) | External (`skills/fallback/SKILL.md` Apps section); start/stop/restart typed via `service-call` | needs role: `manager` plus a Relay route (`docs/work/2026-10-26-supervisor-role-spec.md`) |
| Backups: create, inspect, delete | `backup` | covered |
| Backups: restore (WS `backup/restore`) | `skills/backup/SKILL.md:19` "reboots Home Assistant" | external — reason holds (the connection dies mid-call; nothing verifiable) |
| Backups: automatic settings (WS `backup/config/update`, [backup.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/backup.ts)), download | `backup` → Automatic settings (preview; the raw guard refuses the encryption password and per-location encryption) | covered (0.31: schedule, retention, locations, contents); download and the encryption key external |
| Logs (`system_log/list`, `logger.set_level`) | `diagnose` | covered; `/api/error_log` partial (404 on HA OS, matrix note) |
| Repairs: list | `health` | covered |
| Repairs: ignore (WS `repairs/ignore_issue`), fix flows (REST `POST /api/repairs/issues/fix`, [repairs.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/repairs.ts)) | `health`, live-schema fix steps previewed; deletion/removal repairs typed | covered (0.29 ignore; 0.30 fix) |
| Updates: install, skip, release notes | `updates` | covered; the App auto-update toggle is an App option and needs `manager` (Supervisor row) |
| Analytics (WS `analytics` read, `analytics/preferences` write, [analytics.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/analytics.ts)) | `admin` (preview) | covered (0.31) |
| AI page (2026.10: MCP server card; WS `ai_task/preferences/set`) | `integration-setup` adds the integration | partial |
| Labs (WS `labs/list`, `labs/update`, [labs.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/labs.ts)) | `admin` (typed) | covered (0.31) |
| Voice assistants: pipelines, exposure, engines, custom sentences | `assist` | covered |
| Voice: expose new entities (WS `homeassistant/expose_new_entities/set`, [expose.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/expose.ts)), pipeline debug (WS `assist_pipeline/pipeline_debug/list|get`) | `assist` | covered (0.29) |

### Dashboards, data views, everyday panels

| HA UI | HA NOVA today | Status |
|---|---|---|
| Dashboards: create, edit views/sections/cards, strategies, delete, resources (typed), view theme | `dashboard` (`plan write --domain dashboard`) | covered |
| Themes (YAML); default dashboard per user (`frontend/set_user_data`) | `yaml-config`; nothing | covered; external (a person's own preference) |
| Energy configuration and reports | `energy` | covered |
| History, logbook, statistics | `history`, `ha-nova history` | covered |
| Map (2026.10 overview) | zones and persons via `admin`, positions via states | partial (data only, no map) |
| Media: browse, play, TTS | `media` | covered; local media upload and delete (REST `/api/media_source/local_source/upload`, WS `media_source/local_source/remove`) missing |
| To-do, calendar | `todo`, `calendar` | covered |
| Persistent notifications | `notify` | covered |
| Profile: theme, language, push, tokens, MFA, password | nothing | external — a person's own session and secrets; stays out |

### Developer tools

| HA UI | HA NOVA today | Status |
|---|---|---|
| States: read | `ha-nova find`, `get`, `/api/states` | covered |
| States: set | refused (`cli/relay_raw_guard.go:35` "states are set by devices") | external — reason holds (fakes device state) |
| Actions | `ha-nova call` | covered |
| Templates | `POST /api/template` | covered |
| Events: fire | refused (`skills/service-call/raw-routes.md` → Custom Events And Webhooks) | external — reason holds (untiered listener fan-out) |
| Events: listen | `fallback` bounded capture | covered |
| Statistics: validate, fix unit and sum, clear, import (WS `recorder/import_statistics`; Core only, no UI button) | `maintenance` | covered (import 0.31, typed) |
| YAML reload, check configuration, restart | `ha-nova call …reload_*` (typed, `cli/tier_rules.go:255`), `check_config` | covered |

## 2. Gaps: API, tier, fit, effort

Every row fits "Relay stays dumb, Skills stay smart" unless marked ★ (a Relay or App-manifest change). KISS route = the smallest change through what exists: an allowlist line in `cli/relay_raw_allow.go` (the raw guard, `plan raw` tiering, and chat parity come for free) plus a skill section.

| Gap | HA API | Tier | Effort | KISS route |
|---|---|---|---|---|
| Integration enable/disable | WS `config_entries/disable` | preview; refused when the entry owns a lock, alarm panel, or access cover | S | covered (0.29): allowlist + `integration-setup`; External row removed |
| Integration rename, system options | WS `config_entries/update` | preview | S | covered (0.29): allowlist + `integration-setup` |
| Integration remove, owned | existing raw DELETE | typed | S | covered (0.30): `integration-setup`, counts and consumers before confirmation |
| Subentries | WS `config_entries/subentries/*`, REST `…/subentries/flow` | preview; delete typed | M | covered (0.30): existing live-schema loop, three WS types, start/step/cancel paths |
| Ignore discovery | WS `config_entries/ignore_flow` | preview | S | covered (0.29): allowlist + `integration-setup` |
| User update | WS `config/auth/update` | typed for access fields | S | covered (0.29): allowlist + `admin`; live owner/system/Relay-account guards |
| System General | WS `config/core/update`, `config/core/detect` (read) | preview; typed for time zone and units (they break statistics) | S | covered (0.29): allowlist + `admin` (the home zone lives there) |
| Repairs ignore | WS `repairs/ignore_issue` | preview | S | covered (0.29): allowlist + `health` (its only write) |
| Repairs fix flows | REST `POST /api/repairs/issues/fix`, `…/fix/{flow_id}` | preview; typed when a flow deletes | M | covered (0.30): health reuses live-schema stepping; start/step/cancel, by-id GET plan-only |
| Blueprint delete and owner | WS `blueprint/delete` | typed (consumers via `search/related`) | S / M | covered (0.30): write owns import/save/instantiate/delete |
| Expose new entities; pipeline debug | WS `homeassistant/expose_new_entities/set`; `assist_pipeline/pipeline_debug/*` (reads) | preview for exposure; debug reads | S | covered (0.29): allowlist + `assist`, `diagnose` |
| Labs | WS `labs/update`, `labs/list` (read) | typed (experimental features) | S | covered (0.31): allowlist + `admin` (Settings → System, beside General) |
| Backup automatic settings | WS `backup/config/update` | preview; the password and per-location encryption are refused by the guard | S | covered (0.31): allowlist + field guard in `checkRawWS` + `backup` |
| Remaining four helper families | existing flow paths | preview | S | covered (0.29): `helper` domain list; existing config-entry flow paths |
| Statistics import | WS `recorder/import_statistics` | typed | M | covered (0.31): `maintenance` + reference payload; rows ride in the `plan raw` payload |
| Analytics sharing | WS `analytics` (read), `analytics/preferences` | preview | S | covered (0.31): allowlist + `admin` |
| Media and image upload ★ | multipart REST | preview | L | the Relay's `/core` is JSON-only: new machinery; defer |
| Supervisor: Apps, network, storage, hardware, host logs ★ | WS `supervisor/api` (`endpoint`, `method`, `data`) | reads ordinary; App start/install typed; `host/reboot`, `network/*` typed | L | verified (batch 3b): `supervisor/api` does not pass through `/ws` for any endpoint or role, so no allowlist line; a Relay route (info reads, today's role) and `hassio_role: manager` (the rest) are specified in `docs/work/2026-10-26-supervisor-role-spec.md` for their own security review |

External reasons removed in 0.29: integration enable/disable (a plain WS write). Supervisor: the WS path the frontend uses is closed to Apps too (batch 3b), so the External reason now names the Supervisor's App filter. Backup schedule and retention moved in 0.31.

Stays out, with the reason: passwords, MFA, tokens, profile (a person's own secrets and session); backup restore (reboots Home Assistant, unverifiable); set state (fakes a device); fire event and webhook (untiered fan-out; a webhook id is a secret); Zigbee, Z-Wave, Matter pairing (hardware and BLE, not an API); Home Assistant Link account settings.

## 3. And more

Exists today: whole-home map and where-used with exact counts (`ha-nova map`, `cli/verb_map.go:17`); device swap with a references scan over dashboards, energy, and templates (`ha-nova get --references`); 40+ automation review checks; config snapshots with undo (`snapshot`, `diff`, `plan revert`); root-cause diagnosis across traces, logs, and history; rule migration from other platforms and legacy YAML; "what does my button fire" bounded capture; energy KPI reports with period comparisons; Assist utterance tests; grouped change sets with one confirmation on phone or window.

Worth adding: bulk registry edits with one preview (rename 40 entities); a plain-language "explain my home" document kept current; repairs triage with a one-shot fix per issue; "undo the last hour" from snapshots.

## 4. Order

1. **0.29 — allowlist batch (all S, one PR; covered):** integration enable/disable, rename and system options, ignore discovery; user update; repairs ignore; expose new entities; pipeline debug reads; blueprint delete; the four helper families; System General. Highest daily value, no machinery.
2. **0.30 — flows (M; covered):** repairs fix flows, subentries, integration remove and blueprints owned by a skill. Reuses the live-schema flow.
3. **0.31 — system (M → L):** Supervisor verified on the test bed (batch 3b: `supervisor/api` refused); backup settings; labs; statistics import.
4. **Later, spec first:** media and image upload (Relay change); Supervisor access through a Relay route and a role change (`docs/work/2026-10-26-supervisor-role-spec.md`).

## Decisions (maintainer, 2026-10-26)

1. Goal: everything the Home Assistant web UI can do, HA NOVA can do too — and more.
2. Always KISS: the smallest route through what exists (allowlist line, `plan raw`, existing skills) over new abstractions; a change that adds machinery says so.
3. Order: 0.29 allowlist batch → 0.30 flows → 0.31 system. The Supervisor path is verified on the test bed before any design; a role change for the App is a separate security spec.

## Known limits

- Supervisor items wait for `docs/work/2026-10-26-supervisor-role-spec.md`; `supervisor/api` through the Relay is refused (batch 3b).
- Open issues were not cross-checked against the gaps.
- Line numbers cite `main` fd51d9e2.


## Batch 1 source verification (2026-10-26)

All requested surfaces exist; none dropped. Verified against Home Assistant Core dev
commit `0955a8f7794e0a35fcff2873bddd222819d8ca86` (links pinned below).
Integration enable/disable also checks source-switch devices backing a
`switch_as_x` access entity, as the existing source-switch guard does. Polling
preference writes use that same guard because HA reloads loaded entries. For
non-access entries they remain previewed, as specified for this batch, with the
reload interruption disclosed and `require_restart` handled.
Device-less wrappers use the recorded `options.switch_as_x.entity_id`, not empty
device IDs: [switch_as_x/entity.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/switch_as_x/entity.py)
records it and [config/entity_registry.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/config/entity_registry.py)
returns the extended entry. An unreadable mapping is refused; a known unrelated
wrapper does not block the operation. The existing source-switch guard shares
this resolution.
No new verb or Relay behavior; `homeassistant/expose_new_entities/get` is also
allowlisted for readback. User access fields (`group_ids`, `is_active`,
`local_only`) are typed; an ordinary name edit remains previewed.

| Item | HA source and contract |
|---|---|
| Integration enable/disable, rename/system options, ignore discovery | [config/config_entries.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/config/config_entries.py): `config_entries/disable` takes `entry_id`, `disabled_by: "user"\|null`; `config_entries/update` takes `title`, `pref_disable_polling`, `pref_disable_new_entities`; `config_entries/ignore_flow` takes `flow_id`, `title` and requires discovery `unique_id`. |
| User edit | [config/auth.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/config/auth.py): `config/auth/update` uses `user_id`, `name`, `is_active`, `group_ids`, `local_only`; admin is `group_ids: ["system-admin"]`, not an `is_admin` field. |
| System General | [config/core.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/config/core.py): `config/core/update`, `config/core/detect`; home name is `location_name`, units use `unit_system` and optional `update_units`. |
| Repair ignore | [repairs/websocket_api.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/repairs/websocket_api.py): `repairs/ignore_issue` takes `domain`, `issue_id`, `ignore`. Health owns its only write. |
| Expose new entities | [homeassistant/exposed_entities.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/homeassistant/exposed_entities.py): `homeassistant/expose_new_entities/get\|set` uses `assistant`, `expose_new`; new covers follow, locks and alarm panels do not. |
| Pipeline debug reads | [assist_pipeline/websocket_api.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/assist_pipeline/websocket_api.py): `assist_pipeline/pipeline_debug/list` uses `pipeline_id`; `/get` also requires `pipeline_run_id`. |
| Blueprint delete | [blueprint/websocket_api.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/blueprint/websocket_api.py): `blueprint/delete` takes `domain`, `path`; [models.py](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/blueprint/models.py) refuses a blueprint in use. Consumers are queried through `search/related` with `automation_blueprint` or `script_blueprint`. |
| Four helper families | Manifests for [trend](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/trend/manifest.json), [random](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/random/manifest.json), [filter](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/filter/manifest.json), [generic_hygrostat](https://github.com/home-assistant/core/blob/0955a8f7794e0a35fcff2873bddd222819d8ca86/homeassistant/components/generic_hygrostat/manifest.json) all declare `config_flow: true`; the existing config-entry REST flow paths above apply. Fields stay live-schema-driven. |

Disposable test-bed agent run: HA 2026.10.1 / Relay 0.12.0. Sun disabled and
re-enabled through previewed raw plans with independent entry readbacks; core
`location_name` changed and restored with `get_config` readbacks; disabling the
MQTT entry owning access entities was refused before sending. Automatic exposure
for `conversation` was toggled true → false → true with its own getter verifying
each preference write. Repairs were empty,
so ignore/un-ignore was skipped. Core saves moved `config_source` from `yaml` to
`storage` (HA's normal settings persistence); the requested name was restored.
The owned test bed was taken down afterwards. No live home or native credential
tests ran. Existing Go tier coverage now exercises every allowlisted write through
both CLI and chat planning; no new Vitest files or budget increases.

## Batch 2 source verification (2026-10-26)

All four requested items exist; none dropped. Current dev revisions checked:
Core `54a29725d9ef2b2e000aa34775b49af5ecc3af3e`, frontend
`2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1`. No new verb, Relay behavior,
or flow abstraction: existing raw allowlist, `plan raw`, tier table, and
live-schema stepping only. DELETE cancellation is included because the UI
cancels transient repair and subentry flows too; it remains preview-tier.

| Item | Pinned source and verified contract |
|---|---|
| Repair fix | Core [repairs/websocket_api.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/components/repairs/websocket_api.py) and frontend [repairs.ts](https://github.com/home-assistant/frontend/blob/2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1/src/data/repairs.ts): POST start uses `handler: <domain>`, `issue_id`; by-id GET/POST continue; DELETE cancels. Core [issue_handler.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/components/repairs/issue_handler.py) delegates to integration-specific repair code; there is no deletion-result flag. Health inspects issue domain/translation and live descriptions for delete/remove and raises existing raw-item `typed: true`, including starts and continuation GETs. This is a skill decision, not a new CLI semantic classifier. |
| Subentries | Core [config/config_entries.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/components/config/config_entries.py) and frontend [config_entries.ts](https://github.com/home-assistant/frontend/blob/2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1/src/data/config_entries.ts): list uses `entry_id`; update adds `subentry_id`, optional `title` only; delete uses both IDs. REST start uses `handler: [entry_id, subentry_type]`; optional `subentry_id` selects reconfigure. By-id GET/POST/DELETE reuse the common flow manager. The CLI reuses its existing entry access guard on subentry delete and flow start: any access-capable parent is refused conservatively, even when another subentry is selected. |
| Integration remove | The same Core [config/config_entries.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/components/config/config_entries.py) and frontend [config_entries.ts](https://github.com/home-assistant/frontend/blob/2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1/src/data/config_entries.ts): existing REST DELETE entry endpoint, now owned by integration-setup. Preview owned devices/entities, related consumers, event listeners, and unindexed references; typed, no undo. |
| Blueprints | Core [blueprint/websocket_api.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/components/blueprint/websocket_api.py) and frontend [blueprint.ts](https://github.com/home-assistant/frontend/blob/2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1/src/data/blueprint.ts): import fetches/previews; save accepts `domain`, `path`, `yaml`, optional `source_url`; substitute takes `domain`, `path`, `input`, returns `substituted_config`. Write owns all stages, reviews expanded actions, then uses ordinary automation/script planning; the existing override refusal remains. Delete stays typed with consumer checks. |

Repair descriptions use the UI's existing translation read, now allowlisted:
Core [frontend/__init__.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/components/frontend/__init__.py)
defines `frontend/get_translations` with `language`, `category`, `integration`;
frontend [translation.ts](https://github.com/home-assistant/frontend/blob/2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1/src/data/translation.ts)
reads its `resources`. The [repair dialog](https://github.com/home-assistant/frontend/blob/2e5aae8932324e5ad5b92d279ac3d6dbf3f0fbe1/src/dialogs/repairs-flow/show-dialog-repair-flow.ts)
loads category `issues` and resolves
`component.<domain>.issues.<translation_key-or-issue_id>.fix_flow.step.<step_id>.description`,
overlaying step placeholders onto issue placeholders. Missing descriptions
remain a UI handoff, not proof of a harmless empty form.
Review hardening rejects non-array subentry handlers before forwarding: Core's
tuple coercion also accepts object keys, which must not bypass the parent
guard. Reconfigure previews name the selected subentry, not just its parent.
Device ownership normalization also handles legacy
`config_entries_subentries[entry_id]` only when the singular field is absent;
null singular ownership is authoritative. Pinned Core
[device_registry.py](https://github.com/home-assistant/core/blob/54a29725d9ef2b2e000aa34775b49af5ecc3af3e/homeassistant/helpers/device_registry.py)
retains this compatibility mapping. Unresolved scope blocks destructive previews.
The tier table covers every new read/write through CLI/chat planning; dedicated
regressions cover the coercion bypass, translated repair reads, and target names.
No new test file or budget increase was needed.

Disposable test-bed agent run: HA 2026.10.1 / Relay 0.12.0. An isolated
MQTT sensor subentry fixture was prepared through Core's live forms, then
the agent read the owning skill, listed it, planned its title-only update
at preview tier, applied it, and independently verified the same subentry id
and renamed title. No unrelated subentries existed, so their preservation
was not exercised. The agent then scanned Sun's one device and nine entities
(all ten related queries valid; no automation/script/scene consumers),
previewed the unindexed-reference limits, and removed Sun through a typed raw
plan. Readback proved the entry, device, and nine entities absent;
117 unrelated entity rows, 68 device rows, and nine entries survived unchanged.
Both applies returned exit 0; no restart required. Repairs were empty, so
the repair-fix live run was skipped. Flow navigation and blueprint lifecycle
were source/contract-tested, not exercised end to end. The owned bed was
taken down; no live home, pairing, setup, uninstall, Cloud, or native-keychain
tests ran. Skill word/route budgets remain unchanged.

## Batch 3a source verification (2026-10-26)

The four items of 0.31 that need no Supervisor; all exist, none dropped. Checked
against Core dev `39922841decf4988ec3a74d8bc96edeb7e318151` and frontend dev `068eee3d8082e78aa9fcba7af04bfeb4edf45b99`. No new verb,
Relay behavior, or abstraction: allowlist lines, tier-table entries, one guard
case, plain preview lines, and skill text. Supervisor items stay in batch 3b.

| Item | Pinned source and verified contract |
|---|---|
| Backup automatic settings | Core [backup/websocket.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/backup/websocket.py) and [backup/config.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/backup/config.py), frontend [backup.ts](https://github.com/home-assistant/frontend/blob/068eee3d8082e78aa9fcba7af04bfeb4edf45b99/src/data/backup.ts): `backup/config/update` takes `agents.<id>.{protected, retention}`, `automatic_backups_configured`, `create_backup.{agent_ids, include_addons, include_all_addons, include_database, include_folders, name, password}`, `retention.{copies, days}`, `schedule.{days, recurrence, time}`. `schedule` and `retention` are rebuilt whole (omitted fields reset: recurrence `never`, no limit); `create_backup` and per-agent settings merge. Retention by days deletes older automatic backups from a timer one day later; by copies after the next automatic backup. `backup/config/info` returns `create_backup.password` in clear text, which the CLI already redacts. Owner: `backup`. Tier preview; typed with any retention (it deletes older automatic backups; every other delete is typed too). |
| Labs | Core [labs/websocket_api.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/labs/websocket_api.py), frontend [labs.ts](https://github.com/home-assistant/frontend/blob/068eee3d8082e78aa9fcba7af04bfeb4edf45b99/src/data/labs.ts): `labs/list` returns `features[]` (`domain`, `preview_feature`, `enabled`, URLs); `labs/update` takes `domain`, `preview_feature`, `enabled`, optional `create_backup` (an automatic-settings backup before enabling, awaited to the end through `async_create_automatic_backup`, so longer than the Relay's 10 s request timeout: refused; the backup skill's Create runs first). Both admin-only. Owner: `admin` (Settings → System, beside General) rather than the context skill, which carries no write flows. Tier typed. |
| Statistics import | Core [recorder/websocket_api.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/recorder/websocket_api.py) and [recorder/statistics.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/recorder/statistics.py): `recorder/import_statistics` takes `metadata` (`statistic_id`, `source`, `name`, `unit_of_measurement`, `unit_class`, `mean_type`, `has_sum`) and `stats[]` (`start` on the hour, `sum`, `state`, `mean`, `min`, `max`, `last_reset`). An entity id needs `source: recorder`; `domain:name` needs `source` = domain. Rows for existing hours are overwritten and metadata updated. Missing `mean_type` or `unit_class` logs a deprecation; Core 2026.11 rejects it. The frontend has no import button; Core's WS command is the surface. Owner: `maintenance` (payload in `maintenance-reference.md`). Tier typed. |
| Analytics | Core [analytics/__init__.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/analytics/__init__.py), frontend [analytics.ts](https://github.com/home-assistant/frontend/blob/068eee3d8082e78aa9fcba7af04bfeb4edf45b99/src/data/analytics.ts): the read is `analytics` (`preferences`, `onboarded`); the write is `analytics/preferences` with `preferences` (`base`, `diagnostics`, `usage`, `statistics`, `snapshots`), merged into the stored ones. The gap table above had the two names swapped. Owner: `admin`. Tier preview. |

Guard (`cli/relay_raw_guard.go`, `checkRawWS`): a `backup/config/update` that
carries `create_backup.password` (any value, `null` included: it clears
encryption), any `agents.<id>.protected` (turns encryption on or off for a
location), or a `create_backup`/`agents` value that is not an object is refused
on every raw route — relay, `plan raw`, chat `ha_plan`, and a
`collect_events` envelope — before anything is sent. Decision (recorded):
`protected` counts as an encryption-key field, beyond the literal password.
Further refusals after review: `automatic_backups_configured` (the first
setup creates the key), a `schedule`, `retention`, or `agents.<id>.retention`
block missing a field (Home Assistant resets it), and a remote location
added while no key is saved (read live from `backup/config/info`; an
unreadable read sends nothing). Sibling sweep: `backup/generate` with its own
`password` or a remote location is refused for the same reason.
Changing `agent_ids` to locations with a key saved stays previewed; the skill names each
location from `backup/agents/info`: a location without its own setting encrypts with the saved key
([backup/manager.py](https://github.com/home-assistant/core/blob/39922841decf4988ec3a74d8bc96edeb7e318151/homeassistant/components/backup/manager.py)).
With no saved key Home Assistant uploads as is, so the skill sends the first
automatic setup (`automatic_backups_configured: false`), which creates the
key, to the Home Assistant UI.

Known limits: an already configured remote location still uploads unencrypted
if the key were missing (the UI setup always saves one); the analytics
preview does not say that `usage` and `statistics` need `base`; importing into an existing entity statistic is typed but not
diffed row by row in the plan (the skill reads the range first).

Disposable test-bed run: HA 2026.10.1 / Relay 0.12.0. A previewed
`backup/config/update` set a daily 04:45 schedule, 3 copies, and
`backup.local`; `backup/config/info` read it back (next run the next morning).
Setting `create_backup.password` and `agents.backup.local.protected: false` was
refused on `relay ws` and `plan raw` with nothing sent (password still
`null`). After review: a retention change planned typed; a partial schedule,
a remote location without a key, `automatic_backups_configured`, and
`labs/update` with `create_backup` were refused. `labs/list` showed three
features; `frontend.winter_mode` was turned on and off through typed plans,
each read back. A typed `recorder/import_statistics` wrote three hourly rows
into the throwaway `nova_test:batch3a`, read back with
`statistics_during_period` and `list_statistic_ids`. `analytics/preferences`
was planned at preview tier only (no apply: the bed sends nothing out).
A separate agent session then followed the skills for a schedule plus
retention change, a refused password, an external-statistic import, and a
Labs on/off, with independent read-backs; its wording notes are folded in
above. Typed plans were confirmed with chat codes after turning confirmation
windows off in the bed's isolated HOME. The owned bed was taken down; no live
home, pairing, setup, uninstall, Cloud, or native-keychain tests ran. Skill
word budgets unchanged (backup and admin trimmed to fit).

## Batch 3b Supervisor verification (2026-10-26)

Verified first, as Decision 3 asks, on the throwaway HAOS VM (HAOS 18.3,
Supervisor 2026.10.3, Core 2026.10.2, App 0.11.2; identity checked before any
write: location "Home", demo entry, QEMU owns the forwarded ports). Probed from
inside the App container with its `SUPERVISOR_TOKEN`, the Relay's own upstream.
WS `supervisor/api` was refused (`unauthorized`) for all 25 requests tried,
reads and the one write (`auto_update` set to its current value) alike;
`hassio/*` and `supervisor/subscribe` too. The Supervisor log shows its App WS
proxy blocking them (`DENIED_WS_TYPE_PREFIXES`), and that filter ignores
`hassio_role`. So nothing is allowlisted: an allowlist line would only ever
fail. Direct Supervisor REST with the same token reaches the `/.+/info` reads
on today's role; Apps, store, logs, and storage need `manager`. Details, the
role per UI item, and options A/B/C: `docs/work/2026-10-26-supervisor-role-spec.md`.
A pre-test App backup was taken and removed afterwards; App options, boot, and
state ended unchanged, and the probe files were deleted. No live home, pairing,
setup, uninstall, Cloud, or keychain tests ran. Skill budgets unchanged.
