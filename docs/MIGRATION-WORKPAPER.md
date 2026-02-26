# Workpaper: Migration MCP Server → **ha-nova** (Thin Relay + Skills)

## Status Quo

- **88,160 lines of TypeScript** in 679 files
- 18 manager tools with **200+ actions**
- 70 best-practice rules, dependency tracking, consent gating, batch ops
- Runs as an HA App container on the host

## Core Idea

The **intelligence** (best practices, diagnostics, workflows, validation) moves into **LLM skills**.
The **infrastructure** (WS proxy, filesystem, backups) remains as a slim **API Relay**.
The LLM talks **directly** to the HA REST API for ~60% of all operations.

```
                HA REST API (native)
                /api/states, /api/services, ...
                     │
  LLM + Skill ──────┤
                     │
                Thin API Relay (App, Port 8791)
                  POST /ws          → WS proxy
                  POST /files       → YAML read/write
                  POST /backups     → Backup CRUD
                  GET  /health
```

**Result: ~5,000-8,000 lines** instead of 88,160 (91-94% reduction)

---

## What can a skill do, and what can it not do?

### Skill can do (LLM does it directly):
- Call HA REST API (states, services, automation/script CRUD, history, templates)
- Apply best-practice rules (encoded as knowledge in the skill)
- Dependency analysis (read automations, find references)
- Diagnostics (read traces, detect oscillation, identify conflicts)
- Orchestrate batch operations (backup → execute → verify → rollback)
- Consent/safety (LLM clients have their own permission systems)

### Skill cannot do (Relay required):
- Call WebSocket API (helper CRUD, area/floor/label registry, dashboards, energy, traces)
- Read/write YAML files on the HA host (REST/command-line sensors, template YAML)
- Create/load backups on the filesystem
- Hold persistent event subscriptions

---

## Relay: 5 endpoints instead of 200+ actions

### 1. `POST /ws` — Generic WebSocket proxy
```json
Request:  { "type": "config/area_registry/list" }
Response: { "ok": true, "data": [...] }
```
Covers: helper CRUD, registries, dashboards, energy, traces, repairs - everything that is WS-only.
Security: allowlist of ~30-50 permitted WS message types.

### 2. `POST /ws/subscribe` — Event subscription
```json
Request:  { "type": "subscribe_events", "event_type": "state_changed", "duration_sec": 30 }
Response: SSE stream with events
```
For event monitoring with timeout.

### 3. `POST /files` — Filesystem operations
```json
Request:  { "action": "read_file", "path": "/config/ha_mcp/templates/my_sensor.yaml" }
Response: { "ok": true, "content": "...", "bytes": 1234 }
```
Actions: `list_dir`, `read_file`, `write_file`, `delete_file`.
Security: path allowlist, symlink protection, no secrets.

### 4. `POST /backups` — Backup management
```json
Request:  { "action": "save", "category": "automations", "name": "before-cleanup", "data": "..." }
Response: { "ok": true, "file": "automations/before-cleanup-2026-02-25.json.gz" }
```
Actions: `list`, `save`, `load`, `prune`, `delete`.

### 5. `GET /health` — Health check
```json
Response: { "status": "ok", "ha_ws_connected": true, "version": "1.0.0" }
```

### Auth
Same long-lived access token as HA itself. No second credential.

### Tech Stack
- TypeScript/Node.js (code reuse from the existing project)
- No HTTP framework (Node.js `http.createServer` + 30-line router)
- Dependencies: `ws`, `yaml`, optional `zod`
- **Estimated: ~700 lines of core code**

---

## Project Name: **ha-nova**

> *Nova* - a new star. A project reset: from an 88k-line MCP server to a slim Relay + intelligent skills.

| Aspect | Name |
|--------|------|
| **Project** | `ha-nova` |
| **Skill package** | `ha-nova` (`claude skill install ha-nova`) |
| **Slash command** | `/ha-nova` |
| **Relay App** | "HA Nova Relay" |
| **GitHub repo** | `ha-nova` (or rename from `ha-mcp-addon`) |
| **npm package** | `ha-nova-relay` |

---

## Bootstrap Skill: `ha-nova.md`

The most important skill in the entire system. It is **always loaded** when the user works with Home Assistant and serves three functions:

