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

### 4. Template Condition

```json
{
  "alias": "Water heater off when warm enough",
  "triggers": [
    { "trigger": "state", "entity_id": "sensor.outdoor_temperature" }
  ],
  "conditions": [
    { "condition": "template", "value_template": "{{ states('sensor.outdoor_temperature') | float > 25 }}" }
  ],
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
