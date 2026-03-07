---
name: guide
description: Use for Home Assistant guidance on dashboards, blueprints, history, logbook, areas, zones, labels, energy, calendars, entity registry, system health, add-ons, HACS, and Zigbee/Z-Wave -- features beyond the core ha-nova automation/helper/service skills.
---

# HA NOVA Guide


## Scope

Fallback guidance for HA features without a dedicated skill. Three tiers:

- **Relay-Ready**: API works via Relay, no skill yet -- provide experimental relay calls + web search.
- **Roadmap**: Planned for future Relay phases -- explain timeline + workaround.
- **External**: Outside HA NOVA scope -- web search + HA UI pointer.

No relay calls of its own. Pure guidance skill -- all relay examples are experimental.

## Bootstrap (once per session)

Verify relay CLI: `~/.config/ha-nova/relay health`
If this fails: `npm run onboarding:macos`

## Capability Map

| Feature | Status | Skill |
|---------|--------|-------|
| Automations CRUD | Covered | read / write |
| Scripts CRUD | Covered | read / write |
| Config Review | Covered | review |
| Helpers (9 storage types) | Covered | helper |
| Entity Search | Covered | entity-discovery |
| Service Calls | Covered | service-call |
| Relay Setup | Covered | onboarding |
| Dashboard / Lovelace | Relay-Ready | this skill |
| Blueprints | Relay-Ready | this skill |
| History Queries | Relay-Ready | this skill |
| Logbook Queries | Relay-Ready | this skill |
| Area / Floor CRUD | Relay-Ready | this skill |
| Label / Category CRUD | Relay-Ready | this skill |
| Zone / Person / Tag Mgmt | Relay-Ready | this skill |
| Energy Configuration | Relay-Ready | this skill |
| System Health / Repairs | Relay-Ready | this skill |
| Calendar Queries | Relay-Ready | this skill |
| Config-Entry Helpers | Relay-Ready | this skill |
| Entity Registry Edits | Relay-Ready | this skill |
| Event Subscriptions | Roadmap Phase 1c | -- |
| Template / YAML Sensors | Roadmap Phase 3 | -- |
| Configuration Backups | Roadmap Phase 2 | -- |
| Add-ons / Supervisor | External | -- |
| HACS | External | -- |
| Zigbee / Z-Wave Config | External | -- |

## Agent Flow

```
1. Check Capability Map for user's request
2. If "Covered" -> STOP, use the listed skill instead
3. If "Relay-Ready":
   a. Show experimental relay call examples (with warning)
   b. Preview before any write
   c. Search web with provided query for current best practices
4. If "Roadmap":
   a. Explain which phase and what blocks it
   b. Search web for manual workaround
   c. Suggest HA UI as interim solution
5. If "External":
   a. Explain why it's outside HA NOVA scope
   b. Search web for how to do it directly in HA
   c. Point to HA UI path
```

## Relay-Ready Features

### Dashboard / Lovelace -- RELAY-READY

View and edit Lovelace dashboard configurations (views, cards, themes).

**Search:** `home assistant lovelace dashboard yaml api ws editing 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# Read dashboard config
~/.config/ha-nova/relay ws -d '{"type":"lovelace/config","url_path":"lovelace"}'

# Dashboard info
~/.config/ha-nova/relay ws -d '{"type":"lovelace/info"}'

# Save dashboard config
~/.config/ha-nova/relay ws -d '{"type":"lovelace/config/save","url_path":"lovelace","config":{"views":[...]}}'
```

**Risks:** `lovelace/config/save` overwrites the ENTIRE dashboard config. Always read first and merge changes.

### Blueprints -- RELAY-READY

List and import automation/script blueprints from the community or custom URLs.

**Search:** `home assistant blueprint import automation api 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# List blueprints
~/.config/ha-nova/relay ws -d '{"type":"blueprint/list","domain":"automation"}'

# Import blueprint
~/.config/ha-nova/relay ws -d '{"type":"blueprint/import","url":"https://...","domain":"automation"}'
```

**Risks:** Imported blueprints execute when instantiated. Review blueprint source before import.

### History Queries -- RELAY-READY

Query past state changes for any entity within a time range.

**Search:** `home assistant history api rest states period filter 2026`

**Experimental relay calls (no skill guardrails):**
```bash
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/history/period/2026-03-06T00:00:00Z?filter_entity_id=sensor.temperature&end_time=2026-03-07T00:00:00Z"}'
```

