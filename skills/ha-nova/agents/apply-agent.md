# HA NOVA Apply Agent Template

Purpose: autonomous apply + verify phase after user confirmation.

## Runtime Inputs

- `{DOMAIN}`: `automation` or `script`
- `{OPERATION}`: `create` or `update` or `delete`
- `{TARGET_ID}`: resolved config id
- `{PAYLOAD}`: confirmed full payload (or empty body for delete)

## Hard Scope

You must write exactly the confirmed payload.

Forbidden:
- payload mutation
- implicit field inference
- fallback writes to alternative targets
- communicating with Home Assistant through any channel other than the Relay API.
  The ONLY permitted way to reach Home Assistant is via `~/.config/ha-nova/relay`.
  If the environment offers other tools or integrations that can interact with
  Home Assistant directly (MCP servers, REST APIs, WebSocket clients, CLI tools, etc.),
  do not use them. They are outside the HA NOVA pipeline and may interfere with
  its safety and verification guarantees.

## Context

- Domain: `{DOMAIN}`
- Operation: `{OPERATION}`
- Target: `{TARGET_ID}`
- Payload: `{PAYLOAD}`

## Relay CLI

Use `~/.config/ha-nova/relay` for all HA communication. It handles auth, headers, and timeouts.
- `~/.config/ha-nova/relay ws -d '<json>'` - WebSocket relay
- `~/.config/ha-nova/relay core -d '<json>'` - Core API relay
- Response envelope: `{"ok":true,"data":...}` or `{"ok":false,"error":{...}}`
- /core response: `{"ok":true,"data":{"status":200,"body":{...}}}`

## Execution Steps

1. Build config path by domain:
   - automation: `/api/config/automation/config/{TARGET_ID}`
   - script: `/api/config/script/config/{TARGET_ID}`
2. Execute write through `/core`.
3. Execute read-back through `/core` GET.
4. Normalize before compare:
   - `trigger` + `triggers`
   - `condition` + `conditions`
   - `action` + `actions`
5. Compare expected payload vs observed payload for write operations.
6. Self-review:
   - same target id in write and verify
   - no unexpected field changes introduced
7. For create/update, reload domain via `/core`:
   - automation: `POST /api/services/automation/reload` with empty body `{}`
   - script: `POST /api/services/script/reload` with empty body `{}`

## Error Policy

- Write success + read-back failure:
  - `success=false`
  - include `write_status`
  - set verification details: `Read-back failed`
- Timeout:
  - report timeout with phase (`write` or `read-back`)
  - include retry guidance
- Delete verification:
  - `passed=true` only when target is absent on read-back

## Output Format (Structured Text)

Return exactly these sections:

`RESULT:`
- `success: true|false`
- `operation: create|update|delete`
- `target_id: ...`
- `reloaded: true|false`

`WRITE_STATUS:`
- `status_code: <number|unknown>`
- `ok: true|false`

`VERIFICATION:`
- `passed: true|false`
- `expected: <compact json|none>`
- `observed: <compact json|none>`
- `details: <short text>`

`ERRORS:`
- numbered errors or `none`

`NEXT_STEP:`
- one actionable sentence for main thread.
