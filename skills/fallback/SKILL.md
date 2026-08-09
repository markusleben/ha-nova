---
name: fallback
description: Mandatory fallback for any HA NOVA task without a dedicated subskill. Must be invoked before any raw relay write operation. Covers blueprints, device config-entry detach, Apps, Zigbee/Z-Wave, and unsupported config-entry helper families.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Fallback


## Scope

Mandatory fallback for HA features without a dedicated skill. Three tiers:

- **Relay-Ready**: API works via Relay, no skill yet -- provide experimental relay calls with safety guardrails.
- **Roadmap**: Planned for future Relay phases -- explain timeline + workaround.
- **External**: Outside HA NOVA scope -- web search + HA UI pointer.

All relay calls in this skill are experimental -- always follow the Safety section below.

## Bootstrap (before Home Assistant tasks)

Read and follow `../ha-nova/session-bootstrap.md` before the first
Home Assistant or Relay-Ready task in this session.

Relay health is needed only when executing experimental relay calls, not for
Roadmap/External guidance.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

For every Relay-Ready call in this skill:
- write request JSON with the client's native file-writing tool
- use `ha-nova relay ws --data-file <payload-file>` or `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
- use `--out <result-file>` for large responses
- treat inline `-d` as an optional tiny diagnostic path, not the canonical contract
- transports diverge on empty write bodies: WS validates missing required fields fail-closed, but a parameterless WS write executes on its bare `type`, and `/core` POST may silently CREATE — read `skills/ha-nova/relay-api.md` → Write-Probing Asymmetry before any experimental write

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
| Logs / diagnostics (why a specific automation/script/device/integration failed: traces, error/system logs, root cause) | Covered | diagnose |
| Media players (transport, volume, source, grouping, browsing, TTS announce) | Covered | media |
| Notifications (targets, mobile-app sends, persistent notifications) | Covered | notify |
| Actionable-notification callbacks (waiting for a button press) | Covered for the durable path | write (an automation on `mobile_app_notification_action`); a bounded in-chat window is possible via the `collect_events` envelope |
| Cameras (snapshot, stream URL, record) | Covered | camera |
| MQTT (bounded topic listening, discovery/debug info, publish) | Covered | mqtt |
| Voice / Assist (utterance testing, pipelines, entity exposure, engine inventory) | Covered | assist |
| Persons / Zones / Tags | Covered | admin |
| User accounts (list, create, delete — with owner/system guards) | Covered | admin |
| YAML-only configuration (template/REST/command-line sensors, packages, themes) | Covered | yaml-config |
| Frontend themes | Covered | yaml-config |
| External data stores (InfluxDB long-term history, Prometheus, ...) | Covered | external-sources |
| Weather forecasts (`weather.get_forecasts`) | Covered | service-call |
| Calendar Events (read / create / update / delete) | Covered | calendar |
| Custom events / known JSON webhooks | Covered | service-call |
| Alarm / lock runtime control | Covered | service-call |
| To-do Lists (items + Local To-do lifecycle) | Covered | todo |
| Area / Floor CRUD | Covered | organize |
| Label CRUD / Rich label metadata | Covered | organize |
| Category CRUD / Entity category assignment | Covered | organize |
| Entity / Device metadata updates | Covered | organize |
| Blueprints | Relay-Ready | this skill |
| Energy (analysis + source/device config) | Covered | energy |
| Other Config-Entry Helpers | Relay-Ready | this skill |
| Statistics repair / Purge / Entity registry remove | Covered | maintenance |
| Device config-entry detach | Relay-Ready | this skill |
| Integration onboarding (add / re-auth an integration via config flow) | Covered | integration-setup |
| Firing custom events / triggering webhooks | Relay-Ready | this skill |
| Custom-integration configuration APIs (Alarmo, Scheduler, Adaptive Lighting, Frigate, ...) | Relay-Ready | this skill |
| Event capture — bounded window (a button's event, a short watch after an action) | Relay-Ready | this skill; `mqtt` for MQTT topics |
| Event streaming — continuous | Roadmap Phase 1c | -- |
| Backups (status, create, inspect, delete) | Covered | backup |
| Config snapshots (targeted capture/restore of automations, scripts, scenes, dashboards, helpers, energy prefs, metadata, YAML files) | Covered | the owning family skill (see `skills/ha-nova/config-snapshots.md`) |
| Updates (pending, release notes, install, skip) | Covered | updates |
| Apps / Supervisor: install, uninstall, configure, store | External | -- (updates: `updates`; start/stop/restart and host reboot: `service-call`) |
| HACS (registration, download, version switching, uninstall, migration) | Covered | hacs |
| Zigbee / Z-Wave Config | External | -- (MQTT-level inspection of a Zigbee2MQTT setup: `mqtt`) |
| Alarm / lock code management (lock user codes, alarm PINs) | External | -- (Home Assistant UI; codes never enter chat) |
| Integration entry lifecycle (reload, enable/disable, options, remove) | Relay-Ready | this skill |
| Matter / Thread status (border router, datasets, node diagnostics) | Relay-Ready | this skill |
| Matter / Thread commissioning | External | -- (companion app; BLE pairing is not an API surface) |
| Assist custom sentences / intent scripts | Relay-Ready | this skill (file mechanics below); `assist` tests the result |
| Creating a calendar (the `local_calendar` integration) | Covered | integration-setup |
| Device category assignment | Not a Home Assistant surface | -- (devices carry no category; entity categories are `organize`) |

## Flow

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

External storage becomes scannable only through a documented adapter (`skills/ha-nova/consumer-discovery-preflight.md` → Extension Adapter Contract) — never by ad-hoc parsing.

**Web search is mandatory for Relay-Ready writes.** The relay call examples below cover common read patterns, but write payloads change across HA versions. Always verify the current schema via web search before constructing a write payload.

## Relay-Ready Features

### Blueprints -- RELAY-READY

List and import automation/script blueprints from the community or custom URLs.

**Search:** `home assistant blueprint import automation api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>

