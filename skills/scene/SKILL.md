---
name: scene
description: Use when listing, reading, creating, updating, or deleting Home Assistant storage scenes through HA NOVA Relay. For activating a scene, use ha-nova:service-call.
---

# HA NOVA Scene

## Scope

Storage-scene lifecycle:
- list scenes and show which ones are editable
- read one scene config
- create, update, delete storage scenes (what the HA scene editor stores in `scenes.yaml`)

Not in scope:
- activating scenes (`scene.turn_on`) — use `ha-nova:service-call`
- integration-owned scenes (Hue, deCONZ, ...) — see Editability Guard below
- `scene.create` runtime snapshots inside automations — owned by `ha-nova:write`

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based requests:
- `ha-nova relay ws --data-file <payload-file>`
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
- `--out <result-file>` for large responses, `--jq-file <filter-file>` for complex filters

Relay-core response body is under `.data.body` (see `skills/ha-nova/relay-api.md` → Standard Envelope).

## Editability Guard (critical)

Only scenes with registry `platform: "homeassistant"` are editable through the scene config API. Integration-owned scenes (for example `platform: "hue"`) have no HA-side config — reads return 404 and writes must never be attempted. Resolve the platform BEFORE any config operation:

Create `<payload-file>` with `{"type":"config/entity_registry/get","entity_id":"scene.<slug>"}`, then:
```text
ha-nova relay ws --data-file <payload-file> --out <registry-file>
```
- `platform` must be `homeassistant`; otherwise explain that this scene belongs to an integration and is managed in that integration's own app, then stop.
- `unique_id` is the scene config id for `/api/config/scene/config/<id>` — never use the entity_id slug as the config id.

## Flow

### List
Create `<payload-file>` with `{"type":"config/entity_registry/list"}`, then:
```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file> --out <result-file>
```
Write `<filter-file>` with:
```jq
[.data[] | select(.entity_id | startswith("scene.")) | {entity_id, name: (.name // .original_name), platform, editable: (.platform == "homeassistant")}]
```

### Read
1. Resolve `unique_id` + platform (Editability Guard).
2. `ha-nova relay core --method GET --path /api/config/scene/config/<unique_id> --out <result-file>`
3. Present name, entities, and their captured target states. Flag captured entity_ids missing from the registry (renamed or deleted since capture — the scene applies partially with no error); offer removal, never preserve or drop silently.

### Create
1. Resolve every entity the scene should capture (exact `entity_id`; on ambiguity ask one blocking question). Scenes take actionable domains only (light, switch, cover, climate, fan, media_player, lock, humidifier, `input_*`, ...); refuse read-only domains (sensor, binary_sensor, ...). If the registry already lists a scene with this name, ask before creating a duplicate. Read each entity's state; warn on `unknown`/`unavailable` and leave it out unless the user insists.
2. Build the config body — `id` in the body MUST equal the id in the path; use the current epoch-milliseconds string as the new id (HA editor convention; POSIX example `date +%s000` — any unique numeric string works). The POST is an upsert: before creating, GET the id and require a 404 so an existing scene is never silently overwritten.
   ```json
   {"id":"<epoch-ms>","name":"<name>","icon":"mdi:sofa","entities":{"<entity_id>":{"state":"on"}}}
   ```
   `icon` is optional. Entity values are the target states (plus attributes) the scene applies when activated.
3. **Capture attributes deliberately** (better than the HA editor, which grabs only state + brightness for lights):
   - light that is on: `state`, `brightness`, plus exactly ONE color attribute matching its `color_mode` (`color_temp_kelvin` OR `rgb_color`/`hs_color`) — never both, mixed color attributes reproduce wrong; an off light exposes no color attributes, capture `state: "off"` only
   - prefer individual lights over light-group entities — group snapshots are a known HA reproduce-state trouble spot
   - switch/lock/input_boolean: `state` only
   - other domains: `state` plus clearly writable target attributes (for example cover position, climate temperature and `hvac_mode`); never copy measurement or diagnostic attributes
4. Preview name + full entities map; ask natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation).
5. `ha-nova relay core --method POST --path /api/config/scene/config/<id> --body-file <payload-file>`
6. Read back via GET and verify the fields; the config API reloads scenes automatically. Resolve the new entity_id from the registry by matching `unique_id` to the config id (the entity_id derives from `name`, not the id; never guess the slug — the registry can lag the write by a moment, retry once), then confirm it via `/api/states/<entity_id>`.

### Create from current state ("save this room as a scene")
Same as Create, but the entities map IS the live state: read each requested entity's state and copy it (attribute rules from Create step 3). Show which entities were captured with which values before confirming.
Persistence routing per `skills/ha-nova/best-practices.md` → Persistence Model: a stored scene is a static capture that never updates itself; `scene.create` snapshots and automation-driven save → modify → restore cycles hand off to `ha-nova:write` (helpers, not scenes).

### Update
1. Read the current config (Read flow).
2. Merge the requested change in memory — the POST replaces the ENTIRE scene config; never send a partial body and never drop entities the user did not mention.
3. Preview a concise before/after excerpt; natural confirmation bound to this exact preview.
4. If the conversation paused between read and confirmation, re-read and re-verify the merge basis before writing (last writer wins). Apply the orphaned-member flag from Read step 3.
5. POST the full merged body, then read back and verify both the intended change and the survival of unrelated entities.

### Delete
1. Resolve id + platform (Editability Guard).
2. Consumer check: `{"type":"search/related","item_type":"entity","item_id":"scene.<slug>"}` — show automations/scripts that reference the scene, or an explicit no-consumer result (an empty `data` object means no consumers).
3. Require exact token confirmation `confirm:<token>` — generate a short token, display it in the Options slot, and proceed only when the user types it back exactly.
4. `ha-nova relay core --method DELETE --path /api/config/scene/config/<id>`
5. Verify absence: config GET returns status 404 and the entity is gone.

## Error Handling & State Semantics

- A scene entity's state is the timestamp of its last activation; `unknown` means "never activated" — not an error.
- Config GET 404 on a `homeassistant`-platform scene: the scene was deleted outside this session — re-list instead of retrying.
- Registry get miss on a live scene entity: a YAML scene without an `id:` — not API-editable; point to `scenes.yaml`.
- POST/DELETE failures: show HA's error and stop; do not retry blind. If every scene config call fails, `scenes.yaml` may be missing from the config includes — point to HA docs.
- Full relay error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

- `Scene`
- `Mode`
- `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / confirmation token
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots.

## Safety

- Create/update use natural confirmation after preview; delete uses exact token confirmation only, even for scenes created earlier in the same session.
- Scene writes have no `revert`; the recovery path is Home Assistant Backups — say so before destructive operations.
- Never guess ids or entity_ids; resolve via registry first.
- One scene per mutation; verify read-back, not just the save response.
- Activation hands off to `ha-nova:service-call` — `scene.turn_on` supports `transition` (lights only); `scene.apply` applies a one-off state set without storing a scene.
