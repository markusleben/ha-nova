# HA NOVA Automation Best Practices

Tiered policy for automation writes.

## Snapshot Policy

- Snapshot file: `${HOME}/.cache/ha-nova/automation-bp-snapshot.json`
- Snapshot fields:
  - `automation_bp_refreshed`
  - `automation_bp_refreshed_at`
  - `automation_bp_sources`
  - `automation_bp_ha_version`
- Stale threshold: 30 days.

## Tiered Gate

- Simple automation:
  - Complexity under 3 triggers and under 3 actions.
  - Stale/missing snapshot => warning only.
  - Continue with advisory note in preview.
- Complex automation:
  - 3+ triggers or 3+ actions.
  - Stale/missing snapshot => hard gate.
  - Main thread refresh required before apply.

## Refresh Scope (minimum)

Refresh step checks:
- run modes and overlap behavior
- trigger/condition modeling guidance
- automation services + tracing guidance
- release notes affecting automation semantics

### How to Refresh

1. Research current HA automation best practices via web search (target: `home-assistant.io/docs/automation` + recent release notes).
2. Update the snapshot file with findings:
   Write `${HOME}/.cache/ha-nova/automation-bp-snapshot.json` with your native file-writing tool:
   ```json
   {
     "automation_bp_refreshed": true,
     "automation_bp_refreshed_at": "YYYY-MM-DDTHH:MM:SSZ",
     "automation_bp_sources": ["home-assistant.io/docs/automation", "home-assistant.io/blog"],
     "automation_bp_ha_version": "2026.X"
   }
   ```
3. Replace `YYYY-MM-DD...` with current timestamp, `2026.X` with the HA version the sources cover.
4. Snapshot is now `fresh` for 30 days.

## Automation Mode Selection

Pick the right `mode` based on the automation's behavior:

| Mode | When to use | Example |
|------|-------------|---------|
| `single` | Only one instance should ever run. Re-trigger while active is ignored. | Garage door open/close sequence |
| `restart` | Re-trigger should cancel the current run and start fresh. Best for motion lights with timeout. | Motion → light on → wait 2 min → light off. New motion restarts the timer. |
| `queued` | Each trigger should run in order, one after another. Use `max` to cap queue depth. | Sequential lock/unlock commands |
| `parallel` | All triggers run independently at the same time. Use `max` to cap concurrency. | Per-room climate adjustment via a single automation |

**Default is `single`** — always set mode explicitly so intent is clear.

**Common mistake:** Using `single` for motion lights. The light turns on, but re-triggering during the wait period is ignored — the light turns off too early. Use `restart` instead.

## Enforcement Checklist

1. `mode` is explicit.
2. Trigger model deterministic.
3. Prefer `entity_id` over `device_id`.
4. Avoid templates when native primitives exist (see `template-guidelines.md` → Decision Tree).
5. Add guard conditions for noisy sensors.
6. Avoid long blocking delays when helper/timer fits.
7. Promote reusable action chains to script/helper.
8. Require read-back after write.
9. Verify expected vs observed before success message.
10. Use reload only as recovery path.

## Platform Helpers vs Template Sensors

<!-- Portions adapted from homeassistant-ai/skills (MIT License)       -->
<!-- https://github.com/homeassistant-ai/skills                        -->
<!-- Copyright (c) Sergey Kadentsev (@sergeykad), Julien Lapointe (@julienld) -->

When a user needs a derived/computed sensor, prefer built-in integrations over template sensors.
These are configured via the HA UI (Settings → Helpers or Integrations), not via relay CRUD.

| Need | Use instead of template | Key benefit |
|------|------------------------|-------------|
| Average/sum/min/max of multiple sensors | `min_max` | Handles unavailable states, declarative |
| Rate of change (e.g., W/min) | `derivative` | Built-in smoothing via `time_window` |
| On/off at numeric threshold | `threshold` | Built-in hysteresis, prevents rapid toggling |
| Power → energy (W → kWh) | `integration` (Riemann sum) | Handles gaps, multiple methods |
| Consumption per billing cycle | `utility_meter` | Auto-reset, tariff support |
| Rolling stats (mean, stdev, change) | `statistics` | Time-window or sample-based buffering |
| Time in state (hours on, count) | `history_stats` | Ratio, count, or duration tracking |
| Any-on / all-on logic for entities | `group` | Replaces template binary sensors |
| Weekly on/off schedule | `schedule` | UI-editable, creates binary sensor |
| Time-of-day binary (morning/night) | `tod` | Supports sunrise/sunset offsets |