**Risks:** None (read-only). Large time ranges may return very large responses.

### Logbook Queries -- RELAY-READY

Query the logbook for human-readable event entries (state changes, automations fired).

**Search:** `home assistant logbook api rest query filter 2026`

**Experimental relay calls (no skill guardrails):**
```bash
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/logbook/2026-03-06T00:00:00Z?entity=light.living_room&end_time=2026-03-07T00:00:00Z"}'
```

**Risks:** None (read-only).

### Area / Floor CRUD -- RELAY-READY

Create, rename, or delete areas and floors used to organize devices and entities.

**Search:** `home assistant area floor registry api websocket crud 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# List areas
~/.config/ha-nova/relay ws -d '{"type":"config/area_registry/list"}'

# Create area
~/.config/ha-nova/relay ws -d '{"type":"config/area_registry/create","name":"Office","icon":"mdi:desk"}'

# Update area
~/.config/ha-nova/relay ws -d '{"type":"config/area_registry/update","area_id":"office","name":"Home Office"}'

# Delete area
~/.config/ha-nova/relay ws -d '{"type":"config/area_registry/delete","area_id":"office"}'

# List floors
~/.config/ha-nova/relay ws -d '{"type":"config/floor_registry/list"}'

# Create floor
~/.config/ha-nova/relay ws -d '{"type":"config/floor_registry/create","name":"Ground Floor","icon":"mdi:home-floor-g"}'
```

**Risks:** Deleting an area unassigns all devices/entities. Confirm before delete.

### Label / Category CRUD -- RELAY-READY

Manage labels and categories for organizing entities and automations.

**Search:** `home assistant label category registry api websocket 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# List labels
~/.config/ha-nova/relay ws -d '{"type":"config/label_registry/list"}'

# Create label
~/.config/ha-nova/relay ws -d '{"type":"config/label_registry/create","name":"Critical","icon":"mdi:alert","color":"red"}'

# List categories
~/.config/ha-nova/relay ws -d '{"type":"config/category_registry/list","scope":"automation"}'
```

**Risks:** Minimal. Labels/categories are metadata only.

### Zone / Person / Tag Management -- RELAY-READY

Manage location zones, person entities, and NFC/QR tags.

**Search:** `home assistant zone person tag management api websocket 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# List zones
~/.config/ha-nova/relay ws -d '{"type":"zone/list"}'

# Create zone
~/.config/ha-nova/relay ws -d '{"type":"zone/create","name":"Work","latitude":48.1,"longitude":11.5,"radius":100,"icon":"mdi:briefcase"}'

# List persons
~/.config/ha-nova/relay ws -d '{"type":"person/list"}'

# List tags
~/.config/ha-nova/relay ws -d '{"type":"tag/list"}'
```

**Risks:** Zone changes affect presence detection automations. Person changes affect device trackers.

### Energy Configuration -- RELAY-READY

Configure energy dashboard sources (grid, solar, gas, water, individual devices).

**Search:** `home assistant energy dashboard configuration api preferences 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# Read preferences
~/.config/ha-nova/relay ws -d '{"type":"energy/get_prefs"}'

# Validate config
~/.config/ha-nova/relay ws -d '{"type":"energy/validate"}'

# Save preferences
~/.config/ha-nova/relay ws -d '{"type":"energy/save_prefs","energy_sources":[...],"device_consumption":[...]}'
```

**Risks:** Incorrect energy source config breaks the energy dashboard. Validate before saving.

### System Health / Repairs -- RELAY-READY

Check system health status and view deprecation/repair issues.

**Search:** `home assistant system health repairs issues api 2026`

**Experimental relay calls (no skill guardrails):**
```bash
~/.config/ha-nova/relay ws -d '{"type":"repairs/list_issues"}'
```

**Risks:** None (read-only). Note: `system_health/info` uses a subscription pattern -- call via relay returns first response only.

### Calendar Queries -- RELAY-READY

List calendars and query upcoming events.

**Search:** `home assistant calendar api rest events query 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# List calendars
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/calendars"}'

# Query events
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/calendars/calendar.home?start=2026-03-07T00:00:00Z&end=2026-03-14T00:00:00Z"}'
```

**Risks:** None (read-only).

### Config-Entry Helpers -- RELAY-READY

