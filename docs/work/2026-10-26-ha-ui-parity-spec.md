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
| Integration: remove | `fallback` Relay-Ready, raw `DELETE …/entry/{id}` (`cli/relay_raw_allow.go:23`, `skills/fallback/relay-ready.md:165`) | partial (no owning skill; the code gate is skill text, the typed tier comes from `plan raw`) |
| Integration: enable/disable (WS `config_entries/disable`, [config_entries.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/config_entries.ts)) | External "Settings > Devices & services" (`skills/fallback/SKILL.md:99`, `skills/organize/SKILL.md:106`) | external — no reason stated; the reason no longer holds |
| Integration: rename, system options (polling, new entities) (WS `config_entries/update`) | nothing | missing |
| Integration: subentries add/edit/delete (WS `config_entries/subentries/*`, REST `…/subentries/flow`) | `integration-setup` excludes them | missing |
| Integration: ignore a discovered one (WS `config_entries/ignore_flow`) | nothing | missing |
| Integration: download diagnostics, logs | `diagnose` (`/api/diagnostics/…`, `system_log/list`) | covered |
| Device: rename, area, labels, disable | `organize` (`config/device_registry/update`, `cli/relay_raw_allow.go:72`) | covered |
| Device: delete (WS `config/device_registry/remove`, HA 2026.9+) | `organize`, typed raw plan (#873: `cli/relay_raw_allow.go:73`; texts updated in `docs/reference/ha-api-matrix.md`, `skills/fallback/SKILL.md` capability map, `skills/device/SKILL.md`, `skills/maintenance/SKILL.md`, `skills/ha-nova/SKILL.md` dispatch) | covered |
| Device: automations, scenes, scripts from the device page | `write`, `scene` (`device_automation/*`) | covered |
| Entity settings: name, id, icon, area, labels, category, show-as, precision, unit, hide, disable, aliases, voice exposure | `organize`, `assist` (`config/entity_registry/update`) | covered |
| Helpers (9 storage, 12 config-entry families) | `helper` | covered; `trend`, `random`, `filter`, `generic_hygrostat` Relay-Ready only (`skills/fallback/relay-ready.md:27`) |
| Areas, floors, labels, categories, zones, tags, people | `organize`, `admin` (collections `cli/relay_raw_allow.go:109`) | covered; area and person picture upload (REST `/api/image/upload`, multipart) missing |
| Users: list, create, delete | `admin` | covered |
| Users: rename, admin flag, active (WS `config/auth/update`, [user.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/user.ts)) | nothing | missing |
| Users: password, MFA, auth providers ([auth.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/auth.ts)) | `skills/admin/SKILL.md:18,54` "stay in the Home Assistant UI on purpose" | external — reason holds (a person's secret) |

### Automations, scenes, scripts, blueprints

| HA UI | HA NOVA today | Status |
|---|---|---|
| Create, edit, delete, duplicate, enable, disable, run; traces; "Triggered by" (2026.10) | `write`, `read`, `service-call`, `ha-nova trace`, `diagnose` | covered |
| Blueprints: import, save, instantiate | `fallback` Relay-Ready (`skills/fallback/relay-ready.md:8`) plus `write` | partial (no owning skill) |
| Blueprints: delete (WS `blueprint/delete`, [blueprint.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/blueprint.ts)) | nothing | missing |
| Scenes (storage), scene editor apply | `scene`, `service-call` | covered |

### Settings → System

| HA UI | HA NOVA today | Status |
|---|---|---|
| General: name, location, units, time zone, currency, language, URLs (WS `config/core/update`, `config/core/detect`, [core.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/core.ts)) | nothing (`skills/admin/SKILL.md:39` names the home zone only) | missing |
| Network, Storage, Hardware, host/Supervisor/App logs (WS `supervisor/api`, `network/configure`, [hassio/supervisor.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/hassio/supervisor.ts), [network.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/network.ts)) | `docs/reference/ha-api-matrix.md:233-234` pins `/api/hassio/*` as HTTP 403; the App runs with `hassio_role: default` (`nova/config.yaml:30-32`) | external — reason partly stale: the frontend uses WS `supervisor/api`, not the REST proxy; unverified through the Relay |
| Apps: install, uninstall, configure, store ([hassio/addon.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/hassio/addon.ts)) | External (`skills/fallback/SKILL.md` Apps section); start/stop/restart typed via `service-call` | external — same Supervisor question |
| Backups: create, inspect, delete | `backup` | covered |
| Backups: restore (WS `backup/restore`) | `skills/backup/SKILL.md:19` "reboots Home Assistant" | external — reason holds (the connection dies mid-call; nothing verifiable) |
| Backups: automatic settings (WS `backup/config/update`, [backup.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/backup.ts)), download | `skills/backup/SKILL.md:20` "HA UI" | external — schedule and retention: reason no longer holds (a storage write like energy prefs); the encryption key stays out |
| Logs (`system_log/list`, `logger.set_level`) | `diagnose` | covered; `/api/error_log` partial (404 on HA OS, matrix note) |
| Repairs: list | `health` | covered |
| Repairs: ignore (WS `repairs/ignore_issue`), fix flows (REST `POST /api/repairs/issues/fix`, [repairs.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/repairs.ts)) | `skills/health/SKILL.md:20` "Not in scope: repair/fix/ignore actions" | missing |
| Updates: install, skip, release notes | `updates` | covered; auto-update is an App option → Supervisor row |
| Analytics (WS `analytics/preferences`) | nothing | missing (low value) |
| AI page (2026.10: MCP server card; WS `ai_task/preferences/set`) | `integration-setup` adds the integration | partial |
| Labs (WS `labs/update`, [labs.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/labs.ts)) | nothing | missing |
| Voice assistants: pipelines, exposure, engines, custom sentences | `assist` | covered |
| Voice: expose new entities (WS `homeassistant/expose_new_entities/set`, [expose.ts](https://raw.githubusercontent.com/home-assistant/frontend/dev/src/data/expose.ts)), pipeline debug (WS `assist_pipeline/pipeline_debug/list|get`) | nothing | missing |

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
| Statistics: validate, fix unit and sum, clear | `maintenance` | covered; `recorder/import_statistics` missing |
| YAML reload, check configuration, restart | `ha-nova call …reload_*` (typed, `cli/tier_rules.go:255`), `check_config` | covered |

## 2. Gaps: API, tier, fit, effort

Every row fits "Relay stays dumb, Skills stay smart" unless marked ★ (a Relay or App-manifest change). KISS route = the smallest change through what exists: an allowlist line in `cli/relay_raw_allow.go` (the raw guard, `plan raw` tiering, and chat parity come for free) plus a skill section.

| Gap | HA API | Tier | Effort | KISS route |
|---|---|---|---|---|
| Integration enable/disable | WS `config_entries/disable` | preview; typed when the entry owns a lock, alarm panel, or access cover (reuse the device-disable refusal, `cli/relay_raw_allow.go:72`) | S | allowlist + `integration-setup`; delete the External row |
| Integration rename, system options | WS `config_entries/update` | ordinary | S | allowlist + `integration-setup` |
| Integration remove, owned | existing raw DELETE | typed | S | move the text from `relay-ready.md` into `integration-setup`; no CLI change |
| Subentries | WS `config_entries/subentries/*`, REST `…/subentries/flow` | preview | M | the live-schema flow of options; three allowlist types, two paths |
| Ignore discovery | WS `config_entries/ignore_flow` | preview | S | allowlist + `integration-setup` |
| User update | WS `config/auth/update` | typed (admin flag) | S | allowlist + `admin`; its owner/system guards exist |
| System General | WS `config/core/update`, `config/core/detect` (read) | preview; typed for time zone and units (they break statistics) | S | allowlist + `admin` (the home zone lives there) |
| Repairs ignore | WS `repairs/ignore_issue` | preview | S | allowlist + `health` (its only write) |
| Repairs fix flows | REST `POST /api/repairs/issues/fix`, `…/fix/{flow_id}` | preview; typed when a flow deletes | M | reuse `integration-setup`'s flow stepping; two allowlist paths |
| Blueprint delete and owner | WS `blueprint/delete` | typed (consumers via `search/related`) | S / M | allowlist + promote the Relay-Ready text into `write` |
| Expose new entities; pipeline debug | WS `homeassistant/expose_new_entities/set`; `assist_pipeline/pipeline_debug/*` (reads) | ordinary | S | allowlist + `assist`, `diagnose` |
| Labs | WS `labs/update` | typed (experimental features) | S | allowlist + context skill |
| Backup automatic settings | WS `backup/config/update` | preview; a password field is refused by the guard | S | allowlist with a field guard like the resource-URL one (`cli/relay_raw_allow.go:75`) + `backup` |
| Remaining four helper families | existing flow paths | preview | S | add to `helper`'s domain list; no CLI change |
| Statistics import | WS `recorder/import_statistics` | typed | M | `maintenance`; data file through `plan raw` |
| Media and image upload ★ | multipart REST | preview | L | the Relay's `/core` is JSON-only: new machinery; defer |
| Supervisor: Apps, network, storage, hardware, host logs ★ | WS `supervisor/api` (`endpoint`, `method`, `data`) | reads ordinary; App start/install typed; `host/reboot`, `network/*` typed | M to verify; L if it fails | first prove on the test bed whether `supervisor/api` passes through `/ws` (the pin at `docs/reference/ha-api-matrix.md:233-234` covers `/api/hassio/*` only). If it passes: allowlist `supervisor/api` with an endpoint allowlist in the CLI guard, same pattern as resource URLs. If it fails: `hassio_role: manager` in `nova/config.yaml` is a privilege change and gets its own security spec |

External rows whose reason no longer holds: integration enable/disable (no reason given; a plain WS write), backup schedule and retention (the same kind of storage write as energy prefs), Supervisor via `/api/hassio/*` (the frontend path is WS).

Stays out, with the reason: passwords, MFA, tokens, profile (a person's own secrets and session); backup restore (reboots Home Assistant, unverifiable); set state (fakes a device); fire event and webhook (untiered fan-out; a webhook id is a secret); Zigbee, Z-Wave, Matter pairing (hardware and BLE, not an API); Home Assistant Link account settings.

## 3. And more

Exists today: whole-home map and where-used with exact counts (`ha-nova map`, `cli/verb_map.go:17`); device swap with a references scan over dashboards, energy, and templates (`ha-nova get --references`); 40+ automation review checks; config snapshots with undo (`snapshot`, `diff`, `plan revert`); root-cause diagnosis across traces, logs, and history; rule migration from other platforms and legacy YAML; "what does my button fire" bounded capture; energy KPI reports with period comparisons; Assist utterance tests; grouped change sets with one confirmation on phone or window.

Worth adding: bulk registry edits with one preview (rename 40 entities); a plain-language "explain my home" document kept current; repairs triage with a one-shot fix per issue; "undo the last hour" from snapshots.

## 4. Order

1. **0.29 — allowlist batch (all S, one PR):** integration enable/disable, rename and system options, ignore discovery; user update; repairs ignore; expose new entities; pipeline debug reads; blueprint delete; the four helper families; System General. Highest daily value, no machinery.
2. **0.30 — flows (M):** repairs fix flows, subentries, integration remove and blueprints owned by a skill. Reuses the live-schema flow.
3. **0.31 — system (M → L):** Supervisor verified on the test bed first, then Apps, network, storage, host logs; backup settings; labs; statistics import.
4. **Later, spec first:** media and image upload (Relay change); a Supervisor role change if the WS path fails.

## Decisions (maintainer, 2026-10-26)

1. Goal: everything the Home Assistant web UI can do, HA NOVA can do too — and more.
2. Always KISS: the smallest route through what exists (allowlist line, `plan raw`, existing skills) over new abstractions; a change that adds machinery says so.
3. Order: 0.29 allowlist batch → 0.30 flows → 0.31 system. The Supervisor path is verified on the test bed before any design; a role change for the App is a separate security spec.

## Known limits

- The `supervisor/api` pass-through is unverified; the matrix pin covers only the REST proxy.
- Open issues were not cross-checked against the gaps.
- Line numbers cite `main` fd51d9e2.
