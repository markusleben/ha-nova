# Home Assistant API Matrix

Which HA operations require REST, WS, or filesystem?

## REST API (via the Relay `/core` proxy)

| Endpoint | Method | Purpose |
|----------|---------|-----|
| `/api/` | GET | API running check |
| `/api/states` | GET | All entity states |
| `/api/states/{entity_id}` | GET | Single entity state |
| `/api/services` | GET | All available services (with ETag caching) |
| `/api/services/{domain}/{service}` | POST | Call service (`?return_response` for response data) |
| `/api/config` | GET | HA Core configuration |
| `/api/config/core/check_config` | POST | Validate YAML configuration |
| `/api/template` | POST | Render Jinja2 template (body: `{"template": "..."}`) |
| `/api/events/{event_type}` | POST | Fire a custom event (admin; JSON object; asynchronous listener execution) |
| `/api/webhook/{webhook_id}` | POST | Invoke a known JSON webhook (opaque HTTP 200; effect verification required) |
| `/api/history/period/{start_iso}` | GET | State history (with `?filter_entity_id=...&end_time=...`) |
| `/api/logbook/{timestamp}` | GET | Logbook entries |
| `/api/error_log` | GET | Error log of the current session — **404 on HA OS/Supervised since 2025.11** (log file moved to journald; official docs are stale). Re-enable: `ha core options --duplicate-log-file=true` + `ha core rebuild` + `ha core restart` (2026.1+). Prefer WS `system_log/list` |
| `/api/calendars` | GET | All calendars |
| `/api/calendars/{entity_id}` | GET | Calendar events |
| `/api/services/calendar/create_event` | POST | Create a calendar event (feature bit 1; no recurrence field) |
| `/api/components` | GET | Loaded components |
| `/api/config/automation/config/{id}` | GET | Read automation config |
| `/api/config/automation/config/{id}` | POST | Create/update automation |
| `/api/config/automation/config/{id}` | DELETE | Delete automation |
| `/api/config/script/config/{id}` | GET | Read script config |
| `/api/config/script/config/{id}` | POST | Create/update script |
| `/api/config/script/config/{id}` | DELETE | Delete script |
| `/api/config/scene/config/{id}` | GET | Read storage-scene config (`{id}` is the registry `unique_id`, not the entity slug) |
| `/api/config/scene/config/{id}` | POST | Create/update storage scene |
| `/api/config/scene/config/{id}` | DELETE | Delete storage scene |
| `/api/events` | GET | List event types with total listener counts |
| `/api/config/config_entries/entry` | GET | List config entries (REST alternative to WS `config_entries/get`) |
| `/api/config/config_entries/entry/{id}/reload` | POST | Reload config entry |
| `/api/config/config_entries/flow_handlers` | GET | List config-flow handler domains |
| `/api/config/config_entries/flow` | POST | Start an integration/helper config flow |
| `/api/config/config_entries/flow/{flow_id}` | GET / POST / DELETE | Read, submit, or cancel one config flow |

**Auth:** callers go through the Relay (`ha-nova relay core`), which authenticates inbound with the Relay/device credential and injects the upstream HA credential server-side (`SUPERVISOR_TOKEN` on the App, `HA_LLAT` on the standalone container) — never send an HA token yourself. A raw `Authorization: Bearer {LONG_LIVED_TOKEN}` applies only to direct server-side use outside HA NOVA. The webhook endpoint itself is unauthenticated and treats its ID as the bearer secret; calls through HA NOVA still require Relay authentication.

### Runtime event and security notes

- Custom events use an exact user-defined event type. `GET /api/events` supplies total listener counts; automation-config scans identify only readable automation listeners.
- Automation webhooks default to POST/PUT and `local_only: true`. IDs are secrets, multiple triggers may share one ID, and HTTP 200 does not prove registration, locality, or handler success.
- Alarm-panel feature bits are home `1`, away `2`, night `4`, trigger `8`, custom bypass `16`, vacation `32`; disarm has no feature bit.
- `lock.open` requires feature bit `1`; `lock.lock` and `lock.unlock` have no feature bit. Code-bearing alarm/lock actions stay in the Home Assistant UI.

## WebSocket API (requires Relay as proxy)

