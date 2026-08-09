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
  `transition` (seconds), `effect` (from `effect_list`).
- Use `color_temp_kelvin`, not the older mireds field: Home Assistant's action
  schema documents Kelvin, and higher Kelvin means cooler light — the mireds
  scale ran the other way, so a converted number is not interchangeable.
- A light that is off exposes no color attributes; absent `brightness` is 0%.

## Climate

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
  0-100), plus `toggle_cover_tilt`. A tilt request on a shutter that only
  travels, or a travel call for a tilt request, is a wrong action — read the
  tilt bits before choosing. Travel bits for comparison: `OPEN` 1, `CLOSE` 2,
  `SET_POSITION` 4, `STOP` 8.
- Write `position`/`tilt_position`; verify `current_position`/
  `current_tilt_position`.

## Fan

- `fan.set_percentage` (`percentage` 0-100; some devices treat 0 as off),
  `increase_speed`, `decrease_speed`, `set_preset_mode` (from `preset_modes`),
  `oscillate` (`oscillating` boolean), `set_direction`
  (`forward` / `reverse`).
- Discrete-speed fans expose `percentage_step`: "level 3" on a four-speed fan
  is 3 x 25 = 75. When the requested level is a word rather than a number,
  check `preset_modes` first — it is a preset, not a percentage.

## Vacuum

- `vacuum.start` — the modern vacuum entity has no on/off, so `turn_on` is not
  the way to start a clean. Also `pause`, `stop`, `return_to_base`, `locate`.
- `vacuum.clean_area` (Home Assistant 2026.3+, feature bit 16384) takes
  `cleaning_area_id`, a list of Home Assistant AREA ids — this is the "clean
  the kitchen" request. It also requires the vacuum's own segments to be
  mapped to those areas once in the entity settings; when the bit is absent or
  the call reports unknown areas, name that prerequisite instead of retrying.
- `set_fan_speed` values come from `fan_speed_list`. `send_command` is the
  integration-specific escape hatch — treat an unfamiliar command as an
  unfamiliar write and route through `ha-nova:fallback`.
- States: `cleaning`, `docked`, `idle`, `paused`, `returning`, `error`.
  `returning` is transitional on the way to `docked`.

## Humidifier

- `set_humidity` (`humidity` = target), `set_mode` (from `available_modes`),
  `turn_on` / `turn_off`.
- `humidity` is the setpoint, `current_humidity` the sensor reading — the same
  split as climate. Dehumidifiers live in this domain too, told apart by
  `device_class`.

## Water heater

- `set_operation_mode` (from `operation_list`), `set_away_mode`,
  `set_temperature`.
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
