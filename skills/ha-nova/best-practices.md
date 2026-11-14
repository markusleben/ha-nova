# HA NOVA Automation Best Practices

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
2. Trigger model deterministic; room, floor, or group needs use a purpose-specific trigger with that target (2026.7+).
3. Prefer `entity_id` over `device_id`.
4. Avoid templates when native primitives exist (see `template-guidelines.md` → Decision Tree).
5. Add guard conditions for noisy sensors.
6. Avoid long blocking delays when helper/timer fits.
7. Promote reusable action chains to script/helper.
8. Require read-back after write.
9. Verify expected vs observed before success message.
10. Use reload only as recovery path.

## Triggers, Conditions, and Targets (HA 2026.7+)

From Home Assistant 2026.7 (`get_config` `version`) the automation editor builds purpose-specific triggers and conditions, named after what happens: `motion.detected`, `door.opened`, `light.turned_on`, `temperature.crossed_threshold`; conditions `light.is_on`, `occupancy.is_detected`. Older versions: the general ones only (`state`, `numeric_state`, `time`, `sun`, `template`).

- Room, floor, or group needs ("motion in the hallway", "any window upstairs"): read what the target offers, `get_triggers_for_target` / `get_conditions_for_target` (`relay-api.md` → Device Trigger Queries). A listed entry that says what the user asked for wins, with the area, floor, or label as its target: a new or swapped device there counts without an edit.
- `state`/`numeric_state`/`template` stay right for an exact state value or an attribute, or when no entry fits.
- An existing automation keeps its triggers: never convert them unasked (`smallest-solution.md` rule 7).
- Polling: never a `time_pattern` for a value a trigger can watch (`automation-patterns.md`).

Shape (verified on 2026.11):

```yaml
triggers:
  - trigger: motion.detected
    target:
      area_id: hallway
conditions:
  - condition: light.is_off
    target:
      area_id: hallway
      exclude:
        label_id: night_light
    options:
      behavior: all
actions:
  - action: light.turn_on
    target:
      area_id: hallway
```

- Settings of the trigger or condition go under `options:` (`behavior`, `for`, `threshold`, `offset`). Beside `trigger` only `target`, `id`, `alias`, `enabled`, `variables`; Home Assistant refuses `behavior` or `for` there.
- `behavior` for triggers: `each` (default, once per entity that changes), `first` (the first one; again only after all went back), `all` (when the last one gets there). For conditions: `any` (default) or `all`.
- Thresholds: `options.threshold` = `type` (`above`, `below`, `between`, `outside`) plus `value` or `value_min`/`value_max`, each `{number, unit_of_measurement}` or `{entity}`; units are converted.
- `exclude:` inside `target` takes the same keys, applies to every target, and needs a target beside it. In triggers and conditions an excluded old-style `group.` entity leaves its members in.
- A target covers the matching entities (domain and device class) of that area (the entity's own area, else its device's), floor, label, or device; never hidden, configuration, or diagnostic ones (battery triggers excepted); `first`, `all`, and conditions skip unavailable ones. The preview names what that is today ("the 2 motion sensors in the hallway").

Names (2026.11+): a shown name is the device name plus the entity's own part. Set only the entity's own part; templates use `entity_name()`, `device_name()`, `area_name()`, never `friendly_name` (`template-guidelines.md`).

## Platform Helpers vs Template Sensors

<!-- Portions adapted from homeassistant-ai/skills (MIT License)       -->
<!-- https://github.com/homeassistant-ai/skills                        -->
<!-- Copyright (c) Sergey Kadentsev (@sergeykad), Julien Lapointe (@julienld) -->

When a user needs a derived/computed sensor, prefer built-in integrations over template sensors.
All the config-entry rows below — plus `template` itself when no built-in fits — are relay-backed config-entry helpers owned by `ha-nova:helper` (`schedule` is a storage-based helper, also `ha-nova:helper`, via the `schedule/create` WS flow, not the config-entry path).

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

> **Relay-backed config-entry flows are supported** for the helper-owned domains listed above plus `template` through `ha-nova:helper`. Keep guidance narrow: `group`/`template` must follow the live menu/form flow instead of guessed fields.
