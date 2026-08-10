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

## One-Shot And Temporary Automations

"Only today", "just once", "remind me when the laundry finishes" — the most
common everyday automation request is one that should not outlive its purpose.
A permanent automation the user has to remember to delete is the wrong answer.

Self-disabling is the Home Assistant-native one-shot: the automation turns
itself off with `automation.turn_off` on `{{ this.entity_id }}`, which
survives a restart (a long `delay` or `wait_for_trigger` does not) and re-arms
with a single toggle. WHERE that step sits in the sequence is decided per
pattern below, not fixed — it goes where a failure leaves the safer state.

```yaml
alias: One-shot — tell me when the washing machine finishes
mode: single
triggers:
  - trigger: state
    entity_id: sensor.washing_machine_state
    to: "finished"
actions:
  - action: automation.turn_off
    target:
      entity_id: "{{ this.entity_id }}"
    data:
      stop_actions: false
  - action: notify.mobile_app_phone
    data:
      message: "Laundry is done."
```

Two details carry this pattern, and both are easy to get backwards:

- **Disable FIRST, act second.** Home Assistant aborts an action sequence when
  a step errors, so a disable placed last never runs if the notification
  service is down or rejects its payload — and the one-shot stays armed for
  the next matching transition, firing days later with nobody expecting it.
  Disabling first makes "at most once" hold even when the action fails.
- **`stop_actions: false` is not optional.** That field defaults to `true`, so
  an automation turning ITSELF off cancels its own remaining steps — the
  notification would never be sent.

`{{ this.entity_id }}` avoids naming the automation inside itself, so a rename
cannot break the disable step.

A duration-bound request ("run the sprinkler for 30 minutes") is a WRITE, not
a service call, even though it starts with one. `ha-nova:service-call` runs
the turn-on and stops there; nothing schedules the turn-off. Route the whole
request to `ha-nova:write`, which owns both halves — say plainly that you are
creating a short-lived automation rather than just switching something on.

"For a duration" does not always mean "then turn it off". A thermostat set to
18 °C for an hour has to go back to what it was, not to off — so capture the
CURRENT value of every attribute the first half changes, and make the expiry
automation restore that captured value. Only a request whose natural
counter-action is off gets a plain turn-off — and "natural" means the
direction the request goes: "sprinkler for 30 minutes" turns off afterwards,
"light off for an hour" turns back ON. Capture the prior state either way; the
counter-action is whatever restores it, not a fixed service.
Read the value at write time and embed it, exactly like the deadline — and
re-read it at apply time for the same reason the deadline is re-checked: if
another client moved the thermostat from 21 to 23 during the confirmation
pause, restoring the previewed 21 would overwrite a change the user never saw.
A moved value re-previews rather than being embedded.

Two failure paths need naming because they leave the automation armed with
nothing to undo. If the immediate action definitively FAILS, clean up the expiry
automation you just created — through `ha-nova:write`'s delete flow, which
takes its own typed confirmation even for something created seconds ago;
disable it immediately so it cannot fire while that confirmation is pending — the temporary state never began, and leaving it
armed means a later unrelated change gets reverted at the deadline. And if
someone changes the target DURING the window (thermostat moved from your
temporary 18 to 23), the captured value is no longer what they want restored:
say so and offer to cancel the expiry rather than reverting a deliberate
change. The expiry automation should read the current value at fire time and
skip the restore when it no longer matches the temporary one it set.

Both halves means both, and the ORDER is the safety property: create the
expiry automation FIRST, verify it exists, and only then run the immediate
action. Check the deadline again at that moment, and require MARGIN, not just a future
deadline: the expiry automation is already armed, so a deadline a few seconds
out can fire and disable itself before the immediate action even runs — the
device then starts with nothing left to stop it. Under about a minute of
remaining time, re-preview with a fresh deadline instead of actuating. A
confirmation that arrives after the deadline re-previews for the same reason.

