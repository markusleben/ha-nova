# HA NOVA Template Guidelines

Prescriptive guidance for when and how to use Jinja2 templates in automations and scripts.
For syntax reference and available functions, see `docs/reference/ha-template-reference.md`.

## When to Use Templates

### Decision Tree

| You need... | Use | Not |
|-------------|-----|-----|
| Compare one entity to a value | `trigger: state` with `to:` | `trigger: template` |
| Compare numeric attribute to threshold | `trigger: numeric_state` with `above:`/`below:` | `trigger: template` with `>` / `<` |
| Time-based trigger | `trigger: time` or `time_pattern` | `trigger: template` with `now()` |
| Combine 2+ entity states in a trigger | `trigger: template` | Multiple `trigger: state` triggers |
| Calculate a value from multiple sensors | `trigger: template` (trigger) or `value_template` (condition) | Hardcoded thresholds |
| Store a user-adjustable value | Helper (`input_number`, `input_select`, etc.) | Template sensor |
| Derived read-only value reused across automations | Template sensor (via HA UI) | Inline template repeated in each automation |
| One-off inline calculation | Template in automation | Dedicated template sensor |
| Format a notification message | Template in `message:` field | Hardcoded string |

### Templates vs State Conditions

**Prefer native conditions** when checking a single entity:

```yaml
# Good: native state condition
conditions:
  - condition: state
    entity_id: input_boolean.sleep_mode
    state: "on"

# Bad: template for something native handles
conditions:
  - condition: template
    value_template: "{{ is_state('input_boolean.sleep_mode', 'on') }}"
```

**Use templates** when combining multiple entities or doing calculations:

```yaml
# Good: template needed for multi-entity logic
conditions:
  - condition: template
    value_template: >
      {{ is_state('input_boolean.guest_mode', 'off')
         and states('sensor.temperature') | float(0) < 18 }}
```

### Templates vs Template Sensors

| Inline template | Template sensor |
|----------------|-----------------|
| Used in one automation only | Same expression in 2+ automations |
| Simple, short expression | Complex multi-line calculation |
| No need to expose value in UI | Value useful on dashboards or other integrations |

**Rule of thumb:** If you find yourself copying the same template expression into a second automation, extract it to a template sensor.

## Common Patterns

### Safe Numeric Conversion

Always provide a meaningful default. See review check R-01, R-11.

```yaml
# Good: explicit default, handles unavailable
"{{ states('sensor.temperature') | float(none) }}"

# Bad: silent wrong value when sensor unavailable
"{{ states('sensor.temperature') | float(0) }}"

# Good: guard with has_value for physical quantities
conditions:
  - condition: template
    value_template: >
      {{ has_value('sensor.temperature')
         and states('sensor.temperature') | float(0) < 18 }}
```

### Notification Formatting

```yaml
actions:
  - action: notify.mobile
    data:
      message: >
        {{ state_attr('weather.home', 'friendly_name') }}:
        {{ states('weather.home') }},
        {{ state_attr('weather.home', 'temperature') }}°C
```

### Time Guards

Prefer native `time` condition over template `now()`:

```yaml
# Good: native time condition
conditions:
  - condition: time
    after: "22:00:00"
    before: "06:00:00"

# Bad: template for something native handles (also: now() re-evaluates only once/min)
conditions:
  - condition: template
    value_template: "{{ now().hour >= 22 or now().hour < 6 }}"
```

## Anti-Patterns

These are caught by review checks — listed here for reference:

| Pattern | Review Check | Why it's bad | Fix |
|---------|-------------|-------------|-----|
| `trigger: template` for single entity state | P-01 | Performance overhead | Use `trigger: state` with `to:` |
| `float(0)` on physical sensor | R-01, R-11 | 0 is wrong for temperature/humidity | Use `float(none)` + `has_value()` guard |
| `wait_template` without `timeout:` | R-04 | Blocks forever if condition never met | Add `timeout:` |
| Template trigger using `now()` | P-04 | Re-evaluates only once per minute | Use `time_pattern` for sub-minute precision |
| `states()` in `trigger_variables` | M-04 | Evaluated at attach time, immediately stale | Move to `variables:` or use in template directly |
| Templated event trigger name | R-16 | Event trigger names are attached literally; dynamic `event_type` never matches the intended event | Event trigger names must be literal strings; do not template `event_type:` |
