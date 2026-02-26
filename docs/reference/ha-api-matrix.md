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
| `/api/error_log` | GET | Error log of the current session |
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
| `config/area_registry/create` | Create area (`name`, `icon`, `floor_id`, `labels`) |
| `config/area_registry/update` | Update area |
| `config/area_registry/delete` | Delete area |
| `config/floor_registry/list` | All floors |
| `config/floor_registry/create` | Create floor |
| `config/floor_registry/update` | Update floor |
| `config/floor_registry/delete` | Delete floor |
| `config/label_registry/list` | All labels |
| `config/label_registry/create` | Create label |
| `config/label_registry/update` | Update label |
| `config/label_registry/delete` | Delete label |
| `config/category_registry/list` | All categories |
| `config/category_registry/create` | Create category |
| `config/category_registry/update` | Update category |
| `config/category_registry/delete` | Delete category |
| `config/entity_registry/list` | All entity registry entries |
| `config/entity_registry/get` | Single entity registry entry |
| `config/entity_registry/update` | Rename entity, labels, area, disable, hide |
| `config/entity_registry/remove` | Remove entity from registry |
| `config/device_registry/list` | All devices |
| `config/device_registry/update` | Assign device area, labels, name |

### Helper CRUD (storage-based, direct WS commands)
| WS Type Pattern | Supported types |
|-----------------|-------------------|
| `{type}/list` | input_boolean, input_number, input_text, input_datetime, input_select, input_button, counter, timer, schedule, zone, person, tag |
| `{type}/create` | Same types - creates helpers with type-specific params |
| `{type}/update` | Same types |
| `{type}/delete` | Same types (requires `unique_id`, not `entity_id`) |

### Config Entry Flow (for more complex helpers)
| WS Type | Purpose |
|---------|-----|
| `config_entries/flow` | Start flow (handler = domain, e.g. "template") |
| `config_entries/flow/{flow_id}` | Submit flow step |
| `config_entries/get` | Retrieve config entry |
| `config_entries/delete` | Delete config entry |

**Supported flow helpers:** template, group, utility_meter, derivative, min_max, threshold, integration, statistics, trend, random, filter, tod, generic_thermostat, switch_as_x, generic_hygrostat

### Dashboard / Lovelace
| WS Type | Purpose |
|---------|-----|
| `lovelace/config` | Read dashboard config (URL path as parameter) |
| `lovelace/config/save` | Save dashboard config |
| `lovelace/config/delete` | Delete dashboard |
| `lovelace/resources` | List UI resources |
| `lovelace/info` | Dashboard info |

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
| `homeassistant/expose_entity/list` | Voice-Assistant Exposure |
| `subscribe_events` | Event subscription (real time) |
| `subscribe_trigger` | Trigger subscription |
| `system_health/info` | System health (subscription pattern) |
| `blueprint/list` | List blueprints |
| `blueprint/import` | Import blueprint |

## Filesystem (only from the HA host)

| Operation | Path | When needed |
|-----------|------|-----------|
| Template sensor YAML | `/config/ha_mcp/templates/*.yaml` | For triggers, icon templates, multi-entity |
| REST sensor YAML | `/config/ha_mcp/sensors/rest/*.yaml` | No config flow available |
| Command line sensor YAML | `/config/ha_mcp/sensors/command_line/*.yaml` | No config flow available |
| Patch configuration.yaml | `/config/configuration.yaml` | For `!include_dir_merge_list` entries |
| Backups | `/data/backups/` | Configuration backups before changes |

**Sensor types with config flow (no YAML needed):** SQL, Scrape, Template (limited)
**YAML-only sensor types:** REST, Command Line

## Important Notes

- **Automation/Script REST API** is undocumented but stable (used by the HA frontend)
- **Legacy template sensors** (`sensor:` + `platform: template`) are deprecated since 2025.12, end in 2026.6
- **Helper delete** requires `unique_id`, not `entity_id`
- **Services** can be called with `?return_response` for response data
- **ETag caching** for `/api/services` saves bandwidth on repeated calls