1. **Orientation:** What can the system do? Which sub-skills exist?
2. **Routing:** Which sub-skill is needed for which task?
3. **Ground rules:** How do we operate safely? How is the infrastructure structured?

### Bootstrap Skill structure

```markdown
# HA Nova

You control and configure Home Assistant through two paths:
- **Direct:** HA REST API (states, services, automation/script CRUD, history)
- **Via Relay:** HA Relay App for WebSocket ops, filesystem, backups

## Connectivity

| Access | URL | Auth |
|--------|-----|------|
| HA REST API | {HA_URL}/api/* | Bearer {HA_TOKEN} |
| HA Relay | {RELAY_URL}/* | Bearer {HA_TOKEN} |

## What you can do - skill catalog

### Devices & Entities
| Task | Skill | Examples |
|------|-------|----------|
| Control device (light, switch, climate, ...) | `ha-control` | "Set living room light to 50%" |
| Search entities, read status | `ha-entities` | "Which sensors are in the kitchen?" |
| Rename entity, enable, labels | `ha-entity-registry` | "Rename sensor.temp" |

### Automations & Scripts
| Task | Skill | Examples |
|------|-------|----------|
| Create/update/delete automation | `ha-automation-crud` | "Create a motion-light automation" |
| Validate automation against best practices | `ha-automation-validate` | "Check my automations for issues" |
| Diagnose automation issues | `ha-automation-diagnose` | "Why is my automation not triggering?" |
| Analyze automation traces | `ha-automation-trace` | "Show me the latest trace" |
| Multiple automations at once | `ha-automation-batch` | "Update all light automations" |
| Create/update/delete script | `ha-script-crud` | "Create a good-morning script" |
| Validate script | `ha-script-validate` | "Check my script for errors" |

### Helpers & Organization
| Task | Skill | Examples |
|------|-------|----------|
| Manage input helpers (boolean, number, ...) | `ha-helpers` | "Create an input boolean for vacation mode" |
| Manage areas, floors, labels | `ha-organizer` | "Create attic area on floor 2" |

### Dashboard & Visualization
| Task | Skill | Examples |
|------|-------|----------|
| Manage dashboard/views/cards | `ha-dashboard` | "Add a temperature card" |
| Configure energy dashboard | `ha-energy` | "Configure solar tracking" |

### Sensors & Templates
| Task | Skill | Examples |
|------|-------|----------|
| Manage template sensors (YAML) | `ha-template-yaml` | "Create a template sensor for average temperature" |
| REST/CLI/SQL/scrape sensors | `ha-sensor-yaml` | "Create a REST sensor for weather API" |

### System & Infrastructure
| Task | Skill | Examples |
|------|-------|----------|
| System ops (reload, restart, logs, HACS) | `ha-system` | "Reload YAML configuration" |
| Manage integrations | `ha-integrations` | "Show all integrations" |
| Monitor events & history | `ha-events` | "Watch front-door state changes" |
| Read/write files on HA | `ha-files` | "Show me configuration.yaml" |
| Create/load backups | `ha-backup` | "Create backup before changes" |
| Repairs & deprecations | `ha-repairs` | "Which repairs are open?" |
| Manage scenes | `ha-scene` | "Create movie-night scene" |

### Cross-cutting knowledge
| Task | Skill | When to load |
|------|-------|--------------|
| Best practices (70 rules) | `ha-best-practices` | During validation, diagnostics, review |
| Write safety & consent | `ha-safety` | For every write operation |
| Diagnostic procedures | `ha-diagnose` | During troubleshooting and analysis |
| Analytics (19 domains) | `ha-analytics` | During analysis and reporting |
| Workflow recipes | `ha-playbook` | For complex multi-step tasks |

## Routing logic

When the user describes a task:

1. **Identify the domain** - entities, automations, helpers, dashboard, ...?
2. **Load the matching skill** - see catalog above
3. **For write operations** - also load `ha-safety`
4. **For diagnostics/review** - also load `ha-best-practices` and/or `ha-diagnose`
5. **If unclear** - ask the user what they mean

### Decision tree

User wants...
├── control something → `ha-control`
├── find/query something → `ha-entities`
├── create/update automation → `ha-automation-crud` + `ha-safety`
├── validate automation → `ha-automation-validate` + `ha-best-practices`
├── "Why does X not work?" → `ha-automation-diagnose` or `ha-diagnose`
├── create helper → `ha-helpers` + `ha-safety`
├── modify dashboard → `ha-dashboard` + `ha-safety`
├── create sensor → `ha-template-yaml` or `ha-sensor-yaml` + `ha-safety`
├── system info/reload → `ha-system`
├── multiple things at once → relevant skills + `ha-playbook`
└── unclear → ask follow-up

## Ground rules

### API routing
| Operation | Route |
|-----------|-------|
| Read entity states | `GET {HA_URL}/api/states` |
| Call service | `POST {HA_URL}/api/services/{domain}/{service}` |
| Automation CRUD | `GET/POST/DELETE {HA_URL}/api/config/automation/config/{id}` |
| Script CRUD | `GET/POST/DELETE {HA_URL}/api/config/script/config/{id}` |
| History | `GET {HA_URL}/api/history/period/{timestamp}` |
| Render template | `POST {HA_URL}/api/template` |
| Everything WS-only | `POST {RELAY_URL}/ws` with `{ "type": "..." }` |
| Files | `POST {RELAY_URL}/files` with `{ "action": "..." }` |
| Backups | `POST {RELAY_URL}/backups` with `{ "action": "..." }` |

### Safety ground rules
1. **Backup before write:** Before every write to automations/scripts/sensors
2. **Preview before execute:** Always show what changes before execution
3. **One step at a time:** Batch ops sequentially, verify after each step
4. **No guessing:** If entity IDs or service names are unclear, search/list first
5. **Explain errors:** Translate HA API errors into clear language
```

