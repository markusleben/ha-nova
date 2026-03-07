# HA NOVA Helper Payload Schemas

Reference for AI agents constructing helper payloads via WebSocket CRUD commands.
These are JSON payloads for `{type}/create` and `{type}/update` via the `/ws` relay endpoint.

## When to Use Which Helper

Pick the right helper type for the job:

| Need | Use | Not |
|------|-----|-----|
| On/off flag (sleep mode, guest mode, vacation) | `input_boolean` | Template sensor |
| Numeric value with range (target temp, brightness threshold) | `input_number` | Template sensor with `set_value` service |
| User-selectable option (house mode: home/away/sleep) | `input_select` | Multiple `input_boolean` toggles |
| Free-form text (welcome message, last visitor name) | `input_text` | — |
| Specific time (wake-up time, quiet hours start) | `input_datetime` | `input_text` with manual parsing |
| Manual trigger (doorbell test, run cleanup) | `input_button` | `input_boolean` toggled on then immediately off |
| Counting occurrences (failed login attempts, door opens today) | `counter` | `input_number` with increment automation |
| Countdown / delayed action (laundry reminder, motion light timeout) | `timer` | `delay` in automation (blocks the run) |
| Recurring weekly schedule (heating, irrigation) | `schedule` | Multiple time-based automations |

**Rule of thumb:** If HA has a built-in helper for it, use the helper. Only resort to template sensors when you need derived/calculated values that no helper can store.

## Common Rules

- **Create:** `name` is always required. Type-specific fields vary.
- **Update:** `{type}_id` is always required (the internal `id` from `{type}/list`). Only include fields to change.
- **Delete:** `{type}_id` only.
- **Icon:** Optional `icon` field (e.g., `mdi:sleep`) on all types.

## input_boolean

Toggle helper (on/off state).

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `icon` | no | string | MDI icon |
| `initial` | no | boolean | State after HA restart |

```json
{"type":"input_boolean/create","name":"Sleep Mode","icon":"mdi:sleep","initial":false}
{"type":"input_boolean/update","input_boolean_id":"abc123","name":"Night Mode"}
```

## input_number

Numeric value holder with range constraints.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `min` | create | number | Minimum value |
| `max` | create | number | Maximum value |
| `step` | no | number | Increment step (default: 1) |
| `initial` | no | number | Value after HA restart |
| `unit_of_measurement` | no | string | Unit label (e.g., `°C`) |
| `mode` | no | string | `slider` (default) or `box` |
| `icon` | no | string | MDI icon |

```json
{"type":"input_number/create","name":"Target Temperature","min":16,"max":30,"step":0.5,"unit_of_measurement":"°C","mode":"slider","icon":"mdi:thermometer"}
{"type":"input_number/update","input_number_id":"abc123","max":35}
```

## input_text

Free-form text value holder.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `min` | no | number | Minimum length (default: 0) |
| `max` | no | number | Maximum length (default: 100) |
| `initial` | no | string | Value after HA restart |
| `pattern` | no | string | Regex validation pattern |
| `mode` | no | string | `text` (default) or `password` |
| `icon` | no | string | MDI icon |

```json
{"type":"input_text/create","name":"Welcome Message","min":0,"max":255,"initial":"Hello!","icon":"mdi:message-text"}
{"type":"input_text/update","input_text_id":"abc123","initial":"Welcome home!"}
```

## input_select

Dropdown option list.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `options` | create | string[] | At least 1 option required |
| `initial` | no | string | Must be in `options` list |
| `icon` | no | string | MDI icon |

```json
{"type":"input_select/create","name":"House Mode","options":["Home","Away","Sleep","Guest"],"initial":"Home","icon":"mdi:home-variant"}
{"type":"input_select/update","input_select_id":"abc123","options":["Home","Away","Sleep","Guest","Vacation"]}
```

## input_datetime

Date and/or time value holder.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `has_date` | no | boolean | Include date component (default: false) |
| `has_time` | no | boolean | Include time component (default: false) |
| `initial` | no | string | Initial value (format depends on has_date/has_time) |
| `icon` | no | string | MDI icon |

At least one of `has_date` or `has_time` must be true.

```json
{"type":"input_datetime/create","name":"Wake Up Time","has_date":false,"has_time":true,"initial":"07:00:00","icon":"mdi:alarm"}
{"type":"input_datetime/update","input_datetime_id":"abc123","initial":"06:30:00"}
```

## input_button

Press-only helper (fires event on press, no persistent state).

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `icon` | no | string | MDI icon |

```json
{"type":"input_button/create","name":"Doorbell Test","icon":"mdi:bell"}
```

## counter

Integer counter with optional bounds.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `initial` | no | number | Starting value (default: 0) |
| `step` | no | number | Increment/decrement step (default: 1) |
| `minimum` | no | number | Lower bound — **NOT `min`** |
| `maximum` | no | number | Upper bound — **NOT `max`** |
| `restore` | no | boolean | Restore value on restart (default: true) |
| `icon` | no | string | MDI icon |

**Critical:** Counter uses `minimum`/`maximum`, NOT `min`/`max` (unlike input_number).

```json
{"type":"counter/create","name":"Guest Count","initial":0,"step":1,"minimum":0,"maximum":20,"icon":"mdi:account-group"}
{"type":"counter/update","counter_id":"abc123","maximum":50}
```

## timer

Countdown timer helper.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `duration` | no | string | Default duration (`HH:MM:SS`) |
| `restore` | no | boolean | Restore running timer on restart (default: false) |
| `icon` | no | string | MDI icon |

```json
{"type":"timer/create","name":"Laundry Timer","duration":"01:30:00","restore":true,"icon":"mdi:timer"}
{"type":"timer/update","timer_id":"abc123","duration":"02:00:00"}
```

## schedule

Weekly schedule with time blocks per weekday.

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | create | string | Display name |
| `icon` | no | string | MDI icon |
| weekday keys | no | object[] | `monday`..`sunday`, each an array of `{"from":"HH:MM:SS","to":"HH:MM:SS"}` |

Schedule complexity is high. For complex schedules, recommend using the HA UI.

```json
{"type":"schedule/create","name":"Heating Schedule","icon":"mdi:calendar-clock","monday":[{"from":"06:00:00","to":"08:00:00"},{"from":"17:00:00","to":"22:00:00"}],"tuesday":[{"from":"06:00:00","to":"08:00:00"},{"from":"17:00:00","to":"22:00:00"}]}
{"type":"schedule/update","schedule_id":"abc123","monday":[{"from":"07:00:00","to":"09:00:00"}]}
```
