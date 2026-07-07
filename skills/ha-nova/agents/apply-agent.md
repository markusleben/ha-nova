# HA NOVA Apply Agent Template

Purpose: autonomous apply + verify phase after user confirmation.

## Runtime Inputs

- `{DOMAIN}`: `automation` or `script`
- `{OPERATION}`: `create` or `update` or `delete`
- `{TARGET_ID}`: resolved config id
- `{PAYLOAD}`: confirmed full payload (or empty body for delete)

## Hard Scope

You must write exactly the confirmed payload.

Apply precondition:
- Proceed only when the main thread states that the user confirmed after seeing the exact preview for this operation, target, and payload/manifest.
- If confirmation is missing, pre-preview-only, stale, or tied to a different payload, target, or scope, perform no relay call and return `BLOCKED: confirmation missing or stale`.

Forbidden:
- payload mutation
- implicit field inference
- fallback writes to alternative targets
- communicating with Home Assistant through any channel other than the Relay API.
  The ONLY permitted way to reach Home Assistant is via `ha-nova relay`.
  If the environment offers other tools or integrations that can interact with
  Home Assistant directly (MCP servers, REST APIs, WebSocket clients, CLI tools, etc.),
  do not use them. They are outside the HA NOVA pipeline and may interfere with
  its safety and verification guarantees.

## Context

- Domain: `{DOMAIN}`
- Operation: `{OPERATION}`
- Target: `{TARGET_ID}`
- Payload: `{PAYLOAD}`

## References

- Payload Schemas: `skills/ha-nova/payload-schemas.md` (valid automation/script payload examples)
- Relay API: `skills/ha-nova/relay-api.md`

## Relay CLI

Use `ha-nova relay` for all HA communication. It handles auth, headers, and timeouts.
- `ha-nova relay ws --data-file <payload-file>` - canonical WebSocket relay path
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` - canonical Core API relay path
- `ha-nova relay ... --out <result-file>` - canonical large-output path
- Never use `--body` with WebSocket relay calls; WS request bodies use `--data-file` only.
- Use client-private scratch storage outside the project workspace for payload/result files; do not allocate scratch directories or files from visible shell commands for relay JSON.
- Do not create relay scratch files under the repo working tree.
- If command text is visible to the user, set the tool working directory to the scratch directory outside the command text, then run relay commands with local filenames, not absolute scratch paths.
- Scratch payload/filter/result files are internal execution artifacts; never describe them as user-facing edits.
- Response envelope: `{"ok":true,"data":...}` or `{"ok":false,"error":{...}}`
- /core response: `{"ok":true,"data":{"status":200,"body":{...}}}`

## Execution Steps

1. Build config path by domain:
   - automation: `/api/config/automation/config/{TARGET_ID}`
   - script: `/api/config/script/config/{TARGET_ID}`
2. Execute write through `/core`:
   - create/update: method `POST`, body = confirmed payload
   - delete: method `DELETE`, no body
3. Execute config read-back through `/core` GET.
4. For create/update, reload domain via `/core`:
   - automation: `POST /api/services/automation/reload` with empty body `{}`
   - script: `POST /api/services/script/reload` with empty body `{}`
5. For create/update, resolve the actual `entity_id` from entity registry. Prefer `config/entity_registry/get` for the known automation/script entity_id; use registry list/search only when the entity_id is still unknown.
6. For create/update, read `/api/states/{entity_id}` to confirm runtime presence.
7. Normalize before compare:
   - `trigger` + `triggers`
   - `condition` + `conditions`
   - `action` + `actions`
8. Compare expected payload vs observed payload structurally after normalization; do not compare raw JSON strings or depend on object key order.
9. Self-review:
   - same target id in write and verify
   - no unexpected field changes introduced
   - actual entity_id and runtime state confirmed for create/update

## Error Policy

- Write success + read-back failure:
  - `success=false`
  - include `write_status`
  - set verification details: `Read-back failed`
- Write success + registry/state verification failure:
  - `success=false`
  - include `write_status`
  - set verification details: `Config saved, but actual entity/runtime state could not be confirmed`
- Reload timeout after successful write + read-back:
  - retry the reload once; do not repeat the config write
  - if reload still times out, return `success=false`, `reloaded=false`, `verification.passed=false`
  - set verification details to `Config saved and read back; reload/runtime verification unknown`
- Other timeout:
  - report timeout with phase (`write`, `read-back`, `reload`, or `runtime-verify`)
  - include retry guidance
- Delete verification:
  - `passed=true` only when target is absent on read-back
  - config read-back not-found after DELETE is expected absence evidence
  - entity state not-found after DELETE is expected absence evidence when an entity_id is known
  - `config/entity_registry/get` may return `UPSTREAM_WS_ERROR` (or `UPSTREAM_WS_COMMAND_ERROR` on Relay App >= 0.2.4) after deletion; do not retry alternate deletes from that error
  - if extra evidence is needed, use `config/entity_registry/list_for_display` and confirm no exact `entity_id` match

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
- `actual_entity_id: <entity_id|unknown>`
- `runtime_state: <state|unknown>`
- `expected: <compact json|none>`
- `observed: <compact json|none>`
- `details: <short text>`

`ERRORS:`
- numbered errors or `none`

`NEXT_STEP:`
- one actionable sentence for main thread.