### Size and context budget

| Section | Estimated tokens |
|---------|------------------|
| Connectivity/config | ~200 |
| Skill catalog (all tables) | ~1,500 |
| Routing logic + decision tree | ~600 |
| API routing table | ~400 |
| Safety ground rules | ~300 |
| **Total bootstrap skill** | **~3,000 tokens (~4KB)** |

This leaves >195k tokens in a 200k context window for conversation, API responses, and sub-skills.

### Why the bootstrap skill is critical

| Problem without bootstrap | Solution with bootstrap |
|---------------------------|-------------------------|
| LLM does not know which skills exist | Complete catalog with examples |
| LLM does not know when each skill fits | Decision tree with concrete patterns |
| LLM calls the wrong API (REST vs WS vs Relay) | Clear routing table |
| LLM forgets safety rules | Ground rules always present |
| LLM loads too many skills at once | Routing logic: load only what is needed |
| User request fits no category | Fallback: ask instead of guessing |

### Interaction with sub-skills

```
User: "Create an automation that turns on the light when motion is detected"
                │
                ▼
    Bootstrap skill (always loaded)
    → Identifies: create automation
    → Loads: ha-automation-crud + ha-safety
    → Optional: ha-best-practices (for post-create validation)
                │
                ▼
    ha-automation-crud skill
    → Instructs LLM: REST API calls for automation creation
    → Defines: config structure, trigger types, action formats
                │
                ▼
    ha-safety skill
    → Instructs: backup before write, show preview, collect consent
                │
                ▼
    LLM executes:
    1. GET /api/states → find motion sensor
    2. Build automation config
    3. Show preview to user
    4. POST /api/config/automation/config/{id} → create
    5. POST /api/services/automation/reload → activate
    6. Verify: GET /api/states/automation.{id} → check state
```

---

## Skill Architecture: 29 files instead of 539

### Tier 0 - Always loaded (~4KB)
| Skill | Content |
|-------|---------|
| `ha-nova.md` | **Bootstrap:** skill catalog, routing, API routing, safety ground rules |

