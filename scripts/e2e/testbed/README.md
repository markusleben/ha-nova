# HA NOVA test bed

A throwaway, fully local Home Assistant "house" for testing CLI verbs, the tier
engine, skills, and agent scenarios — no Home Assistant of your own needed.

```bash
npm run testbed:up                         # ~35 s warm; first run also pulls images + builds the relay
npm run testbed:cli -- find "Leselampe"     # the CLI built from this checkout, isolated HOME
npm run testbed:verify                     # PASS/FAIL smoke assertions, read-only
npm run testbed:down                       # containers, volumes, tokens, runtime config: gone
```

Prerequisites (`up.sh` checks them and fails loud): Docker with Compose v2,
Node >= 22.12, Go, curl, and free ports 18123 / 18791 / 11883 on 127.0.0.1.

## Safety rules

- **Never point tests at a real Home Assistant.** Everything here is a local
  container; `seed.mjs` refuses any URL other than `http://127.0.0.1:18123`.
- Isolated compose project `ha-nova-testbed`, isolated ports bound to
  127.0.0.1 only: HA **18123**, relay **18791**, MQTT **11883** (never the
  defaults 8123/8791/1883). The MQTT broker is anonymous because it is local-only.
- The CLI never touches your real `~/.config/ha-nova` or an installed `ha-nova`:
  `cli.sh` builds from `cli/` into `.state/bin` and runs it with
  `HOME=.state/home`, `HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1`, and
  `HA_NOVA_TEST_KEYRING_FILE=.state/home/.test-relay-token`.
- Nothing seeded here calls out: the rest switch polls this Home Assistant's
  own `/manifest.json`; the command_line switch runs `true`. (Stock
  integrations from `default_config`/onboarding, e.g. the weather forecast,
  may still try the internet; analytics stay off.)
- Tear down after every session (`npm run testbed:down`). It wipes `.state/`
  from inside a container because Home Assistant writes it as root.

## Using it

- **CLI:** `npm run testbed:cli -- <verb> …` or `bash scripts/e2e/testbed/cli.sh <verb> …`.
- **Your own shell / agent scenarios:** `eval "$(bash scripts/e2e/testbed/cli.sh --env)"`
  exports the isolated `HOME`, the test-keyring variables, and puts the built
  `ha-nova` first on `PATH`, so an agent session started from that shell talks
  to the test bed only. (In that shell `ha-nova setup <client> --non-interactive
  --relay-url http://127.0.0.1:18791 --relay-token "$(cat scripts/e2e/testbed/.state/relay-token)"
  --ha-url http://127.0.0.1:18123` would install a client into the isolated HOME.)
- **Raw access:** `.state/relay-token` (relay bearer), `.state/ha-llat` (HA
  long-lived token), UI at http://127.0.0.1:18123 (user `testbed`, password
  `testbed-password-1234`).
- `up.sh` prints a summary JSON (HA version, entity/device/automation counts, area ids).

## What's in the house

Floors `erdgeschoss` (EG), `obergeschoss` (OG). Areas (ids are HA slugs of the
names): `kuche` (Küche, EG), `wohnzimmer` (EG), `flur` (EG), `garage` (EG),
`schlafzimmer` (OG). Labels `security`, `lighting`. Plus the full `demo`
integration (lights, covers, locks, media players, climate, …).