If the immediate action grants physical access ("unlock the front door for
five minutes", "open the garage for ten"), the one preview still takes the
typed `confirm:<token>` — a duration does not soften what the first half
does, and the automatic re-lock is not a mitigation because the door is open
for the whole window. Same tier as calling the service directly
(`skills/ha-nova/SKILL.md` → confirmation tiers). Reverse that and a failed automation write leaves the valve open with
nothing scheduled to close it — reporting the partial result does not help a
user who has already walked away. Both go under one preview; if the automation
write fails, nothing has been turned on yet and there is nothing to undo.

The counter-action must also survive Home Assistant being down at the
deadline. A bare `time` trigger is MISSED, not replayed, so the valve stays
open until someone notices. Give the expiry automation a second trigger on
startup and let a condition decide:

```yaml
alias: "One-shot: close irrigation valve at 19:30"
mode: single
triggers:
  - trigger: time
    at: "19:30:00"
  - trigger: homeassistant
    event: start
conditions:
  # on the time path this is already true; on the startup path it catches a
  # deadline that passed while Home Assistant was off
  - condition: template
    # the ABSOLUTE deadline, substituted at write time. `today_at('19:30')`
    # would mean today's 19:30 on every later day: a restart the next morning
    # reads it as not-yet-due and leaves the valve open, and a cross-midnight
    # duration closes it hours early.
    value_template: "{{ now() >= as_datetime('2026-08-09T19:30:00+02:00') }}"
actions:
  - action: valve.close_valve
    target: {entity_id: valve.irrigation_lawn}
  # HA accepting the call is not the device having closed, so confirm the
  # safe state before removing the only retry
  - wait_template: "{{ is_state('valve.irrigation_lawn', 'closed') }}"
    timeout: "00:01:00"
  - condition: state
    entity_id: valve.irrigation_lawn
    state: "closed"
  - action: automation.turn_off
    target: {entity_id: "{{ this.entity_id }}"}
    data: {stop_actions: false}
```

Note the order is the OPPOSITE of the notification one-shot above, and for the
same reason: put the disable where a failure leaves the safer state. A
notification that fails is spent, so disable first or it fires again
tomorrow. A close that fails must be retried, so disable LAST — Home Assistant
aborts the sequence on the error, the automation stays armed, and the startup
trigger closes the valve on the next restart. Never disable a safety
counter-action before it has succeeded.

A `delay` inside the run is for short waits only — it does not survive a
restart at all.

A deadline-bound one-shot needs a second way out. "Only today at 19:00" that
never fires — Home Assistant was down, the trigger entity never reached its
state — stays armed and goes off tomorrow, which is precisely the surprise the
pattern exists to prevent. Give it an expiry the main trigger cannot skip:

```yaml
triggers:
  - trigger: state
    entity_id: sensor.washing_machine_state
    to: "finished"
    id: fired
  - trigger: time
    at: "23:59:00"
    id: expired
  # a missed 23:59 leaves this armed for tomorrow, so recover at startup —
  # but only when the deadline really has passed, or an ordinary restart at
  # 18:00 would disable a one-shot that still has the evening to fire
  - trigger: homeassistant
    event: start
    id: expired
conditions:
  - condition: or
    conditions:
      # the primary path still has to be inside the window: if HA was down at
      # 23:59 and the sensor reaches its target during startup, this fires
      # after the deadline and must not count
      - condition: and
        conditions:
          - condition: trigger
            id: fired
          - condition: template
            value_template: "{{ now() < as_datetime('2026-08-10T23:59:00+02:00') }}"
      # the expiry arm must ALSO be scoped to its own triggers, or a
      # post-deadline `fired` satisfies this one and still reaches the
      # notification branch
      - condition: and
        conditions:
          - condition: trigger
            id: expired
          - condition: template
            # the ABSOLUTE deadline, substituted at write time — `today_at`
            # re-reads as the CURRENT day, so a restart the next morning
            # would see 23:59 as still ahead and leave this armed
            value_template: "{{ now() >= as_datetime('2026-08-10T23:59:00+02:00') }}"
actions:
  # disable first on both paths — a notification is spent whether or not it
  # was delivered, and a failed send must not leave this armed for tomorrow
  - action: automation.turn_off
    target: {entity_id: "{{ this.entity_id }}"}
    data: {stop_actions: false}
  - choose:
      - conditions: [{condition: trigger, id: fired}]
        sequence:
          - action: notify.mobile_app_phone
            data: {message: "Laundry is done."}
```

The disable runs on both paths, so the automation is gone either way — and the
startup trigger means a deadline missed while Home Assistant was down still
clears it, instead of leaving it to fire on tomorrow's laundry. Say in
the preview when it expires.

Rules for this family:
- Name it so it is findable later: start the `alias` with `One-shot:`. A label
  would be tidier, but labels are entity-registry metadata that
  `ha-nova:organize` owns and the write flow does not touch — promising one
  here would leave every one-shot unlabelled. The alias is written with the
  automation itself, so it is always there.
- Say in the preview that it disables itself after running once, and that it
  stays in the automation list until deleted.
- After it has fired, offer to delete it — do not delete anything unprompted.
- A recurring request ("every Monday") is NOT this pattern: that is an ordinary
  automation with a time trigger.

## Save / Restore Patterns

- Save → modify → restore designs that must survive a restart: check `skills/ha-nova/best-practices.md` → Persistence Model before choosing the storage construct. `scene.create` snapshots and `variables:` do not survive restarts.
- Pair the forward (save) branch with a state flag and guard the reverse (restore) branch on that flag; clear the flag after restoring. An unguarded reverse branch overwrites user-set state after cycles where the forward branch never ran.