### Tier 1 - Loaded on demand (2-5KB each)
| Skill | Replaces manager | Transport |
|-------|------------------|-----------|
| `ha-control.md` | control | direct REST |
| `ha-entities.md` | entity_manager (read) | direct REST |
| `ha-entity-registry.md` | entity_manager (write) | Relay WS |
| `ha-automation-crud.md` | automation_manager (CRUD) | direct REST |
| `ha-automation-validate.md` | automation_manager (validation + 70 rules) | skill knowledge |
| `ha-automation-diagnose.md` | automation_manager (diagnostics) | REST + Relay WS |
| `ha-automation-trace.md` | automation_manager (traces) | Relay WS |
| `ha-automation-batch.md` | automation_manager (batch) | REST + Relay backup |
| `ha-script-crud.md` | script_manager | direct REST |
| `ha-script-validate.md` | script_manager (validation) | skill knowledge |
| `ha-helpers.md` | helper_manager | Relay WS |
| `ha-organizer.md` | organizer_manager | Relay WS |
| `ha-dashboard.md` | dashboard_manager | Relay WS |
| `ha-energy.md` | energy_manager | Relay WS |
| `ha-integrations.md` | integration_manager | REST + Relay WS |
| `ha-system.md` | system_manager | direct REST |
| `ha-template-yaml.md` | template_manager | Relay files |
| `ha-sensor-yaml.md` | sensor_manager | Relay files |
| `ha-events.md` | event_monitor | Relay WS subscribe |
| `ha-files.md` | file_manager | Relay files |
| `ha-backup.md` | backup_manager | Relay backups |
| `ha-repairs.md` | repairs_manager | Relay WS |
| `ha-scene.md` | scene_manager | direct REST |
| `ha-analytics.md` | (19 analytics modules) | direct REST |
| `ha-playbook.md` | playbook_manager | skill knowledge |

### Tier 2 - Cross-cutting knowledge (3-5KB each)
| Skill | Content |
|-------|---------|
| `ha-best-practices.md` | All 70 rules as check/why/fix |
| `ha-safety.md` | Write safety model, consent rules, backup-before-write |
| `ha-diagnose.md` | Generic diagnostic procedures (oscillation, writer conflicts, race conditions) |

---

## Migration in 4 Phases

### Phase 1: Foundation + Automation (4-5 weeks)

**Goal:** Build infrastructure and deliver the most important use case - automations. If automations work, everything works.

Phase 1 is split into three sub-phases. Each is independently shippable.

#### Phase 1a: Infrastructure + Onboarding (Week 1-2)

**What gets built:**

1. **Relay MVP** (~500 lines)
   - `POST /ws` - generic WS proxy with type allowlist
   - `GET /health` - health check (HA connectivity + WS status)
   - Auth: bearer token validation against HA
   - Auto-reload: trigger `automation/reload` automatically after config mutations

2. **Bootstrap skill** `ha-nova.md`
   - Skill catalog, routing logic, API table, safety ground rules
   - See "Bootstrap Skill" section above

3. **Safety skill** `ha-safety.md`
   - Backup-before-write rules
   - Preview-before-execute workflow
   - Consent patterns for different LLM clients

4. **Onboarding: zero to working in 5 minutes**

   Onboarding is critical - if a newcomer does not get a win within 5 minutes, we lose them.

   **What a new user must do:**

   ```
   Step 1: Install Relay App (1-click in HA App Store)
   Step 2: Create long-lived access token (HA profile → security)
   Step 3: Add values in the LLM client
   ```

   **For Claude Code:**
   ```bash
   # One time: install skill
   claude skill install ha-nova

   # In .claude/settings.json or project config:
   {
     "env": {
       "HA_URL": "http://homeassistant.local:8123",
       "HA_TOKEN": "<your-token>",
       "HA_RELAY_URL": "http://homeassistant.local:8791"
     }
   }
   ```

   **First success moment (< 1 minute after setup):**
   ```
   User: "Show me all lights in the living room"
   → Skill routes to ha-entities
   → LLM: GET /api/states, filters light.* + area=living_room
   → Response: list with state, brightness, color
   ```

   **Onboarding skill** `ha-onboarding.md` (loaded only on first run):
   - Checks connection: `GET {HA_URL}/api/` → "API running"
   - Checks Relay: `GET {RELAY_URL}/health` → "ok"
   - Checks token permission: `GET {HA_URL}/api/config` → HA version
   - Shows quick wins: "Try: 'Turn on light X' or 'Show all sensors'"
   - Guided tour of key capabilities

   **Setup validation script** (included in the App):
   ```bash
   # User can test without LLM
   curl -s -H "Authorization: Bearer $TOKEN" http://ha:8123/api/
   # → {"message": "API running."}

   curl -s -H "Authorization: Bearer $TOKEN" http://ha:8791/health
   # → {"status":"ok","ha_ws_connected":true}
   ```

   **Error handling in onboarding:**
   | Error | Skill reaction |
   |-------|----------------|
   | HA_URL unreachable | "I cannot reach HA. Check URL and network." |
   | Invalid token (401) | "The token is rejected. Create a new one in profile → security." |
   | Relay unreachable | "The Relay is not responding. Is the App started?" |
   | Relay WS disconnected | "The Relay has no WS connection to HA. Check App logs." |

