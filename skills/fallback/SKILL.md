---
name: fallback
description: Mandatory fallback for any HA NOVA task without a dedicated subskill. Must be invoked before any raw relay write operation. Covers blueprints, zones/persons/tags, device config-entry detach, Apps, HACS, Zigbee/Z-Wave, and unsupported config-entry helper families.
---

# HA NOVA Fallback


## Scope

Mandatory fallback for HA features without a dedicated skill. Three tiers:

- **Relay-Ready**: API works via Relay, no skill yet -- provide experimental relay calls with safety guardrails.
- **Roadmap**: Planned for future Relay phases -- explain timeline + workaround.
- **External**: Outside HA NOVA scope -- web search + HA UI pointer.

All relay calls in this skill are experimental -- always follow Safety Guardrails below.

## Safety Baseline

- Correct invalid Home Assistant premises explicitly.
- Do not silently compensate for a wrong premise.
- Keep corrections brief and technical, not preachy.

## Output Rules

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

## Bootstrap (only before Relay-Ready calls)

Only needed when executing experimental relay calls (not for Roadmap/External guidance).

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

For every Relay-Ready call in this skill:
- write request JSON with the client's native file-writing tool
- use `ha-nova relay ws --data-file <payload-file>` or `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
- use `--out <result-file>` for large responses
- treat inline `-d` as an optional tiny diagnostic path, not the canonical contract

## Capability Map

| Feature | Status | Skill |
|---------|--------|-------|
| Automations CRUD | Covered | read / write |
| Scripts CRUD | Covered | read / write |
| Config Review | Covered | review |
| Helpers (9 storage + 10 config-entry) | Covered | helper |
| Entity Search | Covered | entity-discovery |
| Service Calls | Covered | service-call |
| Relay Setup | Covered | onboarding |
| Dashboard / Lovelace (storage lifecycle, cards, resources) | Covered | dashboard |
| Scenes (storage CRUD) | Covered | scene |
| History Queries | Covered | history |
| Logbook Queries | Covered | history |
| Statistics / Trend Queries | Covered | history |
| System Health / Repairs | Covered | health |
| Calendar Queries | Covered | calendar |
| To-do Lists (items + Local To-do lifecycle) | Covered | todo |
| Area / Floor CRUD | Covered | organize |
| Label CRUD / Rich label metadata | Covered | organize |
| Category CRUD / Entity category assignment | Covered | organize |
| Entity / Device metadata updates | Covered | organize |
| Blueprints | Relay-Ready | this skill |
| Zone / Person / Tag Mgmt | Relay-Ready | this skill |
| Energy (analysis + source/device config) | Covered | energy |
| Other Config-Entry Helpers | Relay-Ready | this skill |
| Statistics repair / Purge / Entity registry remove | Covered | maintenance |
| Device config-entry detach | Relay-Ready | this skill |
| Event Subscriptions | Roadmap Phase 1c | -- |
| YAML / REST / Command-Line Sensors | Roadmap Phase 3 | -- |
| Backups (status, create, inspect, delete) | Covered | backup |
| Updates (pending, release notes, install, skip) | Covered | updates |
| Apps / Supervisor | External | -- |
| HACS | External | -- |
| Zigbee / Z-Wave Config | External | -- |

## Agent Flow

```
1. Check Capability Map for user's request
2. If "Covered" -> STOP, use the listed skill instead
3. If "Relay-Ready":
   a. FIRST: Search web using the provided Search query — understand current payload schema before any call
   b. Show experimental relay call examples informed by search results
   c. Preview full payload before any write — never guess fields
   d. Execute only after user confirms that exact preview (see context skill → Active Preview Confirmation)
4. If "Roadmap":
   a. Explain which phase and what blocks it
   b. Search web for manual workaround or alternative approach
   c. Suggest HA UI as interim solution
5. If "External":
   a. Explain why it's outside HA NOVA scope
   b. Search web for current best practice (how to do it directly in HA)
   c. Point to HA UI path
```

**Web search is mandatory for Relay-Ready writes.** The relay call examples below cover common read patterns, but write payloads change across HA versions. Always verify the current schema via web search before constructing a write payload.

## Relay-Ready Features

### Blueprints -- RELAY-READY

List and import automation/script blueprints from the community or custom URLs.

**Search:** `home assistant blueprint import automation api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** Imported blueprints execute when instantiated. Review blueprint source before import.

### Zone / Person / Tag Management -- RELAY-READY

Manage location zones, person entities, and NFC/QR tags.

**Search:** `home assistant zone person tag management api websocket 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** Zone changes affect presence detection automations. Person changes affect device trackers.

### Other Config-Entry Helpers -- RELAY-READY

Handle unsupported config-entry helper types that are not yet owned by `ha-nova:helper`.

Owned by `ha-nova:helper` now:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`
- `template`

Still handled here:

- `trend`
- `random`
- `filter`
- `generic_thermostat`
- `switch_as_x`
- `generic_hygrostat`

**Search:** `home assistant config entry flow helper trend random filter generic_thermostat api 2026`

**Supported types in this fallback section:** `trend`, `random`, `filter`, `generic_thermostat`, `switch_as_x`, `generic_hygrostat`