| Entity | What it is | Edge case |
| --- | --- | --- |
| `light.bed_light` | demo light, Schlafzimmer, alias **Leselampe** | alias resolution |
| `lock.entrance_door` | MQTT lock, Flur, alias **Haustür** | ACCESS_LOCK; diacritic alias |
| `lock.front_door` | demo lock without registry row | NO_REGISTRY_ROW + typed |
| `cover.testbed_garage` | MQTT cover, `device_class: garage`, Garage | ACCESS_COVER |
| `cover.garage_door` | demo garage cover, Garage | ACCESS_COVER |
| `cover.kitchen_window` | demo cover, registry `device_class` override → shutter | non-access cover, ordinary |
| `switch.smart_plug` | MQTT plug; device in Küche, **entity** area Wohnzimmer | entity-level area override |
| `button.plug_factory_reset` | MQTT button on the plug | DESTRUCTIVE_BUTTON (typed) |
| `button.router_restart` | MQTT button, `device_class: restart` | USER_AUTHORED_BUTTON outranks DISRUPTIVE → typed |
| `button.modem_restart` | demo `button.push`, renamed, `device_class` override restart | DISRUPTIVE → preview |
| `button.gate_buzzer` | template button **without** unique_id, press unlocks `lock.kitchen_door` | known gap: preview, not typed |
| `sensor.testbed_temperature` | MQTT temperature sensor, Wohnzimmer | trigger of automation G |
| `switch.ac` | demo switch, disabled in the registry | never a target (NOT_FOUND) |
| `light.template_desk_lamp`, `switch.template_porch_switch`, `lock.template_side_gate`, `sensor.template_indoor_temperature` | template entities | TEMPLATE_ACTION_UNREAD; TEMPLATES_NOT_CHECKABLE note |
| `switch.garden_pump` / `switch.rest_heater` | command_line / rest switch | UNREAD_COMMAND |
| `light.living_room_group` | light group (living room + ceiling) | group expansion |
| `group.door_group` | old-style group with `lock.entrance_door` | group member containing a lock |
| helpers | `input_boolean.guest_mode`, `input_number.heat_threshold`, `input_select.house_mode`, `input_text.visitor_note`, `input_button.doorbell_test`, `timer.laundry`, `counter.visitors`, `schedule.workday` | trigger-source writes |

Automations (`config/automations.yaml`, UI-managed unless noted):

| Entity | Exercises |
| --- | --- |
| `automation.a_guest_mode_unlocks_entrance_door` | guest_mode on → `lock.unlock`: a helper write reaches a typed action through a consumer |
| `automation.b_living_room_light_follows` | ordinary consumer of `light.living_room_rgbww_lights` |
| `automation.c_disabled_kitchen_light` | `initial_state: false` |
| `automation.d_blueprint_motion_light` | built-in `homeassistant/motion_light.yaml` blueprint |
| `automation.e_webhook_turns_on_kitchen_light` | webhook `testbed-kitchen-light`, GET+POST, local_only: read verbs must never reach it |
| `automation.f_ordered_variables` | `brightness_pct` references the earlier `base` variable (key order matters) |
| `automation.g_heat_threshold_turns_on_plug` | numeric_state above `input_number.heat_threshold` |
| `automation.package_hall_light_evening` | **package** item with id: no stored config → CONFIG_NOT_UI_MANAGED |
| `automation.package_night_lights_off` | package item **without** id → YAML_NO_ID (makes calls on `light.ceiling_lights` typed) |

Scripts: `script.leave_home` (lights off + lock), `script.open_garage_after_delay`
(delay → open garage: typed stored member), `script.good_night` (scene + lock).
Scenes: `scene.movie_night` (lights + media), `scene.door_scene` (group with a lock → typed).

## Determinism and versions

`down` + `up` always yields the same entity ids, area/floor/label ids, and
config ids (MQTT entities pin `default_entity_id`; areas are slugs of fixed
names; onboarding's default areas are removed). Device ids, tokens, plan ids,
and the demo integration's random `geo_location.*` events vary per run, so
address devices through an entity. `seed.mjs house` is idempotent on a
running bed.

Pinned images (reproducible by default, override per run):

| Image | Pin | Override |
| --- | --- | --- |
| Home Assistant | `homeassistant/home-assistant:2026.10.1` | `TESTBED_HA_IMAGE=… npm run testbed:up` |
| Mosquitto | `eclipse-mosquitto:2.0.22` | `TESTBED_MQTT_IMAGE=…` |
| Relay | built from `nova/Dockerfile.standalone` at this checkout | — |

To bump a pin: edit the default in `docker-compose.yml`, run
`npm run testbed:up && npm run testbed:verify && npm run testbed:down`, and fix
whatever the new release moved (MQTT config-flow fields and demo entity ids
are the usual suspects). `.github/workflows/testbed-ha-stable.yml` does this
weekly against the current stable Home Assistant: green opens a pin-bump PR,
red opens a "Test bed fails on Home Assistant <version>" issue.

## Files

`docker-compose.yml`, `mosquitto.conf`, `config/` (committed seed, copied to
`.state/config` per run), `up.sh`, `seed.mjs` (LLAT, MQTT config entry,
floors/areas/labels, registry edits, summary), `cli.sh`, `verify.sh`, `down.sh`.