**Validation for Phase 1a:**
| Test | Pass criterion |
|------|----------------|
| Relay health | `GET /health` → `{"status":"ok"}` |
| Relay auth | No token → 401 |
| Relay WS proxy | `POST /ws {"type":"config/area_registry/list"}` → area list |
| Onboarding | New user: install → config → "Show me all lights" works in < 5 min |
| Bootstrap routing | User says "create automation" → bootstrap loads `ha-automation-crud` |

---

#### Phase 1b: Automation CRUD + Control (Week 2-3)

**The core use case.** Create, edit, and control automations using REST API only.

**Skills that get built:**

1. **`ha-automation-crud.md`** - automation lifecycle
   - **List:** `GET /api/states` → filter `automation.*`
   - **Get config:** `GET /api/config/automation/config/{id}`
   - **Create:** build config → preview → `POST /api/config/automation/config/{id}`
   - **Update:** load existing config → merge → preview → `POST /api/config/automation/config/{id}`
   - **Delete:** back up config → `DELETE /api/config/automation/config/{id}`
   - **Reload:** `POST /api/services/automation/reload`
   - Config structure reference: trigger types, condition types, action types
   - Entity ID conventions, slug generation

2. **`ha-automation-control.md`** - automation control
   - `turn_on`, `turn_off`, `toggle`, `enable`, `disable`
   - All via `POST /api/services/automation/{action}`
   - Reload after changes

3. **`ha-control.md`** - device control (bonus, because trivial)
   - Service-call reference for most common domains
   - `light`, `switch`, `climate`, `cover`, `media_player`, `lock`, `fan`
   - All via `POST /api/services/{domain}/{service}`

4. **`ha-entities.md`** - entity search (required for automations)
   - Search/filter entities via `GET /api/states`
   - Domains, areas, device context
   - Service discovery via `GET /api/services`

**What is intentionally NOT built in Phase 1b:**
- ~~Best-practice validation~~ (Phase 2)
- ~~Trace analysis~~ (Phase 1c)
- ~~Diagnostics~~ (Phase 2)
- ~~Batch operations~~ (Phase 2)
- ~~Dependency analysis~~ (Phase 2)
- ~~Blueprints~~ (Phase 2)

**Automation CRUD needs only REST - no WS, no Relay files, no backups.**

**Validation for Phase 1b:**
| Test | Pass criterion |
|------|----------------|
| List automations | Same list as in HA UI |
| Create automation | "Motion turns light on" → automation created in HA with correct triggers/actions |
| Edit automation | Change existing automation → config merged correctly |
| Delete automation | Automation removed, reload executed |
| Enable/disable automation | `turn_on`/`turn_off` works |
| Control device | "Living room light to 50%" → service call correct |
| Search entities | "All temperature sensors" → correct results |

---

#### Phase 1c: Automation Observability (Week 4-5)

**Traces and diagnostic entry point** - requires the WS proxy from Phase 1a.

**Skills that get built:**

1. **`ha-automation-trace.md`** - trace analysis
   - Fetch traces via Relay: `POST /ws {"type":"trace/list","domain":"automation"}`
   - Read single trace: `POST /ws {"type":"trace/get","domain":"automation","item_id":"...","run_id":"..."}`
   - Interpret trace: what triggered, which conditions were true/false, which actions ran
   - Detect and explain trace errors

