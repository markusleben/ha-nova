# HA Nova Skill Architecture

## Skill Hierarchy

```
ha-nova.md (Bootstrap — ALWAYS loaded, ~4KB)
  │
  ├── Routing: Which sub-skill for which task?
  ├── API table: What goes directly to HA REST vs. Relay?
  ├── Basic safety rules
  └── Skill catalog with examples
  │
  ├── ha-onboarding.md (only on first run)
  │
  ├── Tier 1: Domain skills (loaded as needed)
  │   ├── ha-automation-crud.md
  │   ├── ha-automation-control.md
  │   ├── ha-automation-trace.md
  │   ├── ha-automation-validate.md
  │   ├── ha-automation-diagnose.md
  │   ├── ha-automation-batch.md
  │   ├── ha-automation-dependency.md
  │   ├── ha-automation-blueprint.md
  │   ├── ha-script-crud.md
  │   ├── ha-script-validate.md
  │   ├── ha-control.md
  │   ├── ha-entities.md
  │   ├── ha-entity-registry.md
  │   ├── ha-helpers.md
  │   ├── ha-organizer.md
  │   ├── ha-dashboard.md
  │   ├── ha-energy.md
  │   ├── ha-integrations.md
  │   ├── ha-system.md
  │   ├── ha-template-yaml.md
  │   ├── ha-sensor-yaml.md
  │   ├── ha-events.md
  │   ├── ha-files.md
  │   ├── ha-backup.md
  │   ├── ha-repairs.md
  │   ├── ha-scene.md
  │   ├── ha-analytics.md
  │   └── ha-playbook.md
  │
  └── Tier 2: Cross-cutting skills (added as needed)
      ├── ha-best-practices.md (70 rules)
      ├── ha-safety.md (Write-Safety, Consent)
      └── ha-diagnose.md (diagnostic process)
```

## Bootstrap Skill Tasks

The bootstrap skill `ha-nova.md` is the core. It is ALWAYS loaded and has three jobs:

### 1. Routing
Identifies the correct domain from the user request and loads the matching sub-skill.

**Decision tree:**
```
User wants to...
├── control something → ha-control
├── find something/check status → ha-entities
├── create/change automation → ha-automation-crud + ha-safety
├── check/diagnose automation → ha-automation-diagnose + ha-best-practices
├── create script → ha-script-crud + ha-safety
├── create helper → ha-helpers + ha-safety
├── organize areas/labels → ha-organizer
├── change dashboard → ha-dashboard + ha-safety
├── create sensor → ha-template-yaml OR ha-sensor-yaml + ha-safety
├── system info/reload → ha-system
├── watch events → ha-events
├── manage integrations → ha-integrations
├── inspect repairs → ha-repairs
├── configure energy → ha-energy
├── create backup → ha-backup
├── inspect files → ha-files
├── create scene → ha-scene + ha-safety
├── analysis/report → ha-analytics
├── complex workflow → ha-playbook
└── unclear → ask follow-up questions
```

### 2. API Routing
Tells the LLM which requests go where:

| Operation | Target |
|-----------|------|
| Entity status | `GET {HA_URL}/api/states` |
| Call service | `POST {HA_URL}/api/services/{domain}/{service}` |
| Automation CRUD | `GET/POST/DELETE {HA_URL}/api/config/automation/config/{id}` |
| Script CRUD | `GET/POST/DELETE {HA_URL}/api/config/script/config/{id}` |
| History | `GET {HA_URL}/api/history/period/{timestamp}` |
| Render template | `POST {HA_URL}/api/template` |
| WS operations | `POST {BRIDGE_URL}/ws` |
| Files | `POST {BRIDGE_URL}/files` |
| Backups | `POST {BRIDGE_URL}/backups` |

### 3. Basic Safety Rules
- Backup before write
- Preview before execute
- Sequential execution for batch ops
- Never guess entity IDs
- Explain errors

## Context Budget

| Component | Tokens |
|-----------|--------|
| Bootstrap (ha-nova.md) | ~3.000 |
| 1 Domain skill | ~2.000-4.000 |
| 1 Cross-Cutting Skill | ~2.000-4.000 |
| **Typical session** | **~7.000-11.000** |

With 200K context: >189K for conversation and API responses.

## Onboarding Skill

`ha-onboarding.md` is loaded only on first contact:

1. **Check connection:**
   - `GET {HA_URL}/api/` → "API running"
   - `GET {BRIDGE_URL}/health` → "ok"
   - `GET {HA_URL}/api/config` → HA version

2. **Offer quick wins:**
   - "Show me all lights"
   - "Which sensors are offline?"
   - "Turn on X"

3. **Error handling:**
   - URL not reachable → check network
   - 401 → check token
   - Relay down → check App logs

## Skill Interaction Example

```
User: "Create an automation: When motion is detected in the hallway, turn on the light for 3 minutes"

1. Bootstrap (ha-nova.md) identifies: create automation
2. Loads: ha-automation-crud + ha-safety + ha-entities

3. ha-entities: find motion sensor
   → GET /api/states → filter binary_sensor.*motion*flur*
   → Found: binary_sensor.flur_motion

4. ha-entities: find light
   → GET /api/states → filter light.*flur*
   → Found: light.flur_decke

5. ha-automation-crud: assemble config
   → trigger: state, binary_sensor.flur_motion, to: "on"
   → action: light.turn_on, light.flur_decke
   → action: delay 00:03:00
   → action: light.turn_off, light.flur_decke

6. ha-safety: show preview
   → "I would create this automation: [Config]. Should I continue?"

7. User: "Yes"

8. ha-automation-crud: create
   → POST /api/config/automation/config/flur_motion_light
   → POST /api/services/automation/reload
   → GET /api/states/automation.flur_motion_light → state: "on" ✓
```

## Phase 1 Skills (built first)

### Phase 1a (Week 1-2)
- `ha-nova.md` — Bootstrap
- `ha-onboarding.md` — Onboarding
- `ha-safety.md` — Write-Safety

### Phase 1b (Week 2-3)
- `ha-automation-crud.md` — Automation CRUD
- `ha-automation-control.md` — Automation On/Off/Toggle
- `ha-control.md` — Device control
- `ha-entities.md` — Entity search

### Phase 1c (Week 4-5)
- `ha-automation-trace.md` — Trace analysis
- `ha-automation-validate.md` — Basic validation (Top-10 Rules)
- `ha-automation-verify.md` — Trigger test
