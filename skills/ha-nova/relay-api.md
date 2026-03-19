# HA NOVA Relay API Contract

Single source of truth for Relay calls used by HA NOVA skills.

## Auth

- Header: `Authorization: Bearer $RELAY_AUTH_TOKEN`
- Header: `Content-Type: application/json`
- Base URL: `RELAY_BASE_URL`

## Endpoints

- `GET /health`
- `POST /ws`
- `POST /core`

## Relay CLI Wrapper

For agent-dispatched flows, use the CLI wrapper instead of raw curl:

1. Write request JSON with the client's native file-writing tool.
2. Use file-based relay flags as the default contract:
   - `ha-nova relay ws --data-file <payload-file>`
   - `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
   - `ha-nova relay ... --out <result-file>`
   - `ha-nova relay ... --jq-file <filter-file>` for non-trivial filters
   - `ha-nova relay ... --jq .field` only for short selectors without shell-special characters

Examples:

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
ha-nova relay core --method GET --path /api/config/automation/config/<id> --out <result-file>
ha-nova relay core --method POST --path /api/services/light/turn_on --body-file <payload-file>
```

The wrapper handles auth (OS credential store), headers, timeouts, and base URL internally.
Inline `-d` / `--body` remains available for small diagnostics, but it is not the canonical cross-platform path.

## Standard Envelope

- Success: `{ "ok": true, "data": ... }`
- Error: `{ "ok": false, "error": { "code": "...", "message": "..." } }`

Parsing varies by endpoint:
- `/ws` responses: upstream payload is in `.data` directly (e.g., `.data.entities[]`)
- `/core` responses: upstream payload is in `.data.body` (with `.data.status` for HTTP status)

## ID Types & Resolution

HA uses different identifiers depending on the API. Skills MUST use the correct type.

| ID Type | Example | Used By |
|---------|---------|---------|
| `entity_id` | `automation.kitchen_lights` | Entity registry, states, `search/related`, service calls |
| `unique_id` (config key) | `1766434159701` (UI) or `motion_kitchen` (YAML) | REST config API, `trace/list`, `trace/get` |

**The entity_id slug and the unique_id are NOT the same** for UI-created items. HA generates a numeric `unique_id` for items created through the UI. YAML-defined items typically use the slug as both.

### Standard Resolution: entity_id → unique_id

When you have an entity_id and need the config key for REST or trace APIs:

Create `<payload-file>` with:

```json
{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}
```

Then run:

```text
ha-nova relay ws --data-file <payload-file> --jq .data.unique_id
```

Use the resolved `unique_id` with:
- Config reads: `GET /api/config/automation/config/{unique_id}`
- Config writes/deletes: `POST|DELETE /api/config/automation/config/{unique_id}`
- Trace list: `trace/list` with `"item_id":"{unique_id}"`
- Trace get: `trace/get` with `"item_id":"{unique_id}"`

**Do NOT use the entity_id slug** as the authoritative id for existing-item config reads, writes, deletes, or traces.

## Post-Write Verification (automation/script)

For create/update verification:
1. read back config via `GET /api/config/{domain}/config/{unique_id}`
2. reload the domain service
3. query entity registry and match `unique_id` to the actual `entity_id`
4. read `/api/states/{entity_id}` to confirm runtime presence

If the actual `entity_id` differs from expectation, surface the real slug and offer a rename/refactor follow-up instead of silently assuming the requested slug won.

## /ws Contract

Request examples:
- `{ "type": "ping" }`
- `{ "type": "config/entity_registry/list_for_display" }` (compact entity listing; preferred over `get_states`)
- `{ "type": "get_states" }` (full state dump; avoid for listings — use only for single-entity state when needed)

Expected success body:
- `ok=true`
- Entity registry: `data.entities[]` with abbreviated keys (`ei`=entity_id, `en`=name, `ai`=area_id)
- `get_states`: `data` is an array of full state objects (thousands of entries — avoid for discovery)

Parsing rule:
- Entity registry: `.data.entities[]` — filter with jq `select(.ei | startswith("automation."))`.
- `get_states`: treat as `(.data // [])[]`, filter only object entries with string `entity_id`.

## /core Contract

Request envelope:

```json
{
  "method": "GET|POST|DELETE",
  "path": "/api/...",
  "body": {}
}
```

Response envelope:

```json
{
  "ok": true,
  "data": {
    "status": 200,
    "body": {}
  }
}
```

Parsing rule:
- Success flag: `.ok`
- Upstream status: `.data.status`
- Upstream payload: `.data.body`

## Frequent HA API Paths

Automation config:
- `GET /api/config/automation/config/{id}`
- `POST /api/config/automation/config/{id}`
- `DELETE /api/config/automation/config/{id}`

Script config:
- `GET /api/config/script/config/{id}`
- `POST /api/config/script/config/{id}`
- `DELETE /api/config/script/config/{id}`

State/config helpers:
- `GET /api/states`
- `GET /api/states/{entity_id}`
- `POST /api/services/automation/reload`
- `POST /api/services/script/reload`

## Helper CRUD (via /ws)

Supported types: `input_boolean`, `input_number`, `input_text`, `input_select`,
`input_datetime`, `input_button`, `counter`, `timer`, `schedule`

```
List:   {"type": "{type}/list"}
Create: {"type": "{type}/create", "name": "...", ...type-specific}
Update: {"type": "{type}/update", "{type}_id": "...", ...fields}
Delete: {"type": "{type}/delete", "{type}_id": "..."}
```

Important: `{type}_id` is the internal `id` from the list response, NOT the entity_id.

CLI examples:
```text
ha-nova relay ws --data-file <payload-file>
ha-nova relay ws --data-file <payload-file> --out <result-file>
```