### Registries (CRUD)
| WS Type | Purpose |
|---------|-----|
| `config/area_registry/list` | All areas |
| `config/area_registry/create` | Create area (`name`, `floor_id`, `icon`, `picture`, `aliases`) |
| `config/area_registry/update` | Update area metadata |
| `config/area_registry/delete` | Delete area |
| `config/floor_registry/list` | All floors |
| `config/floor_registry/create` | Create floor (`name`, `level`, `icon`, `aliases`) |
| `config/floor_registry/update` | Update floor metadata |
| `config/floor_registry/delete` | Delete floor |
| `config/label_registry/list` | All labels |
| `config/label_registry/create` | Create label (`name`, `color`, `icon`, `description`) |
| `config/label_registry/update` | Update label metadata |
| `config/label_registry/delete` | Delete label |
| `config/category_registry/list` | All categories for one `scope` |
| `config/category_registry/create` | Create category for one `scope` (`name`, `icon`) |
| `config/category_registry/update` | Update category metadata for one `scope` |
| `config/category_registry/delete` | Delete category for one `scope` |
| `config/entity_registry/list` | All entity registry entries |
| `config/entity_registry/get` | Single entity registry entry |
| `config/entity_registry/update` | Rename entity, aliases, labels, area, scoped `categories`, disable, hide |
| `config/entity_registry/remove` | Remove entity from registry |
| `config/device_registry/list` | All devices |
| `config/device_registry/update` | Assign device area, labels, name, disable |
| `config/device_registry/remove_config_entry` | Remove the owning device (HA 2026.8+; legacy command name) |

### Helper CRUD (storage-based, direct WS commands)
| WS Type Pattern | Supported types |
|-----------------|-------------------|
| `{type}/list` | input_boolean, input_number, input_text, input_datetime, input_select, input_button, counter, timer, schedule |
| `{type}/create` | Same types - creates helpers with type-specific params |
| `{type}/update` | Same types |
| `{type}/delete` | Same types (requires `unique_id`, not `entity_id`) |

### Config Entry Flow (for more complex helpers)
| Transport | Command / Path | Purpose |
|-----------|----------------|---------|
| WS | `config_entries/get` | Retrieve config-entry metadata |
| WS | `manifest/list` | Map integration domains to manifest names and config-flow capability |
| WS | `config_entries/flow/progress` | Retrieve in-progress flows, including `reauth` |
| `/core` | `GET /api/config/config_entries/flow_handlers` | List available config-flow handlers |
| `/core` | `POST /api/config/config_entries/flow` | Start flow |
| `/core` | `GET /api/config/config_entries/flow/{flow_id}` | Read current flow step |
| `/core` | `POST /api/config/config_entries/flow/{flow_id}` | Submit flow step |
| `/core` | `DELETE /api/config/config_entries/flow/{flow_id}` | Cancel an unfinished flow |
| `/core` | `POST /api/config/config_entries/options/flow` | Start options flow for an existing entry |
| `/core` | `GET /api/config/config_entries/options/flow/{flow_id}` | Read current options-flow step |
| `/core` | `POST /api/config/config_entries/options/flow/{flow_id}` | Submit options-flow step |
| `/core` | `DELETE /api/config/config_entries/options/flow/{flow_id}` | Cancel an unfinished options flow |
| `/core` | `DELETE /api/config/config_entries/entry/{entry_id}` | Delete config entry |

Observed locally on a real HA instance on 2026-03-19: raw WS `config_entries/flow` did not succeed in this session; relay `/core` returned the expected config-flow responses.

**Integration flow ownership:** `ha-nova:integration-setup` owns integration add, pending `reauth` continuation, invalid-credential recovery when no reauth flow is pending, options and reconfigure flows for existing entries, and config-entry reload; it uses the live response schema and hands secrets/external steps to the HA UI.

**Helper-owned config-entry domains:** utility_meter, derivative, integration, min_max, threshold, tod, statistics, group, history_stats, template, generic_thermostat, switch_as_x
`group` is menu-driven; the live-proven end-to-end subtype is `sensor`, and other subtypes must stay anchored to the live step schema instead of guessed fields.
**Fallback-owned flow helpers:** trend, random, filter, generic_hygrostat

### Dashboard / Lovelace
| WS Type | Purpose |
|---------|-----|
| `lovelace/dashboards/list` | List dashboards and URL paths |
| `lovelace/dashboards/create` | Create storage dashboard shell |
| `lovelace/dashboards/update` | Update dashboard metadata by `dashboard_id` |
| `lovelace/dashboards/delete` | Delete dashboard by `dashboard_id` |
| `lovelace/config` | Read dashboard config (URL path as parameter) |
| `lovelace/config/save` | Save dashboard config |
| `lovelace/config/delete` | Delete the selected dashboard config object by `url_path` (not the collection delete path used by `ha-nova:dashboard`) |
| `lovelace/resources` | List UI resources |
| `lovelace/resources/create` | Create UI resource (`res_type`, `url`) |
| `lovelace/resources/update` | Update UI resource by `resource_id` |
| `lovelace/resources/delete` | Delete UI resource by `resource_id` |
| `lovelace/info` | Global Lovelace resource mode |

