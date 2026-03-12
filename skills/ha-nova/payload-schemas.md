# HA NOVA Payload Schemas

Reference for AI agents constructing automation and script config payloads.
These are JSON payloads for `POST /api/config/{domain}/config/{id}` via the `/core` relay endpoint.

## Automation Payloads

Required fields: `alias`, `triggers`, `conditions`, `actions`, `mode`.
Use plural forms only (`triggers`, `conditions`, `actions`).

### 1. Simple: State Trigger + Single Action

```json
{
  "alias": "Kitchen motion light",
  "triggers": [
    { "trigger": "state", "entity_id": "binary_sensor.motion_kitchen", "to": "on" }
  ],
  "conditions": [],
  "actions": [
    { "action": "light.turn_on", "target": { "entity_id": "light.kitchen" } }
  ],
  "mode": "single"
}
```

### 2. Time Trigger + Condition

```json
{
  "alias": "Lights off at midnight when away",
  "triggers": [
    { "trigger": "time", "at": "00:00:00" }
  ],
  "conditions": [
    { "condition": "state", "entity_id": "person.markus", "state": "not_home" }
  ],
  "actions": [
    { "action": "light.turn_off", "target": { "entity_id": "light.living_room" } }
  ],
  "mode": "single"
}
```

### 3. Multi-Trigger with Choose Actions

> For multi-trigger automations, prefer trigger `id:` + `condition: trigger` routing — see compound example #6 below.

```json
{
  "alias": "Front door response",
  "triggers": [
    { "trigger": "state", "entity_id": "binary_sensor.front_door", "to": "on" },
    { "trigger": "state", "entity_id": "binary_sensor.doorbell", "to": "on" }
  ],
  "conditions": [],
  "actions": [
    {
      "choose": [
        {
          "conditions": [
            { "condition": "state", "entity_id": "binary_sensor.front_door", "state": "on" }
          ],
          "sequence": [
            { "action": "light.turn_on", "target": { "entity_id": "light.hallway" } },
            { "action": "notify.mobile_app", "data": { "message": "Front door opened" } }
          ]
        },
        {
          "conditions": [
            { "condition": "state", "entity_id": "binary_sensor.doorbell", "state": "on" }
          ],
          "sequence": [
            { "action": "media_player.play_media", "target": { "entity_id": "media_player.speaker" }, "data": { "media_content_id": "doorbell", "media_content_type": "music" } }
          ]
        }
      ]
    }
  ],
  "mode": "single"
}
```

### 4. Numeric State Trigger + Condition

```json
{
  "alias": "Water heater off when warm enough",
  "triggers": [
    { "trigger": "numeric_state", "entity_id": "sensor.outdoor_temperature", "above": 25, "for": { "minutes": 10 } }
  ],
  "conditions": [],
  "actions": [
    { "action": "switch.turn_off", "target": { "entity_id": "switch.water_heater" } }
  ],
  "mode": "single"
}
```

## Script Payloads

Required fields: `alias`, `sequence`, `mode`.
Scripts use `sequence` (not `actions`).

### 1. Simple Sequence

```json
{
  "alias": "Bedtime routine",
  "sequence": [
    { "action": "light.turn_off", "target": { "entity_id": "light.living_room" } },
    { "action": "lock.lock", "target": { "entity_id": "lock.front_door" } }
  ],
  "mode": "single"
}
```

### 2. With Variables + Choose

`fields` declares input variables shown in the HA UI.

```json
{
  "alias": "Set room brightness",
  "fields": {
    "room": { "name": "Room", "description": "Target light entity", "required": true, "selector": { "entity": { "domain": "light" } } },
    "level": { "name": "Level", "description": "low, medium, or high", "required": true, "selector": { "select": { "options": ["low", "medium", "high"] } } }
  },
  "sequence": [
    {
      "choose": [
        { "conditions": [{ "condition": "template", "value_template": "{{ level == 'low' }}" }], "sequence": [{ "action": "light.turn_on", "target": { "entity_id": "{{ room }}" }, "data": { "brightness": 50 } }] },
        { "conditions": [{ "condition": "template", "value_template": "{{ level == 'medium' }}" }], "sequence": [{ "action": "light.turn_on", "target": { "entity_id": "{{ room }}" }, "data": { "brightness": 128 } }] },
        { "conditions": [{ "condition": "template", "value_template": "{{ level == 'high' }}" }], "sequence": [{ "action": "light.turn_on", "target": { "entity_id": "{{ room }}" }, "data": { "brightness": 255 } }] }
      ]
    }
  ],
  "mode": "parallel"
}
```

