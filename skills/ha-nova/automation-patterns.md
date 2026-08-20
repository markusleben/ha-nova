# HA NOVA Automation Patterns

<!-- Portions adapted from homeassistant-ai/skills (MIT License)       -->
<!-- https://github.com/homeassistant-ai/skills                        -->
<!-- Copyright (c) Sergey Kadentsev (@sergeykad), Julien Lapointe (@julienld) -->

Compact reference for native HA constructs that LLMs commonly replace with templates.
For template decision trees see `template-guidelines.md`. For review checks see `skills/review/checks.md`.

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
      entity_id: light.main_light
    data:
      brightness_pct: 80

# Wrong — entity_id under data: (deprecated)
actions:
  - action: light.turn_on
    data:
      entity_id: light.main_light
      brightness_pct: 80
```

### Multiple Targets

```yaml
target:
  entity_id:
    - light.zone_a
    - light.zone_b
  area_id: area_alpha       # all lights in that area
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

## Advanced Patterns

### Manual-Override Window

Suppress an automation for N minutes after manual control. A timer helper is the flag: it auto-expires and (with `restore: true`) survives restarts. A state change caused by another automation carries a parent context; a manual change (physical or UI) does not.

```yaml
# Automation 1 — start the override window on manual control
triggers:
  - trigger: state
    entity_id: light.hallway
actions:
  - if:
      - condition: template
        value_template: "{{ trigger.to_state.context.parent_id is none }}"
    then:
      - action: timer.start
        target: { entity_id: timer.hallway_override }
        data: { duration: "00:30:00" }

# Automation 2 — the motion automation gates on the window
conditions:
  - condition: state
    entity_id: timer.hallway_override
    state: idle
```

### Rate-Limited Notification

Cooldown via a last-notified timestamp helper (`input_datetime` with date and time). Compare as timestamps to avoid naive/aware datetime subtraction errors; the `0` default makes a never-set helper pass the gate.

```yaml
conditions:
  - condition: template
    value_template: >
      {{ as_timestamp(now()) - as_timestamp(states('input_datetime.last_leak_alert'), 0) > 3600 }}
actions:
  - action: notify.mobile_app
    data: { message: "Leak detected!" }
  - action: input_datetime.set_datetime
    target: { entity_id: input_datetime.last_leak_alert }
    data: { timestamp: "{{ now().timestamp() }}" }
```

### Presence-Simulation Loop

Randomized-within-bounds schedule while away. `random` and `now()` are evaluated by Home Assistant at each run — never pre-render a random value into the stored config, that freezes one sample forever.

```yaml
mode: single
triggers:
  - trigger: time_pattern
    minutes: "/30"
conditions:
  - condition: state
    entity_id: input_boolean.vacation_mode
    state: "on"
  - condition: sun
    after: sunset
actions:
  - delay:
      minutes: "{{ range(0, 25) | random }}"
  - action: light.toggle
    target: { entity_id: light.living_room }
```

## One-Shot And Temporary Automations

A request that should not outlive its purpose — "tell me when the laundry
finishes", "run the sprinkler for 30 minutes" — follows
`skills/ha-nova/one-shot-automations.md`: self-disabling patterns, deadline
expiry with startup recovery, and duration requests where write owns both
halves.

## Save / Restore Patterns

- Save → modify → restore designs that must survive a restart: check `skills/ha-nova/best-practices.md` → Persistence Model before choosing the storage construct. `scene.create` snapshots and `variables:` do not survive restarts.
- Pair the forward (save) branch with a state flag and guard the reverse (restore) branch on that flag; clear the flag after restoring. An unguarded reverse branch overwrites user-set state after cycles where the forward branch never ran.
