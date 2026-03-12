# HA NOVA Automation Patterns

<!-- Portions adapted from homeassistant-ai/skills (MIT License)       -->
<!-- https://github.com/homeassistant-ai/skills                        -->
<!-- Copyright (c) Sergey Kadentsev (@sergeykad), Julien Lapointe (@julienld) -->

Compact reference for native HA constructs that LLMs commonly replace with templates.
For template decision trees see `template-guidelines.md`. For review checks see `checks.md`.

## Action Flow Control

### choose vs if/then

| Branches | Use | Why |
|----------|-----|-----|
| 2 (binary) | `if/then/else` | Simpler, reads like prose |
| 3+ | `choose` with `default:` | Multiple branches; `default:` prevents silent no-op (R-09) |

```yaml
# if/then — binary decision
actions:
  - if:
      - condition: sun
        after: sunset
    then:
      - action: light.turn_on
        target: { entity_id: light.porch }
    else:
      - action: light.turn_off
        target: { entity_id: light.porch }
```

### wait_for_trigger vs delay

| Need | Use | Not |
|------|-----|-----|
| "Turn off after no motion for 3 min" | `wait_for_trigger` on motion off + `for:` | `delay: 180` (ignores re-triggers) |
| Fixed pause between steps | `delay:` | `wait_for_trigger` |
| Wait for a condition to become true | `wait_template` (passes immediately if already true) | `wait_for_trigger` (waits for *change*) |

Always add `timeout:` to both (R-04). With `mode: restart`, `wait_for_trigger` resets correctly on re-trigger.

```yaml
# Motion light — restart + wait_for_trigger (canonical pattern)
mode: restart
actions:
  - action: light.turn_on
    target: { entity_id: light.hallway }
  - wait_for_trigger:
      - trigger: state
        entity_id: binary_sensor.motion_hallway
        to: "off"
        for: { minutes: 3 }
    timeout: { minutes: 10 }
  - action: light.turn_off
    target: { entity_id: light.hallway }
    data: { transition: 3 }
```

## Trigger Patterns

### Trigger IDs with choose

Use `id:` on triggers + `condition: trigger` in `choose:` branches (R-13, R-14):

```yaml
triggers:
  - trigger: state
    entity_id: binary_sensor.motion
    to: "on"
    id: motion_on
  - trigger: state
    entity_id: binary_sensor.motion
    to: "off"
    for: { minutes: 5 }
    id: motion_off
actions:
  - choose:
      - conditions: [{ condition: trigger, id: motion_on }]
        sequence:
          - action: light.turn_on
            target: { entity_id: light.hallway }
      - conditions: [{ condition: trigger, id: motion_off }]
        sequence:
          - action: light.turn_off
            target: { entity_id: light.hallway }
```

### Sun Trigger

```yaml
triggers:
  - trigger: sun
    event: sunset
    offset: "-00:30:00"   # 30 min before sunset
```

### Time Pattern Trigger

For sub-minute precision (P-04: template trigger with `now()` re-evaluates only once/min):

```yaml
triggers:
  - trigger: time_pattern
    minutes: "/5"   # every 5 minutes
```

## Native Conditions (prefer over templates)

For the full mapping of templates → native alternatives, see `template-guidelines.md` → Decision Tree.
Below are additive patterns not covered there.

### Numeric State with Duration

```yaml
conditions:
  - condition: numeric_state
    entity_id: sensor.temperature
    above: 25
    for: { minutes: 5 }
```

### State Condition with Attribute

```yaml
conditions:
  - condition: state
    entity_id: climate.thermostat
    attribute: hvac_action
    state: "heating"
```

## Targeting

### target: Structure (M-03)

```yaml
# Correct — use target: key
actions:
  - action: light.turn_on
    target:
      entity_id: light.living_room
    data:
      brightness_pct: 80

# Wrong — entity_id under data: (deprecated)
actions:
  - action: light.turn_on
    data:
      entity_id: light.living_room
      brightness_pct: 80
```

### Multiple Targets

```yaml
target:
  entity_id:
    - light.living_room
    - light.kitchen
  area_id: bedroom       # all lights in bedroom area
```

### entity_id vs device_id

Prefer `entity_id` (stable, user-controllable). `device_id` changes on re-add.

Exception: Zigbee button/remote triggers — see `best-practices.md` → Zigbee Button Patterns.

## Response Variables

Some services return data. Capture with `response_variable`:

```yaml
actions:
  - action: weather.get_forecasts
    target: { entity_id: weather.home }
    data: { type: hourly }
    response_variable: forecast
  - action: notify.mobile_app
    data:
      message: "High: {{ forecast['weather.home'].forecast[0].temperature }}°C"
```