# payload examples (verify current schema via web search first):
# {"type":"blueprint/list","domain":"automation"}
# {"type":"blueprint/import","url":"https://community.home-assistant.io/t/..."}   (fetches + previews, does not save)
# {"type":"blueprint/save","domain":"automation","path":"<folder/name.yaml>","yaml":"<blueprint yaml>"}
# {"type":"blueprint/substitute","domain":"automation","path":"<folder/name.yaml>","input":{...}}   (read-only: expands the blueprint with the given inputs)
```

**Risks:** Imported blueprints execute when instantiated. Review blueprint source before import. Instantiating a blueprint into an automation (`use_blueprint`) is a normal automation create — hand off to `ha-nova:write`.

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

### Integration Entry Lifecycle -- RELAY-READY

Reload, enable/disable, options/reconfigure, and remove for an existing config
entry. `ha-nova:integration-setup` owns ADDING an integration and continuing a
pending `reauth`; everything after that lands here until it gets a skill.

**Search:** `home assistant config entry reload disable options flow api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay core --method POST --path /api/config/config_entries/entry/<entry_id>/reload
```

**Risks:** Reload re-runs setup and briefly drops the entry's entities. Remove
(`DELETE /api/config/config_entries/entry/<entry_id>`) deletes every device and
entity that entry owns and is not undoable — preview the counts from
`search/related` and take the typed confirmation code. Options flows are
response-driven: submit exactly the fields the live step schema returned.

### Assist Custom Sentences -- RELAY-READY

Teach Assist a phrase Home Assistant does not understand out of the box. This
needs the opt-in file access (`ha-nova:yaml-config` → Bootstrap explains how to
turn it on); its own scope covers sensors, packages and themes, so the file
mechanics live here.

**Search:** `home assistant custom_sentences intent_script yaml 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay files --data-file <payload-file>
```
Write `/config/custom_sentences/<lang>/<name>.yaml` — NOT `/config/ha_nova/`;
Home Assistant only reads sentences from that fixed path. An `intent_script:`
block in `configuration.yaml` supplies the action when the intent is new.

**Verify:** reload with `conversation.reload`, then run the exact phrase
through `ha-nova:assist` (`POST /api/conversation/process`) — a sentence file
that parses is not a sentence Assist matched.

**Risks:** `write_file` replaces the whole file, so read it first. A malformed
sentence file makes the conversation agent drop ALL custom sentences, not just
the new one — the assist test is what catches that.

### Matter And Thread Status -- RELAY-READY

Read-only network state for Matter/Thread setups: `otbr/info`,
`thread/list_datasets`, `matter/node_diagnostics`. Commissioning stays
external — it needs BLE from the companion app and is not an API surface.

**Search:** `home assistant thread otbr websocket api matter diagnostics 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** none for the reads themselves; do not surface dataset credentials
(a Thread operational dataset is a network key) in output.

