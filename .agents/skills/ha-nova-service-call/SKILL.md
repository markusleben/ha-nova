---
name: ha-nova-service-call
description: Use when the user wants to call Home Assistant services (turn on lights, set temperature, toggle switches) through HA NOVA Relay.
---

# HA NOVA Service Call

<!-- ha-nova-managed-install repo_root: __HA_NOVA_REPO_ROOT__ -->

## Scope

Direct device/service control:
- call any HA service (`light.turn_on`, `climate.set_temperature`, etc.)
- list available services
- target by entity_id, area_id, or device_id

No config mutations (use `ha-nova-write` for automation/script changes).

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

## Safety

- Preview every service call before execution.
- Never guess entity IDs; resolve or ask.
- No token confirmation needed (service calls are reversible actions).
- For potentially disruptive services (e.g., `homeassistant.restart`), warn and ask for explicit confirmation.

## Guardrails

- One entity at a time unless user explicitly requests batch.
- Verify state change after call.
- If state didn't change as expected, report discrepancy.
