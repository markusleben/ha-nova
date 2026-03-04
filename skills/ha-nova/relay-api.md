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

```bash
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}'
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/my_id"}'
```

The wrapper handles auth (Keychain), headers, timeouts, and base URL internally.

## Standard Envelope

- Success: `{ "ok": true, "data": ... }`
- Error: `{ "ok": false, "error": { "code": "...", "message": "..." } }`

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

## Domain Payload Rules

Automation fields: `alias`, `triggers`, `conditions`, `actions`, `mode`
Script fields: `alias`, `sequence`, `mode`

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

List traces for an automation:
```json
{"type":"trace/list","domain":"automation","item_id":"motion_kitchen"}
```

Get detailed trace:
```json
{"type":"trace/get","domain":"automation","item_id":"motion_kitchen","run_id":"abc123"}
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

## Curl Timeouts

For mutating and verify-critical calls use:
- `--connect-timeout 5`
- `--max-time 15`

## Runtime Env

- Main-thread: `eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"`
- Agent-dispatched: use `~/.config/ha-nova/relay` (auth + base URL resolved inside wrapper).