2. **`ha-automation-validate.md`** - baseline validation
   - Validate config structure (are triggers/conditions/actions valid?)
   - Validate entity IDs (do referenced entities exist?)
   - Validate service names (does the service exist?)
   - Top 10 best-practice rules (not full catalog; full in Phase 2):
     - Binary sensor trigger without `for:` (bounce/noise)
     - `wait_for_trigger` without timeout
     - Service call without target
     - Template without availability guard
     - Parallel actions with shared actuators
     - State trigger without attribute filter
     - Missing off delay for motion sensors
     - Automation mode `single` on repeatable triggers
     - Delay instead of timer helper
     - Missing condition in `restart` mode

3. **`ha-automation-verify.md`** - verify trigger
   - Trigger automation manually: `POST /api/services/automation/trigger`
   - Wait for and analyze trace
   - Explain result to user

**Validation for Phase 1c:**
| Test | Pass criterion |
|------|----------------|
| List traces | Fetch latest 5 traces for an automation |
| Analyze trace | "Why did my automation not fire?" → trace shows blocking condition |
| Validation | Automation missing `for:` → warning emitted |
| Verify | Trigger automation → trace confirms execution |

---

### Phase 1 Overall View

```
Week 1-2: Infrastructure + Onboarding
  ├── Relay MVP (WS proxy + health)
  ├── Bootstrap skill (routing + catalog)
  ├── Safety skill (write rules)
  └── Onboarding skill (setup validation + quick wins)

Week 2-3: Automation CRUD + Control
  ├── ha-automation-crud (create/read/update/delete)
  ├── ha-automation-control (on/off/toggle/enable/disable)
  ├── ha-control (device control)
  └── ha-entities (entity search)

Week 4-5: Automation Observability
  ├── ha-automation-trace (trace analysis)
  ├── ha-automation-validate (baseline validation + top 10 rules)
  └── ha-automation-verify (trigger test)
```

**After Phase 1, the user has:**
- Relay installed and configured (5-minute onboarding)
- Can create, edit, delete automations
- Can control devices
- Can search entities
- Can analyze traces and diagnose issues
- Has baseline validation with the 10 most important rules

---

### Phase 2: Full Breadth (6-8 weeks)

**Goal:** Migrate all remaining managers + complete automation features.

#### 2a: Complete automation (Week 1-3)

| Skill | What | Relay change |
|-------|------|--------------|
| `ha-automation-diagnose.md` | Full diagnostics (oscillation, writer conflicts, condition blockers) | None |
| `ha-automation-batch.md` | Batch update/delete with backup + rollback | Relay: `/backups` |
| `ha-automation-dependency.md` | Cross-automation reference analysis | None (LLM scans configs) |
| `ha-automation-blueprint.md` | Blueprint import/list | None (REST) |
| `ha-best-practices.md` | Full catalog of all 70 rules | None (skill knowledge) |

#### 2b: Migrate remaining managers (Week 3-8)

| # | Skill | Replaces | Relay change |
|---|-------|----------|--------------|
| 1 | `ha-script-crud.md` + `ha-script-validate.md` | script_manager | None (REST) |
| 2 | `ha-helpers.md` | helper_manager | None (WS proxy exists) |
| 3 | `ha-organizer.md` | organizer_manager | None (WS proxy exists) |
| 4 | `ha-dashboard.md` | dashboard_manager | None (WS proxy exists) |
| 5 | `ha-integrations.md` | integration_manager | None |
| 6 | `ha-system.md` | system_manager | None |
| 7 | `ha-energy.md` | energy_manager | None (WS proxy exists) |
| 8 | `ha-events.md` | event_monitor | Relay: `/ws/subscribe` |
| 9 | `ha-scene.md` | scene_manager | None |
| 10 | `ha-repairs.md` | repairs_manager | None (WS proxy exists) |
| 11 | `ha-playbook.md` | playbook_manager | None |
| 12 | `ha-analytics.md` | analytics | None |

**Relay extensions in Phase 2:**
- `POST /backups` - backup CRUD (for batch operations)
- `POST /ws/subscribe` - event subscriptions with timeout (for event monitor)

**Estimated Relay size after Phase 2:** ~1,500-2,000 lines

**Risk:** Medium. High skill-writing effort, but no new architectural challenges.

---

### Phase 3: YAML & Filesystem (4-6 weeks)

**Goal:** Deliver the capabilities that truly require host container access.

**Relay extension:**
- `POST /files` - filesystem read/write/list/delete with path allowlist

