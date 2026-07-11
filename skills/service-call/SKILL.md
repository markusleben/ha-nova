---
name: service-call
description: Use when the user wants to call Home Assistant services (turn on lights, set temperature, toggle switches) through HA NOVA Relay.
---

# HA NOVA Service Call


## Scope

Direct device/service control:
- call any HA service (`light.turn_on`, `climate.set_temperature`, etc.)
- list available services
- target by entity_id, area_id, or device_id

No config mutations (use `ha-nova:write` for automation/script changes).

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based payloads for service writes:
- `ha-nova relay core --method POST --path /api/services/... --body-file <payload-file>`
- `ha-nova relay core --method GET --path /api/states/<entity_id>`
- `--out <result-file>` when the response is large

## Response services

Some services return data (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`, ...) and REQUIRE the `?return_response` query parameter — without it HA returns 400 "requires responses". Path shape: `/api/services/<domain>/<service>?return_response`; the data lives under `.data.body.service_response`. Pure data services (the examples above) are reads — no write confirmation. A response-capable ACTION service (for example direct `script.<script_id>`) still follows the full preview/confirmation flow below — the parameter only adds the response, it never downgrades an action to a read.

## Flow

1. Resolve target entity (use entity discovery if name is ambiguous).
2. If service is unclear, list available services for the domain:
   - `ha-nova relay core --method GET --path /api/services`
   - Filter by relevant domain.
3. If the user names a room/area but the intended scope could be narrower, ask one clarifying question before using `area_id`.
   - Do not ask a second blocking ambiguity question in the same turn.
   - If entity resolution already consumed the one blocking question, default to the narrower confirmed target or stop and explain the ambiguity.
4. Preview the service call with stable localized slots:
   - Before preview: read current state via `ha-nova relay core --method GET --path /api/states/{entity_id}`.
   - If service changes an attribute present in the service call parameters (brightness, temperature, position, hvac_mode, etc.) OR inherently changes entity state (toggle, turn_on, turn_off, press, lock, unlock, open, close), show state delta before the call details:
     ```
     **State delta:**
     brightness: 100% → 40%
     ```
   - Attribute display rules:
     - **Brightness**: HA uses 0-255 internally. ALWAYS show delta in %: `brightness: 100% → 40%` (not raw 0-255). If light is off (brightness null or absent), treat as 0%: `brightness: 0% → 40%`.
     - **Temperature**: Show with unit: `22.5°C → 19°C`. Note: `temperature` = setpoint (what we're changing), `current_temperature` = sensor reading (do NOT use for delta).
     - **Cover position**: `position: 100% (open) → 30%`.
     - **State / mode**: For parameterless state-changing services (toggle, turn_on, turn_off, press, lock, unlock), always show state delta: `on → off`. For mode changes, show: `hvac_mode: heat → cool`.
   - Entity `unavailable` → show delta as `unavailable → {target}` with warning: "Device is offline or unreachable."
   - Entity `unknown` → show delta as `unknown → {target}` with info: "State not yet known (HA may not have polled yet). Service call may still work."
   - State read failed → preview without delta, do not block.
   - Show: service (`domain.service`), target (`entity_id`), data fields.
   - Include an explicit not-executed-yet line before confirmation.
   - Show an Options block with the execute/apply choice and `cancel`. Do not offer `show yaml` unless the user asks for raw payload details.
   - Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation). Earlier planning consent is draft-only.
5. Execute:
   - `ha-nova relay core --method POST --path /api/services/{domain}/{service} --body-file <payload-file>`
6. Verify result:
   - Check entity state after call: `ha-nova relay core --method GET --path /api/states/{entity_id}`
   - Report: service called, new state, any errors.

## Service Data Fields

Common patterns:
- `light.turn_on`: `brightness` (0-255), `color_temp` (mireds), `rgb_color` ([r,g,b])
- `climate.set_temperature`: `temperature`, `hvac_mode`
- `switch.toggle`: no extra fields needed
- `cover.set_cover_position`: `position` (0-100)

If unsure about required fields, check `/api/services` response for the service schema.

## Helper Service Patterns

Common service calls for helper entities:

- **input_boolean:** `input_boolean.turn_on`, `input_boolean.turn_off`, `input_boolean.toggle`
- **input_number:** `input_number.set_value` (`value`), `input_number.increment`, `input_number.decrement`
- **input_text:** `input_text.set_value` (`value`)
- **input_select:** `input_select.select_option` (`option`), `input_select.select_first`, `input_select.select_last`, `input_select.select_next`, `input_select.select_previous`
- **input_datetime:** `input_datetime.set_datetime` (`date`, `time`, or `datetime`)
- **input_button:** `input_button.press`
- **counter:** `counter.increment`, `counter.decrement`, `counter.reset`, `counter.set_value` (`value`)
- **timer:** `timer.start` (optional `duration`), `timer.pause`, `timer.cancel`, `timer.finish`, `timer.change` (`duration`)
- **schedule:** `schedule.reload` (reloads all schedule entities from config)

Example:
```json
{"method":"POST","path":"/api/services/input_number/set_value","body":{"entity_id":"input_number.target_temperature","value":22.5}}
```

For helper CRUD (create/update/delete helpers themselves), use `ha-nova:helper` instead.

## Automation And Script Runtime Calls

`automation.trigger`, direct script execution (`script.<script_id>`), and `script.turn_on` are live runtime actions. They may run arbitrary action sequences and can affect physical devices even when the user's intent is "just test it".

Rules:
- Never call them automatically from read, review, write, or post-write verification.
- Use this service-call flow only after a concrete preview shows the exact service, target, payload, and whether `skip_condition` is set.
- Treat `skip_condition: true` as higher risk because it bypasses automation conditions.
- Ask for confirmation bound to that exact runtime-call preview before execution.
- After execution, verify only the target automation/script state and any user-approved helper/state assertions; do not infer device safety from a successful service response alone.

## Error Handling

Full relay/upstream error taxonomy (codes, HTTP-status split, retry rules): `skills/ha-nova/relay-api.md` → Error Handling.

Service-call specifics:
- `404/NOT_FOUND` or upstream `.data.status` 404: entity or service does not exist — re-resolve before retrying
- `502/UPSTREAM_*` transport errors: HA may already have accepted the action — verify entity state first (see `relay-api.md` → Timeout and Retry Guidance); retry once only when verification shows no state change, otherwise report the result
- State verification failure (state didn't change): report discrepancy, do not retry automatically

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Report the executed service, its target, and the verified state change (or the discrepancy); summarize response-service data instead of dumping it.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No token confirmation needed for ordinary service calls; confirmation is still bound to the active preview.
- For potentially disruptive services (e.g., `homeassistant.restart`), warn and ask for explicit post-preview confirmation.

## Guardrails

- One entity at a time unless user explicitly requests batch (array `entity_id` supported).
- For batch service calls, show a grouped manifest first and bind confirmation to that exact manifest.
- Verify state change after call.
- If state didn't change as expected, report discrepancy.