### 3. With Delay + Wait Template

```json
{
  "alias": "Garage door safety close",
  "sequence": [
    { "action": "notify.mobile_app", "data": { "message": "Garage closing in 30s" } },
    { "delay": { "seconds": 30 } },
    { "wait_template": "{{ is_state('binary_sensor.garage_obstruction', 'off') }}", "timeout": "00:01:00", "continue_on_timeout": false },
    { "action": "cover.close_cover", "target": { "entity_id": "cover.garage_door" } }
  ],
  "mode": "single"
}
```

## Compound Examples

<!-- Portions adapted from homeassistant-ai/skills (MIT License)       -->
<!-- https://github.com/homeassistant-ai/skills                        -->
<!-- Copyright (c) Sergey Kadentsev (@sergeykad), Julien Lapointe (@julienld) -->

These show multiple best practices working together.

### 5. Motion Light with restart + wait_for_trigger

Demonstrates: `mode: restart`, `wait_for_trigger` with `for:`, native `time` condition, transition.
See `automation-patterns.md` → wait_for_trigger vs delay.

```json
{
  "alias": "Hallway motion light",
  "triggers": [
    { "trigger": "state", "entity_id": "binary_sensor.motion_hallway", "to": "on" }
  ],
  "conditions": [
    { "condition": "time", "after": "06:00:00", "before": "23:00:00" }
  ],
  "actions": [
    { "action": "light.turn_on", "target": { "entity_id": "light.hallway" }, "data": { "brightness_pct": 80, "transition": 1 } },
    { "wait_for_trigger": [{ "trigger": "state", "entity_id": "binary_sensor.motion_hallway", "to": "off", "for": { "minutes": 3 } }], "timeout": { "minutes": 10 }, "continue_on_timeout": true },
    { "action": "light.turn_off", "target": { "entity_id": "light.hallway" }, "data": { "transition": 3 } }
  ],
  "mode": "restart"
}
```

### 6. Multi-Trigger with trigger.id + choose

Demonstrates: trigger `id:`, `condition: trigger`, `choose` with `default:` (R-09, R-13).

```json
{
  "alias": "Front door handler",
  "triggers": [
    { "trigger": "state", "entity_id": "binary_sensor.front_door", "to": "on", "id": "door_open" },
    { "trigger": "state", "entity_id": "binary_sensor.doorbell", "to": "on", "id": "doorbell" }
  ],
  "conditions": [],
  "actions": [
    {
      "choose": [
        {
          "conditions": [{ "condition": "trigger", "id": "door_open" }],
          "sequence": [
            { "action": "light.turn_on", "target": { "entity_id": "light.hallway" } },
            { "action": "notify.mobile_app", "data": { "message": "Front door opened" } }
          ]
        },
        {
          "conditions": [{ "condition": "trigger", "id": "doorbell" }],
          "sequence": [
            { "action": "notify.mobile_app", "data": { "title": "Doorbell", "message": "Someone at the door" } }
          ]
        }
      ],
      "default": [
        { "action": "notify.mobile_app", "data": { "message": "Unknown front door event" } }
      ]
    }
  ],
  "mode": "queued",
  "max": 5
}
```

### 7. Parallel Window/Climate with trigger variable

Demonstrates: `mode: parallel`, multi-entity trigger, dynamic target via `trigger.entity_id`.

```json
{
  "alias": "Turn off climate when window opens",
  "triggers": [
    {
      "trigger": "state",
      "entity_id": ["binary_sensor.bedroom_window", "binary_sensor.living_room_window", "binary_sensor.kitchen_window"],
      "to": "on"
    }
  ],
  "conditions": [],
  "actions": [
    {
      "action": "climate.turn_off",
      "target": {
        "entity_id": "{{ state_attr(trigger.entity_id, 'climate_entity') | default('climate.' ~ (trigger.entity_id.split('.')[1] | replace('_window', ''))) }}"
      }
    }
  ],
  "mode": "parallel",
  "max": 10
}
```
