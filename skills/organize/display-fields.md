# Organize — Display Fields

On-demand part of `../organize/SKILL.md`. Read it before changing an entity's
icon, "Show as", display precision, or unit, or an area's labels or
temperature/humidity sensor. Every rule of the skill still applies: read the
record first, preview current → new, confirm, write once, read back.

## Entity (`config/entity_registry/update`)

- **Icon**: `icon: "mdi:<name>"`; `null` brings back the default icon. Use a
  real Material Design Icons name; if unsure, offer two or ask, never guess.
- **"Show as"** (`device_class`): only for a `binary_sensor`, the one place
  Home Assistant offers it (for example `door`, `window`, `garage_door`,
  `motion`, `occupancy`, `moisture`); `null` brings back the original. A
  cover's "Show as" is refused by the CLI because it decides whether opening
  needs the one-time code: send the user to the entity's settings in Home
  Assistant. A switch shown as a light, lock, or cover is a new helper
  entity, not this field: `ha-nova:helper` (`switch_as_x`).
- **Display precision** (`sensor` with a number state): `options_domain:
  "sensor"` and `options` = the entity's current `options.sensor` object with
  only `display_precision` changed (0–6 decimals; `null` = default). Home
  Assistant replaces the whole `sensor` object, so a key left out is reset.
  It changes how the number is shown, not the stored value.
- **Unit** (`sensor` or `number` whose kind Home Assistant can convert, such
  as temperature °C/°F, speed, or energy): the same merge with
  `unit_of_measurement`. A unit change changes the state value itself, so
  number limits in automations ("above 25"), templates, and long-term
  statistics see new numbers. Before it, run the rename consumer check
  (WS `search/related` plus `ha-nova get <entity_id> --references`), name
  what may change, and capture the auto config snapshot (category
  `metadata`, `../ha-nova/config-snapshots.md`). Home Assistant ignores a
  unit it cannot convert to: verify by the state's `unit_of_measurement`
  (`ha-nova get <entity_id>`), not only the registry.
- No other `options`: a lock's or alarm panel's `default_code` holds a PIN
  (never ask for one), and voice-assistant exposure belongs to
  `ha-nova:assist`.

## Area (`config/area_registry/update`)

- **Labels**: `labels` is the full list. Read the area, then send the whole
  new list (replace, add, remove, or clear, like entity labels). Label ids
  come from `config/label_registry/list`; a missing label is created first,
  in the same preview.
- **Temperature / humidity sensor**: `temperature_entity_id` /
  `humidity_entity_id` take a `sensor` whose `device_class` is
  `temperature` / `humidity`; Home Assistant refuses anything else. Prefer
  one in that area; with several, list them by name and ask. `null` clears
  it.