No domain reload needed — storage-based helpers take effect immediately.

See `skills/ha-nova/helper-schemas.md` for type-specific fields and constraints.

## Config-Entry Helpers

Supported helper domains:

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`

`group` is menu-driven; the live-proven end-to-end subtype is `sensor`, and other subtypes must stay anchored to the live step schema instead of guessed fields.

Canonical identity: `entry_id`

List/read source:

```json
{"type":"config_entries/get"}
{"type":"config/entity_registry/list"}
```

Current editable options readback:

```json
{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"<entry_id>","show_advanced_options":false}}
```

Create flow:

```json
{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"min_max"}}
{"method":"POST","path":"/api/config/config_entries/flow/{flow_id}","body":{"name":"..."}}
```

Use separate payload files for those two calls:

- flow start payload = handler-start body only
- capture `flow_id` from the flow-start response before calling `/flow/{flow_id}`
- flow submit payload = step form fields only

Update flow:

```json
{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"<entry_id>","show_advanced_options":false}}
{"method":"POST","path":"/api/config/config_entries/options/flow/{flow_id}","body":{"field":"value"}}
```

Use the live current options step as the update merge base:

- read current field values from `description.suggested_value`
- if a requested field is exposed but lacks `description.suggested_value`, fail loud instead of guessing the current value
- carry forward unchanged required fields
- do not submit read-only fields
- if the entry exposes no options flow on the running HA version, fail loud as unsupported update

Delete:

```json
{"method":"DELETE","path":"/api/config/config_entries/entry/{entry_id}"}
```

Verification rules:

- create/delete success is decided at the config-entry layer first
- create success = `entry_id` from the terminal flow result confirmed in the after-read, or a constrained before/after `config_entries/get` diff by `entry_id` if the flow omits it
- the before/after fallback requires a pre-create `config_entries/get` baseline
- the before/after fallback passes only when exactly one new `entry_id` appeared and that new entry matches the requested domain/title
- if the fallback diff is empty, plural, or metadata-inconsistent, fail loud as ambiguous create verification
- update success = the same `entry_id` still exists in `config_entries/get` and a reopened options flow shows the requested changed editable fields in `description.suggested_value`
- if a requested changed field is exposed in the verification step but lacks `description.suggested_value`, fail loud as unverifiable update on this HA version
- use `config_entries/get` as the source of truth for identity/existence
- resolve linked entities from `config/entity_registry/list` by matching `config_entry_id`
- linked entity appearance/disappearance is secondary evidence only

Observed locally on Markus's HA on 2026-03-19:

- all 9 supported domains completed real create/update/delete loops through relay `/core`
- raw WS `config_entries/flow` did not succeed in this session
- field-level update verification required reopening the options flow

See `skills/ha-nova/helper-flow-schemas.md` for the observed field sets and domain-specific notes.

## Domain Payload Rules

Automation fields: `alias`, `triggers`, `conditions`, `actions`, `mode`
Script fields: `alias`, `sequence`, `mode`, `description`, `fields`, `variables`

Method: create/update = `POST`, delete = `DELETE`

## Service Calls (via /core)

List all available services:
```json
{"method":"GET","path":"/api/services"}
```

Call a service:
```json
{"method":"POST","path":"/api/services/light/turn_on","body":{"entity_id":"light.living_room","brightness":128}}
```

Call with response data:
```json
{"method":"POST","path":"/api/services/weather/get_forecasts?return_response","body":{"entity_id":"weather.home","type":"daily"}}
```

Supported target fields: `entity_id` (string or array), `area_id`, `device_id`.

## Registry Queries (via /ws)

List areas:
```json
{"type":"config/area_registry/list"}
```

List devices:
```json
{"type":"config/device_registry/list"}
```

List entity registry (includes area/device assignment):
```json
{"type":"config/entity_registry/list"}
```

## Trace Queries (via /ws)

**`item_id` must be the `unique_id` (config key), NOT the entity_id slug.** See [ID Types & Resolution](#id-types--resolution).

List traces for an automation:
```json
{"type":"trace/list","domain":"automation","item_id":"{unique_id}"}
```

Get detailed trace:
```json
{"type":"trace/get","domain":"automation","item_id":"{unique_id}","run_id":"{run_id}"}
```

Trace response includes: `trace.trigger`, `trace.condition`, `trace.action` nodes with `result`, `timestamp`, and `changed_variables`.

## Error Codes (Common)

- `400 / VALIDATION_ERROR`: invalid request shape or missing fields
- `401 / UNAUTHORIZED`: relay auth token invalid
- `403 / FORBIDDEN`: request rejected by relay policy
- `404 / NOT_FOUND`: target id/path missing
- `409 / CONFLICT`: target state changed during write
- `422 / UNPROCESSABLE_ENTITY`: HA rejected payload semantics
- `502 / UPSTREAM_WS_ERROR`: relay could not reach HA websocket/upstream
- `504 / TIMEOUT`: relay upstream request timed out

## Timeout and Retry Guidance

For mutating and verify-critical calls use:
- `--connect-timeout 5`
- `--max-time 15`

On `504 / TIMEOUT`:
- verify state/config first before retrying
- retry exactly once only when verification shows no state change
- if config read-back succeeded but reload timed out, treat it as partial verification and confirm registry/state before retrying

## Safe Bulk Patterns

For bulk inspection or review preparation:
1. save discovery output with `--out <result-file>`
2. keep JSON filtering in `--jq-file` or `ha-nova relay jq --file <result-file>`
3. iterate over the saved shortlist with native file/loop tools

Do not rely on external `jq` pipes as the canonical path.

## Runtime Env

- Use `ha-nova relay` for all HA communication (auth + base URL resolved inside the CLI).
- Bootstrap check: `ha-nova relay health`