> **Relay-backed config-entry flows are now supported** for the helper-owned domains listed above through `ha-nova:helper`. Keep guidance narrow: unsupported helper families still fall back to the HA UI or `ha-nova:fallback`, and `group` must follow the live menu/form flow instead of guessed fields.

## Zigbee Button Patterns

<!-- Portions adapted from homeassistant-ai/skills (MIT License)       -->
<!-- https://github.com/homeassistant-ai/skills                        -->
<!-- Copyright (c) Sergey Kadentsev (@sergeykad), Julien Lapointe (@julienld) -->

Zigbee buttons/remotes are stateless devices — they fire events, not state changes. The trigger pattern depends on the integration.

### ZHA (Zigbee Home Automation)

Use `event` trigger with `device_ieee` (persistent across re-adds):

```yaml
triggers:
  - trigger: event
    event_type: zha_event
    event_data:
      device_ieee: "00:15:8d:00:07:26:f2:8a"
      command: "toggle"
    id: button_toggle
```

Find `device_ieee` and `command` values: Developer Tools → Events → subscribe to `zha_event` → press button.

### Zigbee2MQTT (Z2M)

Use autodiscovered `device` trigger (acceptable despite using `device_id`, because Z2M manages the mapping):

```yaml
triggers:
  - trigger: device
    device_id: abc123def456
    domain: mqtt
    type: action
    subtype: single
    id: button_single
```

Alternative — explicit MQTT topic trigger:

```yaml
triggers:
  - trigger: mqtt
    topic: "zigbee2mqtt/Bedroom Button/action"
    payload: "single"
    id: button_single
```

### ZHA vs Z2M Quick Reference

| Aspect | ZHA | Zigbee2MQTT |
|--------|-----|-------------|
| Trigger type | `event` | `device` or `mqtt` |
| Persistent ID | `device_ieee` | `device_id` (autodiscovered) |
| Event source | `zha_event` | MQTT device trigger |
| Button actions | `command` field | `type` + `subtype` |

## Failure Semantics

- Snapshot refresh failure on simple automation: continue with warning.
- Snapshot refresh failure on complex automation: block apply.
- Return structured failure with:
  - what failed
  - why
  - next concrete step

## Persistence Model (restart survival)

Which Home Assistant constructs survive a restart. Any save → modify → restore design with a restart-recovery path (a `homeassistant` start trigger) must keep the saved state in a persistent construct. Apply this during planning, before recommending an approach — not only during review.

Persistent (survive restart):
- `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime` (state restored from disk — UNLESS a YAML-configured `initial:` value is set; `initial:` overrides restore and resets the helper on every restart)
- `counter` (restored; `restore: false` opts out)
- `timer` only with `restore: true` (default does NOT restore running timers)
- YAML/UI-defined scenes (the definition persists; activating one is still a one-shot action)
- config-entry helpers and integration entities backed by restore state

Transient (lost on restart):
- `scene.create` runtime snapshots
- automation/script `variables:` (execution-scoped)
- `trigger_variables` (attach-time, see also the stale-snapshot reliability check)
- `input_*` helpers with a YAML `initial:` value (reset to that value on boot)
- running script/automation state, `wait_*` state, queued/parallel run backlog
- timers without `restore: true`

Rules:
- If a restore path is reachable from a startup trigger, every value it reads must come from the persistent list.
- Never recommend `scene.create` for snapshots that must survive a restart; use helpers instead.
- Verify the save-target helper does not set a YAML `initial:` value when the saved value must survive restarts.
- When proposing a save/restore design, state explicitly whether it survives a restart.