**Skills:**
| Skill | What | Complexity |
|-------|------|------------|
| `ha-template-yaml.md` | Create/edit/migrate template sensors via YAML | L |
| `ha-sensor-yaml.md` | REST/command-line/SQL/scrape sensors via YAML | L |
| `ha-files.md` | Read/edit config files | S |
| `ha-backup.md` | Backup management | S |
| `ha-diagnose.md` | Cross-domain diagnostic procedures | M |

**Template/sensor specifics:**
- Template sensors with only simple features → config flow via WS (no YAML needed)
- Template sensors with triggers, icon templates, multi-entity → YAML via Relay `/files`
- REST/command-line sensors → YAML-only, Relay `/files` required
- SQL/scrape sensors → config flow via WS (no YAML needed)

**Validation:**
Side-by-side comparison: same sensor config created via skill as with current MCP server.
Best-practice rules: skill must detect ≥90% of issues (measured on 50 test automations).

**Risk:** Medium. YAML writing with backup + atomic write is the critical path.

---

### Phase 4: Cleanup + Polishing (4-6 weeks)

**Goal:** Remove MCP server, perfect onboarding, finalize docs.

#### 4a: MCP server sunset

```
Week 0:  All managers on migration_mode=skill (default)
Week 2:  MCP server in "sunset" mode (deprecation notice in tools/list)
Week 4:  MCP server default disabled (legacy_mcp_server: true to reactivate)
Week 8:  MCP server code removed, Git tag for rollback
```

#### 4b: MCP facade for non-skill clients

For Cursor, Windsurf, etc. that cannot use skills:
```yaml
relay_mcp_compat: true  # Enables /mcp endpoint on Relay
```
~2,000-3,000 lines of "dumb" tool registration exposing Relay endpoints as MCP tools. No intelligence - pass-through only.

#### 4c: Code cleanup

**What is removed:**
- `src/server/fastmcp/` - 539 files, 64,582 lines
- `src/best-practices/` - rule engine (knowledge now in skills)
- `src/server/resources.ts`, `response-policy*.ts`
- Various server-specific infrastructure

**What remains (for Relay):**
- `src/ha/rest.ts` + `src/ha/ws.ts` - HA API clients
- `src/backup/` - backup manager
- `src/security/` - auth (simplified)
- `src/config.ts` - config (simplified)
- `src/utils/` - logger, helpers

#### 4d: Perfect onboarding

- Refine onboarding skill based on Phase 1 learnings
- Video/GIF tutorial for README
- Troubleshooting skill for common problems
- Incorporate feedback from early adopters

---

## Summary

| Metric | Today (MCP server) | Target (Relay + Skills) |
|--------|--------------------|-------------------------|
| Server code | 88,160 lines / 679 files | ~5,000-8,000 lines / ~25 files |
| `fastmcp/` logic | 64,582 lines / 539 files | 0 (replaced by skills) |
| Relay code | — | ~2,000-3,000 lines |
| Skills | — | 1 bootstrap + 1 onboarding + 28 sub-skills |
| Best-practice rules | 70 TypeScript modules | Skill knowledge (Markdown) |
| Maintenance for HA API changes | TypeScript PR + build + deploy | Edit skill file |
| Client compatibility | MCP clients only | Direct REST + Relay + optional MCP facade |
| Intelligence updates | Redeploy App | Update skill file (effective immediately) |
| Onboarding time | ~15-20 min (App + MCP client config + verify) | **< 5 min** (App + token + skill install) |

| Phase | Duration | Core deliverable |
|-------|----------|------------------|
| 1a: Infrastructure + Onboarding | 2 weeks | Relay MVP, bootstrap, onboarding in < 5 min |
| 1b: Automation CRUD | 1-2 weeks | Create/edit/delete automations + device control |
| 1c: Automation Observability | 1-2 weeks | Traces, baseline validation (top 10 rules), trigger verify |
| 2: Full Breadth | 6-8 weeks | All 18 managers migrated, automation complete |
| 3: YAML & Filesystem | 4-6 weeks | Template/sensor YAML, file ops, backups |
| 4: Cleanup | 4-6 weeks | MCP server removed, MCP facade, polishing |
| **Total** | **~18-25 weeks** | **91-94% code reduction, < 5 min onboarding** |