### Device Config-Entry Detach -- RELAY-READY

Remove a config entry from a device (entity-registry removal is owned by `ha-nova:maintenance`).

**Search:** `home assistant device registry remove config entry websocket api 2026`

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay ws --data-file <payload-file>
```

**Risks:** Device detach depends on integration support (`supports_remove_device`) and can sever the current device/config-entry relationship. Preview impact first.

### Custom-Integration Configuration APIs -- RELAY-READY

Integrations that ship their OWN configuration API outside the config-entry flow (Alarmo, Scheduler, Adaptive Lighting, Frigate, ...). Runtime control of their entities stays with the owning skills (for example alarm arm/disarm via `service-call`); this section covers the configuration layer only.

**Search:** `<integration name> home assistant configuration api endpoints 2026` — prefer the integration's own repository docs for payload schemas.

**Experimental relay calls (no skill guardrails):**
```text
ha-nova relay core --method GET --path /api/<integration>/<resource> --out <result-file>
ha-nova relay core --method POST --path /api/<integration>/<resource> --body-file <payload-file>
```

**Observed API behavior (Alarmo; treat as the default assumption for this class):**
- Write commands often exist only as HTTP POST paths, not WS commands (`unknown_command`); such paths answer GET with `405`, which reveals their existence.
- Whether a POST creates or updates depends solely on whether an identifier is present in the body (`area_id`, `entity_id`, `automation_id`, ...) — `POST` with `{}` silently CREATES an empty object (`skills/ha-nova/relay-api.md` → Write-Probing Asymmetry).
- Nested blocks must be sent complete; partial objects overwrite the rest.

**Risks:** These APIs are private and version-dependent. Never probe schemas with empty or partial POST bodies — resolve the schema via web search first. After ANY write, read the affected list/config back and verify no unintended object appeared; an accidental create can trigger integration side effects (Alarmo auto-enables its master panel at two areas).

## Roadmap Features

### Event Subscriptions -- ROADMAP (Phase 1c)

Real-time event streams for state changes, automation triggers, and custom events.

**Search:** `home assistant event subscription state_changed real time api 2026`

**Status:** CONTINUOUS streams are Phase 1c, blocked by the absence of an SSE
endpoint. BOUNDED capture already works: wrap the subscription in a
`collect_events` envelope (max 100 events, max 10 s — `skills/ha-nova/relay-api.md`
→ Bounded Event Collection) and the relay unsubscribes for you. That is enough
to answer "what event does this button fire" or to watch a short window after
an action; `ha-nova:mqtt` uses exactly this pattern.
**Workaround for anything longer:** an automation that reacts and notifies
(`ha-nova:write`), or polling `GET /api/states/{entity_id}`.

## External Features

### Apps / Supervisor Management -- EXTERNAL

App *management* (install, uninstall, configure, store browsing) is Supervisor
territory and stays external. The common premise that "the Supervisor API needs
different auth and endpoints" is only half true: on HA OS/Supervised the
`hassio` integration proxies it under `/api/hassio/*`, and start/stop/restart
plus host reboot/shutdown are ordinary Home Assistant services — those live in
`ha-nova:service-call` under its disruptive tier, with a refusal for the App
running this Relay. App UPDATES are `ha-nova:updates`.

**Search:** `home assistant supervisor app add-on install manage api 2026`

**Alternative for management:** HA UI: Settings > Apps. Or `ha` CLI on HA OS.

### HACS (Home Assistant Community Store) -- COVERED

Owned by `ha-nova:hacs` (schema-guarded WS command map, reconcile loops,
consumer discovery, migration backup gate). Hand off there; never probe
`hacs/*` WS commands from this skill.

### Zigbee / Z-Wave / Network Configuration -- EXTERNAL

Device pairing is hardware-specific, requires direct coordinator access (ZHA, Z2M, Z-Wave JS).

**Search:** `home assistant zigbee2mqtt zha device pairing configuration 2026`

**Alternative:** HA UI: Settings > Devices & Services > [Zigbee/Z-Wave integration].

## Error Handling

Experimental calls may fail with unfamiliar errors. Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling. Fallback-specific rules:

- `400/VALIDATION_ERROR`: payload schema wrong -- search web for current WS type schema
- `404/NOT_FOUND`: endpoint may not exist in this HA version -- check HA release notes
- `502/UPSTREAM_*` transport errors: verify state/config first (see `relay-api.md` → Timeout and Retry Guidance); retry once only when verification shows no change, then route to `ha-nova:onboarding`

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there.

Every experimental call result names the tier (Relay-Ready / Roadmap / External), carries the EXPERIMENTAL marker where required, and summarizes verified outcomes only.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

Premise handling:
- Correct invalid Home Assistant premises explicitly.
- Do not silently compensate for a wrong premise.
- Keep corrections brief and technical, not preachy.

Rules for all experimental relay calls in this skill:

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- Read before write: fetch current state first for any destructive operation
- **Full-document overwrites** (e.g., `lovelace/config/save`): MUST read full config, merge changes in memory, preview merged result with a plain-language behavior line (`skills/ha-nova/write-safety.md` → Behavior narrative), then write. There is no partial update endpoint — the entire config is replaced. After the write, read the document back and verify both the intended change and the survival of unrelated content (views, cards, sources) before reporting success.
- **Field-level list replacements** (e.g., `energy/save_prefs`): omitted top-level keys are preserved, but each provided key replaces its entire list. To add one item, read the existing list first, append, then save back the full list. After the write, read the prefs back and verify the pre-existing list items survived alongside the new one.
- **Web search before write**: always search for current payload schema before constructing any write payload. HA APIs evolve across versions — the examples in this skill are starting points, not authoritative schemas.
- **Schema discovery never probes writes**: never send an empty or partial body to a `/core` POST path to elicit a validation error — `/core` has no schema check and may CREATE an object instead — and never send a bare WS `type` you have not verified to be read-only, because a parameterless write command executes immediately (`skills/ha-nova/relay-api.md` → Write-Probing Asymmetry). Empty-body probing is limited to WS commands already documented as read-only; the write schema still comes from web search.
- **Pre-delete `search/related` verdicts fail closed**: verify `ok=true` and `data` is an object before projecting family keys (`skills/ha-nova/relay-api.md` → Parsing rule); a failed or unexecuted scan is inconclusive — never a no-consumer result.
- Every experimental call must show: "EXPERIMENTAL: No dedicated subskill schema guardrails. Proceed with caution."
- **No diff or auto-undo here**: these writes have no `## Changes` preview or `revert`. When a write may be hard to reverse, say so plainly and point to Home Assistant Backups (Settings > System > Backups) as the safety net before confirming.
- One resource at a time (no batch writes)
- Experimental results may be unexpected — verify data-target match before presenting conclusions (see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding)

### Write Safety by Endpoint Type

| Type | Behavior | Safe pattern | Examples |
|------|----------|-------------|----------|
| Full-document overwrite | Entire config replaced | Read → modify → save full document → read back and verify unrelated content survived | `lovelace/config/save` |
| Field-level list replace | Omitted keys preserved, provided keys fully replaced | Read existing list → append/modify → save full list → read back and verify pre-existing items survived | `energy/save_prefs` |
| Merge/patch | Only provided fields updated | Send only changed fields | `config/area_registry/update`, `config/entity_registry/update` |
| Delete | Irreversible for areas/zones/labels | Always `search/related` first, typed confirmation code | `config/area_registry/delete`, `config/label_registry/delete` (entity removal → `ha-nova:maintenance`) |

No HA WS endpoint has optimistic locking (no ETags, no version numbers). Last writer wins silently.

### Anti-Patterns (never do this)

- Sending `lovelace/config/save` with a guessed or partial payload — this overwrites the ENTIRE dashboard config, destroying all other views and cards
- Sending `energy/save_prefs` with a single source — this replaces the entire `energy_sources` list, deleting all existing sources
- Probing write endpoints to "see what happens" — read the Relay-Ready section first
- Sending an empty or partial body to a `/core` POST path to discover its schema — WS validates fail-closed, HTTP POST silently creates (`relay-api.md` → Write-Probing Asymmetry)
- Skipping this skill and going straight to `ha-nova relay ws`/`ha-nova relay core` for unfamiliar operations
- Using trial-and-error to discover payload schemas — search web for the WS type schema instead
