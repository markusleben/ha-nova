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

Verify relay CLI: `~/.config/ha-nova/relay health`
If this fails: `npm run onboarding:macos`

## Flow

1. Resolve target entity (use entity discovery if name is ambiguous).
2. If service is unclear, list available services for the domain:
   - `~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/services"}'`
   - Filter by relevant domain.
3. Preview the service call:
   - Show: service (`domain.service`), target (`entity_id`), data fields
   - Ask for natural confirmation.
4. Execute:
   - `~/.config/ha-nova/relay core -d '{"method":"POST","path":"/api/services/{domain}/{service}","body":{"entity_id":"{entity_id}"}}'`
5. Verify result:
   - Check entity state after call: `~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/{entity_id}"}'`
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

Example:
```json
{"method":"POST","path":"/api/services/input_number/set_value","body":{"entity_id":"input_number.target_temperature","value":22.5}}
```

For helper CRUD (create/update/delete helpers themselves), use `ha-nova:helper` instead.

## Safety

- Preview every service call before execution.
- Never guess entity IDs; resolve or ask.
- No token confirmation needed (service calls are reversible actions).
- For potentially disruptive services (e.g., `homeassistant.restart`), warn and ask for explicit confirmation.

## Error Handling

- `400/VALIDATION_ERROR`: invalid request shape — check path and body format
- `404/NOT_FOUND`: entity or service does not exist — re-resolve
- `502/UPSTREAM_WS_ERROR` or `504/TIMEOUT`: relay lost connection to HA — retry once, then report failure
- State verification failure (state didn't change): report discrepancy, do not retry automatically

## Guardrails

- One entity at a time unless user explicitly requests batch (array `entity_id` supported).
- Verify state change after call.
- If state didn't change as expected, report discrepancy.