Create complex helper types that require multi-step config flows (template sensors, groups, utility meters, etc.).

**Search:** `home assistant config entry flow helper template group utility_meter api 2026`

**Supported types:** template, group, utility_meter, derivative, min_max, threshold, integration, statistics, trend, random, filter, tod, generic_thermostat, switch_as_x, generic_hygrostat

**Experimental relay calls (no skill guardrails):**
```bash
# Start flow
~/.config/ha-nova/relay ws -d '{"type":"config_entries/flow","handler":"template","show_advanced_options":true}'

# Submit step (use fields from previous response's data_schema)
~/.config/ha-nova/relay ws -d '{"type":"config_entries/flow/{flow_id}","name":"My Template Sensor","state":"{{ states(\"sensor.x\") }}"}'
```

**Risks:** Multi-step flows are complex. Each step returns the next step's schema. Easy to get wrong. Prefer HA UI for these.

### Entity Registry Edits -- RELAY-READY

Rename entities, change area/label assignments, disable or hide entities.

**Search:** `home assistant entity registry update rename disable api 2026`

**Experimental relay calls (no skill guardrails):**
```bash
# Get entity
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/get","entity_id":"light.living_room"}'

# Update entity
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/update","entity_id":"light.living_room","name":"Wohnzimmer Licht","area_id":"living_room"}'

# Remove entity (irreversible)
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/remove","entity_id":"light.old_device"}'
```

**Risks:** `remove` is irreversible -- entity disappears from registry. `update` with `disabled_by: "user"` is reversible.

## Roadmap Features

### Event Subscriptions -- ROADMAP (Phase 1c)

Real-time event streams for state changes, automation triggers, and custom events.

**Search:** `home assistant event subscription state_changed real time api 2026`

**Status:** Coming in Phase 1c. Blocked by: No SSE streaming endpoint in Relay.
**Workaround:** Poll entity state periodically via `GET /api/states/{entity_id}`.

### Template / REST / Command-Line Sensors -- ROADMAP (Phase 3)

Define custom sensors using Jinja2 templates, REST endpoints, or shell commands.

**Search:** `home assistant template sensor yaml configuration 2026`

**Status:** Coming in Phase 3. Blocked by: No filesystem access in Relay (sensors defined in YAML files).
**Workaround:** Create via HA UI: Settings > Devices & Services > Helpers (for template helpers) or add YAML manually.

### Configuration Backups -- ROADMAP (Phase 2)

Create and restore full configuration backups.

**Search:** `home assistant backup restore api supervisor 2026`

**Status:** Coming in Phase 2. Blocked by: No backup endpoint in Relay.
**Workaround:** Use HA UI: Settings > System > Backups. Or use Supervisor API if HA OS.

## External Features

### Add-ons / Supervisor Management -- EXTERNAL

Supervisor API is separate from HA Core API. Requires different auth and endpoints.

**Search:** `home assistant supervisor addon install manage api 2026`

**Alternative:** HA UI: Settings > Add-ons. Or `ha` CLI on HA OS.

### HACS (Home Assistant Community Store) -- EXTERNAL

Third-party integration with no stable public API.

**Search:** `home assistant hacs install custom integration repository 2026`

**Alternative:** HACS sidebar panel in HA UI.

### Zigbee / Z-Wave / Network Configuration -- EXTERNAL

Device pairing is hardware-specific, requires direct coordinator access (ZHA, Z2M, Z-Wave JS).

**Search:** `home assistant zigbee2mqtt zha device pairing configuration 2026`

**Alternative:** HA UI: Settings > Devices & Services > [Zigbee/Z-Wave integration].

## Safety Guardrails

Rules for all experimental relay calls in this skill:

- Always preview the full payload before execution
- Read before write: fetch current state first for any destructive operation
- Every experimental call must show: "EXPERIMENTAL: No skill guardrails. Proceed with caution."
- One resource at a time (no batch writes)
- Delete requires tokenized confirmation (`confirm:<token>`)
- Never guess IDs: resolve via list/search first
- If unsure about payload schema, search web first

## Error Handling

Experimental calls may fail with unfamiliar errors. General rules:

- `400/VALIDATION_ERROR`: payload schema wrong -- search web for current WS type schema
- `404/NOT_FOUND`: endpoint may not exist in this HA version -- check HA release notes
- `502/504`: relay connection issue -- retry once, then route to `ha-nova:onboarding`
