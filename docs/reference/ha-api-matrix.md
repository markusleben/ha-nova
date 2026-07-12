# Home Assistant API Matrix

Which HA operations require REST, WS, or filesystem?

## REST API (directly with Long-Lived Access Token)

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
| `/api/events/{event_type}` | POST | Fire custom event |
| `/api/webhook/{webhook_id}` | POST | Invoke webhook |
| `/api/history/period/{start_iso}` | GET | State history (with `?filter_entity_id=...&end_time=...`) |
| `/api/logbook/{timestamp}` | GET | Logbook entries |
| `/api/error_log` | GET | Error log of the current session — **404 on HA OS/Supervised since 2025.11** (log file moved to journald; official docs are stale). Re-enable: `ha core options --duplicate-log-file=true` + `ha core rebuild` (2026.1+). Prefer WS `system_log/list` |
| `/api/calendars` | GET | All calendars |
| `/api/calendars/{entity_id}` | GET | Calendar events |
| `/api/components` | GET | Loaded components |
| `/api/config/automation/config/{id}` | GET | Read automation config |
| `/api/config/automation/config/{id}` | POST | Create/update automation |
| `/api/config/automation/config/{id}` | DELETE | Delete automation |
| `/api/config/script/config/{id}` | GET | Read script config |
| `/api/config/script/config/{id}` | POST | Create/update script |
| `/api/config/script/config/{id}` | DELETE | Delete script |
| `/api/config/config_entries/entry/{id}/reload` | POST | Reload config entry |

**Auth header:** `Authorization: Bearer {LONG_LIVED_TOKEN}`

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
| `config/device_registry/remove_config_entry` | Detach config entry from device |

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
| `/core` | `POST /api/config/config_entries/flow` | Start flow |
| `/core` | `POST /api/config/config_entries/flow/{flow_id}` | Submit flow step |
| `/core` | `POST /api/config/config_entries/options/flow` | Start options flow for an existing entry |
| `/core` | `POST /api/config/config_entries/options/flow/{flow_id}` | Submit options-flow step |
| `/core` | `DELETE /api/config/config_entries/entry/{entry_id}` | Delete config entry |

Observed locally on a real HA instance on 2026-03-19: raw WS `config_entries/flow` did not succeed in this session; relay `/core` returned the expected config-flow responses.

**Helper-owned config-entry domains:** utility_meter, derivative, integration, min_max, threshold, tod, statistics, group, history_stats, template
`group` is menu-driven; the live-proven end-to-end subtype is `sensor`, and other subtypes must stay anchored to the live step schema instead of guessed fields.
**Fallback-owned flow helpers:** trend, random, filter, generic_thermostat, switch_as_x, generic_hygrostat

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

### Traces
| WS Type | Purpose |
|---------|-----|
| `trace/list` | List traces (`domain`, `item_id`) |
| `trace/get` | Read single trace (`domain`, `item_id`, `run_id`) |

### Misc
| WS Type | Purpose |
|---------|-----|
| `config/automation/list` | Automations with metadata |
| `repairs/list_issues` | Repairs/Deprecation Issues |
| `config_entries/get` | Config-entry metadata; health uses not-loaded entries as integration status |
| `system_health/info` | System health finite event response (Skill opts into Relay `collect_events` until `finish`) |
| `homeassistant/expose_entity/list` | Voice-Assistant Exposure |
| `subscribe_events` | Event subscription (real time) |
| `subscribe_trigger` | Trigger subscription |
| `blueprint/list` | List blueprints |
| `blueprint/import` | Import blueprint |

## Filesystem (only from the HA host)

| Operation | Path | When needed |
|-----------|------|-----------|
| Template sensor YAML | `/config/ha_mcp/templates/*.yaml` | For triggers, icon templates, multi-entity |
| REST sensor YAML | `/config/ha_mcp/sensors/rest/*.yaml` | No config flow available |
| Command line sensor YAML | `/config/ha_mcp/sensors/command_line/*.yaml` | No config flow available |
| Patch configuration.yaml | `/config/configuration.yaml` | For `!include_dir_merge_list` entries |
| Backups (raw file download/upload) | `/data/backups/` | Planned; lifecycle (status/create/inspect/delete) is covered via `/ws` `backup/*` (`ha-nova:backup`) |

**Sensor types with config flow (no YAML needed):** SQL, Scrape, Template (limited)
**YAML-only sensor types:** REST, Command Line

## Important Notes

- **Automation/Script REST API** is undocumented but stable (used by the HA frontend)
- **Legacy template sensors** (`sensor:` + `platform: template`) are deprecated since 2025.12, end in 2026.6
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
