# Service Data Fields by Domain

Reference for `ha-nova:service-call`. Read the row for the domain you are
calling; do not load this file for a domain it does not list.

`/api/services` gives field NAMES, not this device's valid VALUES. Every list
below marked "from `<attribute>`" is read from the target's own attributes in
the pre-preview state read — never guessed, never carried over from another
device.

## Light

- `light.turn_on`: `brightness_pct` (0-100 — the skill displays brightness in
  percent), `color_temp_kelvin` (Kelvin), `rgb_color` (`[r,g,b]`),
  `transition` (seconds, bit 32), `effect` (from `effect_list`, bit 4). Colour
  and brightness are gated by `supported_color_modes`, not by a feature bit.
- Use `color_temp_kelvin`, not the older mireds field: Home Assistant's action
  schema documents Kelvin, and higher Kelvin means cooler light — the mireds
  scale ran the other way, so a converted number is not interchangeable.
- A light that is off exposes no color attributes; absent `brightness` is 0%.

## Climate

- Feature bits: TARGET_TEMPERATURE 1, TARGET_TEMPERATURE_RANGE 2,
  TARGET_HUMIDITY 4, FAN_MODE 8, PRESET_MODE 16, SWING_MODE 32, TURN_OFF 128,
  TURN_ON 256, SWING_HORIZONTAL_MODE 512.
- `climate.set_temperature` takes EITHER a single `temperature` OR the pair
  `target_temp_high` + `target_temp_low`, which are required together when the
  entity targets a range (typically `hvac_mode: heat_cool`). Read the current
  `hvac_mode` and `supported_features` first: a single-setpoint payload on a
  range device is rejected or silently misapplied.
- Mode families are separate services with separate value lists:
  `set_hvac_mode` (from `hvac_modes`), `set_preset_mode` (`preset_modes`),
  `set_fan_mode` (`fan_modes`), `set_swing_mode` (`swing_modes`),
  `set_swing_horizontal_mode` (`swing_horizontal_modes`).
- A named mode ("Eco", "Boost") is a preset on most thermostats and an hvac
  mode on others. Route it to whichever of those lists actually contains it;
  ask one question when several do. There is no `aux_heat` any more.
- `temperature` is the setpoint, `current_temperature` the sensor reading.

## Cover

- Travel: `set_cover_position` (`position` 0-100), `open_cover`,
  `close_cover`, `stop_cover`.
- Tilt is a second, independent axis with its own feature bits and its own
  attribute: `open_cover_tilt` (bit 16), `close_cover_tilt` (32),
  `stop_cover_tilt` (64), `set_cover_tilt_position` (128, `tilt_position`
  0-100), plus `toggle_cover_tilt`, which Home Assistant gates on OPEN_TILT or
  CLOSE_TILT rather than a bit of its own. A tilt request on a shutter that only
  travels, or a travel call for a tilt request, is a wrong action — read the
  tilt bits before choosing. Travel bits for comparison: `OPEN` 1, `CLOSE` 2,
  `SET_POSITION` 4, `STOP` 8.
- Write `position`/`tilt_position`; verify `current_position`/
  `current_tilt_position`.

## Fan

- `fan.set_percentage` (`percentage` 0-100; some devices treat 0 as off),
  `increase_speed`, `decrease_speed` — all three need SET_SPEED (bit 1);
  `set_preset_mode` (from `preset_modes`, bit 8); `oscillate`
  (`oscillating` boolean, bit 2); `set_direction` (`forward` / `reverse`,
  bit 4). `/api/services` lists the domain's services regardless of what a
  given fan supports, so the bit is the only per-entity gate: without it the
  call can return success while the fan ignores it.
- Discrete-speed fans expose `percentage_step`: "level 3" on a four-speed fan
  is 3 x 25 = 75. When the requested level is a word rather than a number,
  check `preset_modes` first — it is a preset, not a percentage.

## Vacuum

- `vacuum.start` (bit 8192) — the modern vacuum entity has no on/off, so
  `turn_on` is not the way to start a clean. Also `pause` (4), `stop` (8),
  `return_to_base` (16), `locate` (512), `clean_spot` (1024),
  `send_command` (256).
- `vacuum.clean_area` (Home Assistant 2026.3+, feature bit 16384) takes
  `cleaning_area_id`, a list of Home Assistant AREA ids — this is the "clean
  the kitchen" request. It also requires the vacuum's own segments to be
  mapped to those areas once in the entity settings; when the bit is absent or
  the call reports unknown areas, name that prerequisite instead of retrying.
- `set_fan_speed` (bit 32) values come from `fan_speed_list`. `send_command` is the
  integration-specific escape hatch: require the exact command and data from
  that vacuum's current integration documentation; otherwise refuse instead of
  guessing.
- States: `cleaning`, `docked`, `idle`, `paused`, `returning`, `error`.
  `returning` is transitional on the way to `docked`.

## Humidifier

- `set_humidity` (`humidity` = target), `set_mode` (from `available_modes` —
  MODES is the domain's only feature bit, value 1), `turn_on` / `turn_off`.
- `humidity` is the setpoint, `current_humidity` the sensor reading — the same
  split as climate. Dehumidifiers live in this domain too, told apart by
  `device_class`.

## Water heater

- `set_temperature` (`temperature`, degrees in the system unit — bit 1),
  `set_operation_mode` (`operation_mode`, a value from `operation_list` —
  bit 2), `set_away_mode` (`away_mode`, boolean — bit 4). `turn_on`/`turn_off`
  need bit 8. Each field is required: an entity-only payload is a schema
  error, and as with fans the bit is the only per-entity gate — `/api/services`
  lists these regardless of what this heater supports.
- The entity STATE is the operation mode, so verify a mode change by reading
  the state back, not an attribute.

## Siren

- `siren.turn_on` optionally takes `tone` (bit 4, values from
  `available_tones`), `volume_level` (bit 8), and `duration` (bit 16). A
  parameter the siren does not support is dropped silently, so confirm the bit
  before promising the user a short or quiet run.

## Not listed here

Helpers have their own section in the skill (Helper Service Patterns).
`switch`, `input_boolean`, and `button` need no extra fields. For anything
else: read the schema from `/api/services`, read the target's attributes for
the valid values, and preview both.