**Experimental relay calls (no skill guardrails):**
```text
# Start create/reconfigure flow
ha-nova relay core --method POST --path /api/config/config_entries/flow --body-file <payload-file>

# Submit create/reconfigure step
ha-nova relay core --method POST --path /api/config/config_entries/flow/{flow_id} --body-file <payload-file>

# Start options flow for update when supported
ha-nova relay core --method POST --path /api/config/config_entries/options/flow --body-file <payload-file>

# Submit options step
ha-nova relay core --method POST --path /api/config/config_entries/options/flow/{flow_id} --body-file <payload-file>

# Delete unsupported config-entry helper by entry_id
ha-nova relay core --method DELETE --path /api/config/config_entries/entry/{entry_id}
```

**Risks:** Multi-step flows are complex. Each step returns the next step's schema. Update support can be domain- and version-specific. Delete requires correct `entry_id` resolution first. Prefer HA UI for these.

### Device Config-Entry Detach -- RELAY-READY

Remove a config entry from a device (entity-registry removal is owned by `ha-nova:maintenance`).

**Search:** `home assistant device registry remove config entry websocket api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** Device detach depends on integration support (`supports_remove_device`) and can sever the current device/config-entry relationship. Preview impact first.

## Roadmap Features

### Event Subscriptions -- ROADMAP (Phase 1c)

Real-time event streams for state changes, automation triggers, and custom events.

**Search:** `home assistant event subscription state_changed real time api 2026`

**Status:** Coming in Phase 1c. Blocked by: No SSE streaming endpoint in Relay.
**Workaround:** Poll entity state periodically via `GET /api/states/{entity_id}`.

### YAML / REST / Command-Line Sensors -- ROADMAP (Phase 3)

Config-entry template helpers are covered by `ha-nova:helper` — this roadmap item is only about YAML-file, REST, and command-line sensors.

Define custom sensors using Jinja2 templates, REST endpoints, or shell commands.

**Search:** `home assistant template sensor yaml configuration 2026`

**Status:** Coming in Phase 3. Blocked by: No filesystem access in Relay (sensors defined in YAML files).
**Workaround:** Add the YAML manually via the HA config files.

## External Features

### Apps / Supervisor Management -- EXTERNAL

Supervisor API is separate from HA Core API. Requires different auth and endpoints.

**Search:** `home assistant supervisor app add-on install manage api 2026`

**Alternative:** HA UI: Settings > Apps. Or `ha` CLI on HA OS.

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
- **Full-document overwrites** (e.g., `lovelace/config/save`): MUST read full config, merge changes in memory, preview merged result, then write. There is no partial update endpoint — the entire config is replaced. After the write, read the document back and verify both the intended change and the survival of unrelated content (views, cards, sources) before reporting success.
- **Field-level list replacements** (e.g., `energy/save_prefs`): omitted top-level keys are preserved, but each provided key replaces its entire list. To add one item, read the existing list first, append, then save back the full list. After the write, read the prefs back and verify the pre-existing list items survived alongside the new one.
- **Web search before write**: always search for current payload schema before constructing any write payload. HA APIs evolve across versions — the examples in this skill are starting points, not authoritative schemas.
- Every experimental call must show: "EXPERIMENTAL: No dedicated subskill schema guardrails. Proceed with caution."
- **No diff or auto-undo here**: these writes have no `## Changes` preview or `revert`. When a write may be hard to reverse, say so plainly and point to Home Assistant Backups (Settings > System > Backups) as the safety net before confirming.
- One resource at a time (no batch writes)
- Delete requires tokenized confirmation (`confirm:<token>`)
- Broad pre-preview approval is never write confirmation; changed payloads require a fresh preview and confirmation.
- Never guess IDs: resolve via list/search first
- Experimental results may be unexpected — verify data-target match before presenting conclusions (see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding)

### Write Safety by Endpoint Type

| Type | Behavior | Safe pattern | Examples |
|------|----------|-------------|----------|
| Full-document overwrite | Entire config replaced | Read → modify → save full document → read back and verify unrelated content survived | `lovelace/config/save` |
| Field-level list replace | Omitted keys preserved, provided keys fully replaced | Read existing list → append/modify → save full list → read back and verify pre-existing items survived | `energy/save_prefs` |
| Merge/patch | Only provided fields updated | Send only changed fields | `config/area_registry/update`, `config/entity_registry/update` |
| Delete | Irreversible for areas/zones/labels | Always `search/related` first, tokenized confirmation | `config/area_registry/delete`, `config/label_registry/delete` (entity removal → `ha-nova:maintenance`) |

No HA WS endpoint has optimistic locking (no ETags, no version numbers). Last writer wins silently.

### Anti-Patterns (never do this)

- Sending `lovelace/config/save` with a guessed or partial payload — this overwrites the ENTIRE dashboard config, destroying all other views and cards
- Sending `energy/save_prefs` with a single source — this replaces the entire `energy_sources` list, deleting all existing sources
- Probing write endpoints to "see what happens" — read the Relay-Ready section first
- Skipping this skill and going straight to `ha-nova relay ws`/`ha-nova relay core` for unfamiliar operations
- Using trial-and-error to discover payload schemas — search web for the WS type schema instead

## Error Handling

Experimental calls may fail with unfamiliar errors. Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling. Fallback-specific rules:

- `400/VALIDATION_ERROR`: payload schema wrong -- search web for current WS type schema
- `404/NOT_FOUND`: endpoint may not exist in this HA version -- check HA release notes
- `502/UPSTREAM_*` transport errors: verify state/config first (see `relay-api.md` → Timeout and Retry Guidance); retry once only when verification shows no change, then route to `ha-nova:onboarding`