### Recorder statistics
| WS Type | Purpose |
|---------|-----|
| `recorder/statistics_during_period` | Bounded long-term statistics for eligible entities |

### Energy
| WS Type | Purpose |
|---------|-----|
| `energy/get_prefs` | Read energy preferences |
| `energy/save_prefs` | Save energy preferences |
| `energy/info` | Energy info (cost sensors, solar forecast) |
| `energy/validate` | Validate energy config |
| `energy/solar_forecast` | Solar forecast data |
| `energy/fossil_energy_consumption` | Fossil consumption calculation |

### Calendar event writes
| WS Type | Purpose |
|---------|---------|
| `calendar/event/create` | Recurring event create with RFC 5545 `rrule` (feature bit 1); non-recurring creates use the REST service |
| `calendar/event/update` | Full-object event update by `entity_id` + `uid` (feature bit 4) |
| `calendar/event/delete` | Event delete by `entity_id` + `uid` (feature bit 2) |

Recurring instances add `recurrence_id`; `recurrence_range` is `""` for only that occurrence or `THISANDFUTURE` for that occurrence and later ones.

### Traces
| WS Type | Purpose |
|---------|-----|
| `trace/list` | List traces (`domain`, `item_id`) |
| `trace/get` | Read single trace (`domain`, `item_id`, `run_id`) |

### Misc
| WS Type | Purpose |
|---------|-----|
| `webhook/list` | Registered webhook IDs plus domain/name, locality, and allowed methods (secret-bearing; private `--out` file only, never stdout) |
| `repairs/list_issues` | Repairs/Deprecation Issues |
| `config_entries/get` | Config-entry metadata; health uses not-loaded entries as integration status |
| `system_health/info` | System health finite event response (Skill opts into Relay `collect_events` until `finish`) |
| `homeassistant/expose_entity/list` | Voice-Assistant Exposure |
| `subscribe_events` | Event subscription — bare calls are rejected by the Relay (`UNSUPPORTED_WS_TYPE`); permitted only inside the bounded `collect_events` envelope |
| `subscribe_trigger` | Trigger subscription — same envelope-only rule |
| `render_template` | Streaming render — rejected bare by the Relay; use `POST /api/template` instead |
| `blueprint/list` | List blueprints |
| `blueprint/import` | Import blueprint |
| `blueprint/save` | Save a blueprint (used by `ha-nova:fallback`) |
| `blueprint/substitute` | Expand a blueprint config (used by `ha-nova:fallback`) |

## Filesystem (via the Relay's opt-in `/files` endpoint)

File access defaults to off. When enabled, the Relay enforces a deny-list (`.storage`, `.cloud`, `.ssh`, `.git`, `deps`, `ssl`, `tts`, `backups`, `custom_components`, `python_scripts`, `www`, plus secret/db/env/log patterns) and a writable-extension gate (`.yaml` / `.yml` / `.conf` / `.json` / `.txt` / `.md`) — there is no directory whitelist. Keeping HA NOVA's own additions under `/config/ha_nova/` is a convention (`skills/yaml-config/SKILL.md`), not relay enforcement.

| Operation | Path | When needed |
|-----------|------|-----------|
| Template sensor YAML | `/config/ha_nova/templates/*.yaml` | For triggers, icon templates, multi-entity |
| REST sensor YAML | `/config/ha_nova/sensors/rest/*.yaml` | No config flow available |
| Command line sensor YAML | `/config/ha_nova/sensors/command_line/*.yaml` | No config flow available |
| Patch configuration.yaml | `/config/configuration.yaml` | For `!include_dir_merge_list` entries |

Raw backup file transfer (`/data/backups/`) is planned host-filesystem tooling — NOT reachable via `/files`, whose paths must stay inside `/config`. Backup lifecycle (status/create/inspect/delete) stays on WS `backup/*` (`ha-nova:backup`).

**Sensor types with config flow (no YAML needed):** SQL, Scrape, Template (limited)
**YAML-only sensor types:** REST, Command Line

## Surfaces the skills pin (grouped)

This document routes an operation to its transport. It is not a command
inventory — `skills/ha-nova/relay-api.md` holds the calling contract and each
skill holds its own payloads. The families below are pinned somewhere in
`skills/**` and all travel over `/ws` unless noted; they are listed so a reader
can tell "exists and is used" from "not available".
A command Home Assistant offers but no skill pins yet does NOT belong here —
listing it would route a request to an owner with no payload and no
guardrails.

