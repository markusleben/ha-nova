---
name: service-call
description: Use when the user wants to call Home Assistant services (turn on lights, set temperature, toggle switches) through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
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

## Owning-Skill Deferrals (Critical)

"Call any service" never overrides a stricter owning skill. When the request matches a row below, STOP and continue in that skill — it carries gates this flow lacks (feature checks, backup offers, typed codes, restore duties):

| Service(s) | Owning skill |
|---|---|
| `mqtt.publish` | `ha-nova:mqtt` |
| `update.install` / `update.skip` / `update.clear_skipped` | `ha-nova:updates` |
| `camera.snapshot` / `camera.record` | `ha-nova:camera` |
| `media_player.*` / `tts.*` | `ha-nova:media` |
| `notify.*` / `persistent_notification.*` | `ha-nova:notify` |
| `logger.set_level` | `ha-nova:diagnose` |

Runtime calls that stay here: `scene.turn_on`, `automation.trigger`, direct `script.*` (see Automation And Script Runtime Calls), and plain `lock`/`alarm_control_panel`/`cover` control under the high-consequence rule (see Safety).

## Response services

Some services return data (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`, ...) and REQUIRE the `?return_response` query parameter — without it HA returns 400 "requires responses". Path shape: `/api/services/<domain>/<service>?return_response`; the data lives under `.data.body.service_response`. Pure data services (the examples above) are reads — no write confirmation. A response-capable ACTION service (for example direct `script.<script_id>`) still follows the full preview/confirmation flow below — the parameter only adds the response, it never downgrades an action to a read.

### Weather forecasts

`weather.get_forecasts` is the response service for forecasts — there is no weather skill because this IS the whole API:
`POST /api/services/weather/get_forecasts?return_response` with `{"entity_id":"weather.<id>","type":"daily"}` (`hourly` / `twice_daily` also exist). The forecast list arrives under `.data.body.service_response["weather.<id>"].forecast` — bracket notation, the key contains a dot. Read-only: no confirmation needed.

## Flow

1. Resolve target entity (use entity discovery if name is ambiguous).
2. If service is unclear, list available services for the domain:
   - `ha-nova relay core --method GET --path /api/services`
   - Filter by relevant domain.
3. If the user names a room/area but the intended scope could be narrower, ask one clarifying question before using `area_id`.
   - Do not ask a second blocking ambiguity question in the same turn.
   - If entity resolution already consumed the one blocking question, default to the narrower confirmed target or stop and explain the ambiguity.
   - When the call proceeds with an `area_id`/`device_id` target, expand it to the concrete member entities the service's domain applies to BEFORE the preview, and list them there — the preview and later verification bind to that exact list. Areas expand via WS `search/related` on the resolved area (the canonical relation source, `skills/ha-nova/bulk-patterns.md`) — registry `area_id` alone misses entities that inherit the area from their device.
4. Preview the service call with stable localized slots:
   - Before preview: read current state via `ha-nova relay core --method GET --path /api/states/{entity_id}`.
   - Capability gate: if the call depends on a device capability (position, color_temp, hvac modes, sound mode, ...), check the target's attributes/`supported_features` in that state read first; if the device cannot do it, say so instead of calling (pattern: `ha-nova:media`).
   - If service changes an attribute present in the service call parameters (brightness, temperature, position, hvac_mode, etc.) OR inherently changes entity state (toggle, turn_on, turn_off, press, lock, unlock, open, close), show state delta before the call details:
     ```
     **State delta:**
     brightness: 100% → 40%
     ```
   - Two or more changing values (or batch targets): the `Field | Before | After` mini table (`output-rules.md` -> Cards) under the same `State delta:` label; one value keeps the arrow one-liner.
   - Attribute display rules:
     - **Brightness**: HA uses 0-255 internally; ALWAYS show delta in % (never raw). Light off (brightness null/absent) counts as 0%: `brightness: 0% → 40%`.
     - **Temperature**: Show with unit: `22.5°C → 19°C`; `temperature` = setpoint (what we change), `current_temperature` = sensor reading (NOT for delta).
     - **Cover position**: `position: 100% (open) → 30%`.
     - **State / mode**: parameterless state-changing services (toggle, turn_on, turn_off, press, lock, unlock): `on → off`; mode changes: `hvac_mode: heat → cool`.
   - Entity `unavailable` → delta `unavailable → {target}` + warning: "Device is offline or unreachable."
   - Entity `unknown` → delta `unknown → {target}` + info: "State not yet known; the call may still work."
   - State read failed → preview without delta, do not block.
   - Show: service (`domain.service`), target (`entity_id`), data fields.
   - Include an explicit not-executed-yet line before confirmation.
   - Show an Options block with the execute/apply choice and `cancel`. Do not offer `show yaml` unless the user asks for raw payload details.
   - Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation). Earlier planning consent is draft-only.
5. Execute:
   - `ha-nova relay core --method POST --path /api/services/{domain}/{service} --body-file <payload-file>`
6. Verify result — match the check to what the call promises:
   - Default: read the entity state back (`ha-nova relay core --method GET --path /api/states/{entity_id}`) and confirm the expected change.
   - Transitions: covers report `opening`/`closing`, lights fade over `transition`, climate ramps — when the read-back shows a transitional or unchanged value on such a device, wait a few seconds (up to the transition length) and re-read once before calling it a discrepancy.
   - Stateless targets: `button.press`/`input_button.press`, `scene.apply`, and direct `script.*` runs do not reflect the call in the target's own state. Verify the promise instead — a script via `last_triggered` or acted-on member entities, `scene.apply` via the applied member states, a button press via acceptance only — and say what was (not) verifiable rather than reporting a false discrepancy. `scene.turn_on` IS verifiable on the target: the scene entity's state is its last-activation timestamp — check that it advanced first, member entities only as secondary evidence.
   - Area/device targets: verify the member list expanded and previewed in step 3, not a single entity.
   - Report: service called, verified state (or the honest verification limit), any errors.

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
- Post-write test runs: plan structure, real-path recipes, and the post-run follow-up live in `skills/ha-nova/test-run.md`.
- When a Test Plan Card already showed the concrete preview (service, target, payload, `skip_condition`), the user's option choice on that card IS the bound confirmation — do not ask again.

## Error Handling

Full relay/upstream error taxonomy (codes, HTTP-status split, retry rules): `skills/ha-nova/relay-api.md` → Error Handling.

Service-call specifics:
- `404/NOT_FOUND` or upstream `.data.status` 404: entity or service does not exist — re-resolve before retrying
- `502/UPSTREAM_*` transport errors: HA may already have accepted the action — verify entity state first (see `relay-api.md` → Timeout and Retry Guidance); retry once only when verification shows no state change, otherwise report the result
- State verification failure (state didn't change): report discrepancy, do not retry automatically

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Previews are the runtime-action Preview Card (`apply · cancel`); results are the Result Card. Report the executed service, its target, and the verified state change (or the discrepancy); summarize response-service data as the Report shape (output-rules.md) instead of dumping it.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No token confirmation needed for ordinary service calls; confirmation is still bound to the active preview.
- **High-consequence runtime actions take the typed `confirm:<token>`** like a destructive write: unlocking a lock, disarming an alarm panel, opening a garage door, gate, or entry-door cover. Check `device_class` and what the entity controls — a garage door exposed as `cover.*` belongs here, a living-room blind does not. These actions grant physical access; calling the opposite service afterwards does not undo the exposure window.
- For potentially disruptive services (`homeassistant.restart`, `homeassistant.stop`), warn and ask for explicit post-preview confirmation.

## Guardrails

- One entity at a time unless user explicitly requests batch (array `entity_id` supported).
- For batch service calls, show a grouped manifest first and bind confirmation to that exact manifest.
- Verify per Flow step 6 — transition- and stateless-aware, never a naive immediate re-read.
- If state didn't change as expected after those checks, report discrepancy.
