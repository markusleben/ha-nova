# Organize — Display Fields

On-demand part of `../organize/SKILL.md`. Read it before changing an entity's
icon, "Show as", display precision, or unit, or an area's labels or
temperature/humidity sensor. Every rule of the skill still applies: read the
record first, preview current → new, confirm, write once, read back.
Previews use Home Assistant's own words, not raw values: "Icon: walking
person", "Show as: Door → Window".

## Entity (`config/entity_registry/update`)

- **Icon**: `icon: "mdi:<name>"`; `null` brings back the default icon. Use a
  real Material Design Icons name; if unsure, offer two or ask, never guess.
  A fixed icon on a `binary_sensor` no longer changes with its state (motion
  and no motion look the same): say so in the preview.
- **"Show as"** (`device_class`): only for a `binary_sensor`, the one place
  Home Assistant offers it (for example `door`, `window`, `garage_door`,
  `motion`, `occupancy`, `moisture`); `null` brings back the original. A
  cover's "Show as" is refused by the CLI because it decides whether opening
  needs the one-time code: send the user to the entity's settings in Home
  Assistant. A switch shown as a light, lock, or cover is a new helper
  entity, not this field: `ha-nova:helper` (`switch_as_x`).
- **`options_domain` is the entity's own domain, and only `sensor` or
  `number`.** `options` is that domain's whole current object (from the
  record's `options.<domain>`) with only the asked key changed: Home
  Assistant replaces the whole object, so a key left out is reset. Read back
  that the other keys survived.
- **Display precision** (`sensor` with a number state): key
  `display_precision` (0–6 decimals; `null` = default). It changes how the
  number is shown, not the stored value.
- **Unit**: key `unit_of_measurement`, written exactly as Home Assistant
  writes it (`°F`, `km/h`, `kWh`). A `sensor` converts when its kind allows
  it (temperature, speed, energy, ...); a `number` converts temperature only.
  A unit change changes the state value itself: number limits in automations
  ("above 25"), templates, and long-term statistics see new numbers, and for
  a `number` every action that sets it (`number.set_value` in automations,
  scripts, scenes) now means the new unit. Before it, run the rename consumer
  check (WS `search/related` plus `ha-nova get <entity_id> --references`),
  name each consumer that may change, and capture the auto config snapshot
  (category `metadata`, `../ha-nova/config-snapshots.md`). Home Assistant
  ignores a unit it cannot convert to: verify by the state's
  `unit_of_measurement` (`ha-nova get <entity_id>`), not only the registry.
  Restore: write the snapshot's `options.<domain>` object back with the same
  `options_domain`.
- Never any other `options_domain`: not `lock` or `alarm_control_panel`
  (`default_code` holds a PIN; never ask for one or repeat one a read
  shows), not `cloud.*` (for example `disable_2fa`), `conversation`,
  `switch_as_x`, or `sensor.private`. Voice exposure to Assist belongs to
  `ha-nova:assist`; Google Assistant and Alexa settings, a weather entity's
  units, and anything else: say where it is in Home Assistant (the entity's
  settings).

## Area (`config/area_registry/update`)

- **Labels**: `labels` is the full list. Read the area, then send the whole
  new list (replace, add, remove, or clear, like entity labels). Label ids
  come from `config/label_registry/list`; a missing label is created first,
  in the same preview. For each label ADDED or REMOVED (compare the current
  and the new list), run WS `search/related` (`item_type: "label"`) and name
  each automation or script that targets it: an added label makes it also
  act on everything in this area, a removed one makes it stop.
- **Temperature / humidity sensor**: `temperature_entity_id` /
  `humidity_entity_id` take a `sensor` whose `device_class` is
  `temperature` / `humidity`; Home Assistant refuses anything else. List the
  sensors in the area (`ha-nova find --domain sensor --area <area_id> --limit
  200`; narrow until `returned` equals `total`, since a partial list can hide
  a match) and keep those whose `device_class` matches (`ha-nova get
  <entity_id>`). One:
  preview it. Several: list them by name and ask. None: say so and ask
  whether to use one from elsewhere. `null` clears it.