| Family | Commands | Owning skill |
|---|---|---|
| Relations | `search/related` — the pre-delete and consumer-scan workhorse | the skills that pin it: `write`, `helper`, `scene`, `organize`, `read`, `review`, `service-call`, `entity-discovery`, `maintenance`, `admin`, `media`, `camera`, `todo`, `hacs`, `fallback` |
| Compact registry | `config/entity_registry/list_for_display` (abbreviated keys) | entity-discovery, review, bulk flows |
| System log | `system_log/list` (the working log source on HA OS) | diagnose |
| Core state | `get_states` (full state dump; prefer `config/entity_registry/list_for_display` for listings) | ha-nova (`relay-api.md`), used across read paths |
| Backups | `backup/info`, `backup/generate`, `backup/generate_with_automatic_settings`, `backup/delete`, `backup/agents/info`, `backup/details`, `backup/config/info` | backup |
| People & access | `person/*`, `zone/*`, `tag/*`, `config/auth/list`, `config/auth/create`, `config/auth/delete`, `auth/current_user` | admin |
| Voice | `assist_pipeline/pipeline/list`, `assist_pipeline/pipeline/create`, `assist_pipeline/pipeline/update`, `assist_pipeline/pipeline/delete`, `assist_pipeline/pipeline/set_preferred`, `homeassistant/expose_entity` and `homeassistant/expose_entity/list`, engine inventories `tts/engine/list`, `stt/engine/list`, `conversation/agent/list`, `wake_word/info`. `assist_pipeline/run` is pinned as NOT usable via the Relay (streaming audio subscription); utterance tests go through REST `/api/conversation/process` | assist |
| Recorder repair | `recorder/info`, `recorder/list_statistic_ids`, `recorder/get_statistics_metadata`, `recorder/validate_statistics`, `recorder/clear_statistics`, `recorder/update_statistics_metadata`, `recorder/change_statistics_unit`, `recorder/adjust_sum_statistics` | maintenance |
| Device automation | `device_automation/trigger/list`, `device_automation/trigger/capabilities` | write |
| MQTT | `mqtt/subscribe` (envelope only), `mqtt/device/debug_info` | mqtt |
| Media | `media_player/browse_media`, `media_player/search_media`, `media_source/browse_media`, `media_source/search_media`, `media_source/resolve_media` | media |
| Camera | `camera/stream`; REST `/api/camera_proxy/<entity_id>` (binary) | camera |
| Notifications | `persistent_notification/get` | notify |
| Updates | `update/release_notes` | updates |
| Logger | `logger/log_info` (numeric levels; `logger.set_level` takes names) | diagnose |
| Diagnostics | `diagnostics/list`; REST `/api/diagnostics/config_entry/<entry_id>[/device/<device_id>]` | diagnose |
| To-do | `todo/item/move`; REST `/api/services/todo/*?return_response` | todo |
| Conversation | REST `/api/conversation/process` (executes what it understands) | assist |
| Thread / Matter | `otbr/info`, `thread/list_datasets` (dataset is a network credential — `--out` file only), `matter/node_diagnostics` (takes `device_id`) | fallback |
| Custom-integration config APIs | REST `/api/<integration>/<resource>` (GET/POST; private, version-dependent — Alarmo, Scheduler, Frigate, ...) | fallback |
| HACS | `hacs/*` (17 commands) — not a Home Assistant API; the pinned map lives in `skills/hacs/hacs-commands.md` | hacs |

Out of scope: InfluxDB paths (`/api/v2/query`, `/api/v3/query_sql`) pinned in
`skills/external-sources` are not Home Assistant APIs. Supervisor paths
(`/api/hassio/*`) are pinned only as unreachable through the Relay (HTTP 403);
App control goes through ordinary services (`ha-nova:service-call`).

Completeness: rebuilt 2026-08-19 against `skills/**/*.md` (issue #517).

## Important Notes

- **Automation/Script REST API** is undocumented but stable (used by the HA frontend)
- **Legacy template sensors** (`sensor:` + `platform: template`) were deprecated in 2025.12 and removed in HA 2026.6
- **Helper delete** requires `unique_id`, not `entity_id`
- **Dashboard write/delete eligibility** comes from `lovelace/dashboards/list` (`mode=storage`), not from `lovelace/info`
- **Dashboard content writes are full-document saves** and must preserve unrelated views/cards
- **Lovelace resources have dedicated CRUD** via `lovelace/resources|create|update|delete`
- **Category CRUD is scope-based** and entity category assignment uses `config/entity_registry/update` with a `categories` map
  - every `config/category_registry/*` call must include `scope`
  - set one scope: `{"categories":{"<scope>":"<category_id>"}}`
  - clear one scope: `{"categories":{"<scope>":null}}`
  - do not rely on `{"categories":{}}` to clear an existing scoped category
- **Long-term trends use recorder statistics**, not wide history scans, when the question spans many days
- **Services** can be called with `?return_response` for response data
- **ETag caching** for `/api/services` saves bandwidth on repeated calls
