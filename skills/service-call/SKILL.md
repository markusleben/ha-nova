---
name: service-call
description: Use when the user wants to call Home Assistant services, fire custom events, trigger known webhooks, or control alarm panels and locks through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Service Call


## Scope

Direct device/service control:
- call any HA service (`light.turn_on`, `climate.set_temperature`, etc.)
- list available services
- target by entity_id, area_id, or device_id
- fire a named custom event or trigger a known JSON webhook
- control alarm panels and locks with capability and secret-code gates

No config mutations (use `ha-nova:write` for automation/script changes).

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based payloads for service writes:
- `ha-nova relay core --method POST --path /api/services/... --body-file <payload-file>`
- `ha-nova relay core --method GET --path /api/events`
- `ha-nova relay core --method POST --path /api/events/<event_type> --body-file <payload-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>` for internal `webhook/list` metadata; both files stay in client-private scratch storage and the response never goes to stdout
- `ha-nova relay core --method POST --path /api/webhook/<webhook_id> --body-file <payload-file>`
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

Runtime calls that stay here: `scene.turn_on`, `automation.trigger`, direct `script.*` (see Automation And Script Runtime Calls), custom events, known JSON webhooks, and `lock`/`alarm_control_panel`/`cover` control under the gates below.

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
   - When the call proceeds with an `area_id`/`device_id` target, expand it to the concrete member entities the service's domain applies to BEFORE the preview, and list them there. Areas expand via WS `search/related` on the resolved area (the canonical relation source, `skills/ha-nova/bulk-patterns.md`) — registry `area_id` alone misses entities that inherit the area from their device. Execute with that expanded `entity_id` list instead of the broad target — it freezes the previewed membership into the payload, so a member added or removed after the preview is neither silently actuated nor silently skipped; the preview and verification bind to exactly that list.
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
   - User-assisted observation: a physical action by the user (pressing the device's own button, operating it by hand) never verifies a service call this skill sent — it is separate evidence about the device or input. When such an observation is needed, follow context skill → User-Assisted Readiness: read the baseline state and timestamp BEFORE the instruction, confirm ready, give one exact "act now" instruction, then re-read, compare, and attribute the result to the user's action, not to the call.
   - Transitions: covers report `opening`/`closing`, lights fade over `transition`, climate ramps — when the read-back shows a transitional or unchanged value on such a device, wait a few seconds (up to the transition length) and re-read once before calling it a discrepancy.
   - Timestamp targets: `scene.turn_on`, `button.press`, and `input_button.press` record the action as the target's state timestamp — verify that it advanced; scene member entities only as secondary evidence.
   - Stateless targets: `scene.apply` and direct `script.*` runs do not reflect the call in the target's own state. Verify the promise instead — a script via `last_triggered` or acted-on member entities, `scene.apply` via the applied member states — and say what was (not) verifiable rather than reporting a false discrepancy.
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

## Custom Events And Webhooks

Both paths are runtime actions that can start every matching automation. Never use either endpoint to probe or discover whether a listener exists.

### Custom events

1. Require the exact user-defined `event_type` and a JSON-object payload. Never normalize or invent the name, and never fire core lifecycle/state events through this flow.
2. Read `GET /api/events` for the total listener count. Scan readable automation configs for current event triggers (`trigger: event`) and legacy triggers (`platform: event`) with the same static `event_type`; apply any literal `event_data` filters to classify known matches. Templated event types and non-automation listeners are not safely enumerable — disclose that limit.
3. Inspect known matching automations for high-consequence actions. Preview the exact event type, payload fields, known matching automations, total listener count, unclassified-listener warning, and risk tier. Use natural bound confirmation unless the known impact reaches the high-consequence tier; then require `confirm:<token>`.
4. Execute `POST /api/events/<event_type>` with the JSON object. A success response proves only that Home Assistant accepted the bus fire.
5. For known matching automations, compare `last_triggered` or a new trace with the pre-call baseline using up to three reads over ten seconds. Never claim that every listener completed, and never repeat an event automatically after any timeout or transport error.

### Webhooks

1. Resolve the target automation by exact identity, then extract its static `webhook_id` internally from the stored trigger config. Scan all readable automation configs for that exact ID because multiple triggers can share one webhook. A templated ID is not safe to resolve here.
2. Call WS `webhook/list` internally with `--out <result-file>` in client-private scratch storage; never allow its secret-bearing response on stdout. Read the saved result internally, confirm that the ID is registered and `POST` is allowed, and retain only redacted metadata such as `local_only` for preview. The current Relay sends JSON; if the automation expects form/query data or another method, stop and use an explicitly authorized local caller outside this JSON-only flow.
3. Treat the webhook ID as an authentication secret: never ask the user to paste it, echo it, put it in a preview/result, or persist it outside client-private scratch storage. Only the internal request path may contain it.
4. Preview the JSON payload fields, every known matching automation, local-only status, and risk tier — never the ID. Shared IDs mean every match runs. Use the same bound/high-consequence confirmation rule as custom events.
5. Execute one `POST /api/webhook/<webhook_id>`. Home Assistant intentionally returns HTTP 200 for unknown IDs, blocked remote calls, and handler errors, so status alone is not verification.
6. Compare every known match's `last_triggered` or trace baseline with up to three reads over ten seconds. If no fresh run appears, report the outcome as unverified; never retry automatically and never weaken `local_only` to make the call work.

## Alarm Panels And Locks

Read the exact entity state immediately before preview. Never include a `code` field in a Relay payload and never ask for, accept, repeat, or store a PIN/code in chat.

Alarm panel services and feature bits:

| Service | Required `supported_features` bit | Expected terminal state |
|---|---:|---|
| `alarm_arm_home` | 1 | `armed_home` |
| `alarm_arm_away` | 2 | `armed_away` |
| `alarm_arm_night` | 4 | `armed_night` |
| `alarm_trigger` | 8 | `triggered` |
| `alarm_arm_custom_bypass` | 16 | `armed_custom_bypass` |
| `alarm_arm_vacation` | 32 | `armed_vacation` |
| `alarm_disarm` | none | `disarmed` |

For an arm action, hand off to the Home Assistant UI whenever `code_arm_required` is true, even when `code_format` is absent. For disarm/trigger, hand off when `code_format` indicates a code. `alarm_disarm` takes the typed high-consequence confirmation; `alarm_trigger` is disruptive, so warn explicitly and require bound confirmation even when no code is needed.

`lock.lock` and `lock.unlock` have no feature bit. `lock.open` requires `supported_features & 1`. If `code_format` is present, finish the action in the Home Assistant UI. `lock.unlock` and `lock.open` take the typed high-consequence confirmation; `lock.lock` uses normal bound confirmation.

Verify terminal states transition-aware: alarm panels can pass through `arming`, `pending`, or `disarming`; locks can pass through `locking`, `unlocking`, or `opening`. A service response is not proof of the physical result. Report `jammed`, `unavailable`, timeouts, or an unchanged terminal state as a discrepancy; never auto-retry a security action.

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
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No typed confirmation code needed for ordinary service calls; confirmation is still bound to the active preview.
- **High-consequence runtime actions take the typed `confirm:<token>`** like a destructive write: unlocking or opening a lock, disarming an alarm panel, opening a garage door, gate, or entry-door cover. Check `device_class` and what the entity controls — a garage door exposed as `cover.*` belongs here, a living-room blind does not. These actions grant physical access; calling the opposite service afterwards does not undo the exposure window.
- For potentially disruptive services (`homeassistant.restart`, `homeassistant.stop`), warn and ask for explicit post-preview confirmation.

## Guardrails

- One entity at a time unless user explicitly requests batch (array `entity_id` supported).
- For batch service calls, show a grouped manifest first and bind confirmation to that exact manifest.
- Verify per Flow step 6 — transition- and stateless-aware, never a naive immediate re-read.
- If state didn't change as expected after those checks, report discrepancy.
