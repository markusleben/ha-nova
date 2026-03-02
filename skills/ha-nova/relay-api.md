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
~/.config/ha-nova/relay ws -d '{"type":"get_states"}'
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/my_id"}'
```

The wrapper handles auth (Keychain), headers, timeouts, and base URL internally.

## Standard Envelope

- Success: `{ "ok": true, "data": ... }`
- Error: `{ "ok": false, "error": { "code": "...", "message": "..." } }`

## /ws Contract

Request examples:
- `{ "type": "ping" }`
- `{ "type": "get_states" }`

Expected success body:
- `ok=true`
- `data` is usually an array for `get_states`

Parsing rule:
- For state reads, treat payload as `(.data // [])[]`.
- Filter only object entries with string `entity_id`.

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
