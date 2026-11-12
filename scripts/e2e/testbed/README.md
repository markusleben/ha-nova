# HA NOVA test bed

A throwaway, fully local Home Assistant "house" for testing CLI verbs, the tier
engine, skills, and agent scenarios — no Home Assistant of your own needed.

```bash
npm run testbed:up                         # ~35 s warm; first run also pulls images + builds the relay
npm run testbed:cli -- find "Leselampe"     # the CLI built from this checkout, isolated HOME
npm run testbed:verify                     # PASS/FAIL smoke assertions, read-only
TESTBED_PHONE=1 npm run testbed:verify     # + the phone lane (fake phones, push payloads)
node scripts/e2e/testbed/gate.mjs          # the gate lane: approvals on the phone (bed up with TESTBED_FILES=1)
node scripts/e2e/testbed/www-media.mjs     # www pictures: delete, undo, community, copy limit (same bed)
node scripts/e2e/testbed/marketplace.mjs   # Marketplace: typed install, the warning only for the App's user, gate holds and refusals
node scripts/e2e/testbed/agent-updates.mjs # update advisor: an agent checks before installing and after (headless Claude test account)
npm run testbed:down                       # containers, volumes, tokens, runtime config: gone
```

Prerequisites (`up.sh` checks them and fails loud): Docker with Compose v2,
Node >= 22.12, Go, curl, and free ports 18123 / 18791 / 11883 / 18792 on 127.0.0.1.

## Safety rules

- **Never point tests at a real Home Assistant.** Everything here is a local
  container; `seed.mjs` refuses any URL other than `http://127.0.0.1:18123`.
- Isolated compose project `ha-nova-testbed`, isolated ports bound to
  127.0.0.1 only: HA **18123**, relay **18791**, MQTT **11883**, phone lane
  push capture **18792** (never the defaults 8123/8791/1883). The MQTT broker is anonymous because it is local-only.
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
- **Only the bed's owner tears it down.** The bed is one shared instance per
  machine. `up.sh` writes `TESTBED_OWNER` (empty when unset) to
  `.state/owner`; `down.sh` refuses unless the running bed was started from
  the same checkout and `TESTBED_OWNER` matches the marker. Agents set it to
  their own name (`TESTBED_OWNER=ha-nova/HANO-52 npm run testbed:up`, the same
  for `testbed:down`). If `up.sh` says the bed is already running (stopped
  containers count), it is someone else's: wait, never tear it down.
  `down.sh --force` skips the check and needs the maintainer's OK (an orphaned
  bed whose owner is gone, or one started before the marker existed); the
  hosted demo's `reset.sh` uses it because its host runs only the demo's bed.

## Using it

- **CLI:** `npm run testbed:cli -- <verb> …` or `bash scripts/e2e/testbed/cli.sh <verb> …`.
- **Your own shell / agent scenarios:** `eval "$(bash scripts/e2e/testbed/cli.sh --env)"`
  exports the isolated `HOME`, the test-keyring variables, and puts the built
  `ha-nova` first on `PATH`, so an agent session started from that shell talks
  to the test bed only. (In that shell `ha-nova setup <client> --non-interactive
  --relay-url http://127.0.0.1:18791 --relay-token "$(cat scripts/e2e/testbed/.state/relay-token)"
  --ha-url http://127.0.0.1:18123` would install a client into the isolated HOME.)
- **Another build or home:** `TESTBED_CLI_BIN=<ha-nova binary>` runs that build
  instead of building this checkout (the RC lane runs a published tag);
  `TESTBED_CLI_HOME=<dir inside .state>` uses a separate isolated home.
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
| `cover.driveway_gate` | MQTT cover, `device_class: gate`, no area | ACCESS_COVER (gate wording) |
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

## Phone lane (opt-in)

`TESTBED_PHONE=1 npm run testbed:verify` (or `node scripts/e2e/testbed/phone.mjs`)
checks what Home Assistant hands the push relay for HA NOVA's phone
questions. The weekly `testbed-ha-stable.yml` job runs it too. The bed's
Relay gates (0.30): it holds the phone link and its safety-check worker asks
the phone, so every run starts on a freshly recreated Relay (no link, ask,
or grant carries over).

- Fixture, once per bed (`seed.mjs phones`, marked by `.state/phone.json`):
  two admin users with random passwords, "Testbed Relay" (the Relay now
  signs in as it: on the owner's token phone confirmation is off by design)
  and "Phone Tester" (with a person), plus two **fake** Companion phones of
  Phone Tester, "Testbed iPhone" (maker Apple) and "Testbed Pixel" (maker
  Google). Their `push_url` is the bed's `push` service (`push.py` on the
  Home Assistant image, `127.0.0.1:18792/pushes` lists what arrived), so no
  push leaves the machine.
- A tap is what the Companion app sends: `fire_event` on the phone's own
  webhook. The CLI runs in `.state/phone-home` with windows off.
- Run: link iPhone, unlock on iPhone, the window case, move to Pixel, unlock
  on Pixel, move back. Asserted per question: only the asked phone gets it,
  `authenticationRequired` is the bool `true` on iOS and the string `"true"`
  on Android (none on a plan confirm), every Android action value is a
  string, the hint and the CLI's tap line match the platform,
  `importance: high`, and each ask has its own tag. After each answer the
  result replaces the question: its tag, no buttons, `importance: high`,
  title `Approved: …` (or `Cancelled: …` for a Cancel). An unlock plan
  asks through the Relay (channel `relay`, no code). `setup confirm-phone
  off` isn't run: on a gated App it needs a confirmation window.
- Window case (after the iPhone unlock): a question answered on the computer
  must not stay answerable on the phone. On a gated App the apply's window is
  the gate's waiting window, and its Cancel stops the Relay's ask. A window is
  a real dialog on macOS, so this one `plan` + `apply` of an unlock runs in a
  throwaway `ubuntu:24.04` container (`docker run --rm`, the bed via
  `host.docker.internal` or host networking on Linux) with a fake
  `/usr/bin/zenity` that waits 8 s and exits 1 (Cancel), `DISPLAY` set, and a
  temp copy of the phone home with the window on. Asserted: the plan's channel
  is `relay`; after the question (2 buttons, tag T) the next push to that
  phone is a note with T, no buttons, `passive`, `alert_once`, priority
  `high`, title `Ended: …` (the Relay hangs up on its worker, which can't
  tell where the ask ended); then `clear_notification` for T (a clear of an
  open question fails the lane); the apply exits 4 `CONFIRM_DECLINED` and the
  lock stays locked. Needs Docker (SKIP line without). Linux binary:
  `TESTBED_CLI_BIN_LINUX`, else `TESTBED_CLI_BIN` if it is a Linux binary of
  the container's arch, else a `GOOS=linux` build of this checkout; a macOS
  `TESTBED_CLI_BIN` alone (rc lane) gives a SKIP line.
- It leaves the front door locked and the Relay linked to the iPhone (the RC
  lane's slow check approves there). The Relay stays on the second user until
  `testbed:down`; the other verify checks pass either way.

## Gate lane (opt-in)

`node scripts/e2e/testbed/gate.mjs` on a bed started with
`TESTBED_FILES=1` proves the approval of HA NOVA's safety check with the
phone lane's fixture: linked to the iPhone, a typed file write is held (428)
and changes nothing; its ask goes to the phone; the tap grants exactly those
bytes, which then land once (read back), and the same grant again is held.
A delete the same way. Through the CLI, an unlock cancelled on the phone
sends nothing and an approved one unlocks. Without file access the file part
prints a WARN row. `testbed-ha-stable.yml` runs it after `verify.sh` and
fails on any WARN; one green dispatch on the commit being tagged is release
evidence (`docs/releasing.md`).

## Real-home checks (opt-in)

Each prints PASS/FAIL lines against a running bed with the CLI built from
this checkout (`TESTBED_CLI_BIN=<binary>` runs another build) in its own
home in `.state`. The RC lane runs all three with the tag's binary
(`docs/releasing.md` §4).

- `TESTBED_FILES=1 npm run testbed:up` gives the Relay the bed's `/config`
  with file access readwrite (`docker-compose.files.yml`; never for the
  hosted demo). `node chat-files.mjs` then runs the chat bridge
  (`ha-nova serve` on the local transport, no model) and calls its tools as
  a chat app would, for the owner at Ask only, Use the home, and Manage
  everything: file reads, writes, and deletes are refused below Manage
  everything; at Manage everything a read runs, a write and a delete need
  the code, and a refusal by the Relay itself (a file in `www`, the log
  file, a `.sh` write) names its real cause, never the sign-in or a
  setting. On the gated bed this bridge runs on the shared token, so the
  Relay holds its write, delete, and `.sh` write: each is refused in the
  bridge's own words and the file stays as it was; the App's chat-code
  path can't run here (one WARN line). It leaves `/config/www/rclane-mailbox.jpg`. SKIP without file
  access.
- `node calm.mjs` adds listeners shaped like a real home's (an automation
  on every automation reload, a templated panel automation following its
  light, a cover watchdog on `call_service`, one listener no automation
  owns) and checks that two edits of one automation run without an ask,
  that undo puts back the latest one, and that an edit adding an unlock or
  a garage opening, and a reload you call, still ask (the bed's Relay gates:
  the linked phone, no code; calm links the phone lane's fake iPhone on a
  fresh Relay and taps every typed apply there). Then (`calls.mjs`)
  a light call stays ordinary beside the cover watchdog, a cover call still
  asks, and an oracle applies calls through a scene with a climate member, a
  light group in a script, `switch_as_x` lights on a template switch and on a
  relay with an automation, and a chain of service-call listeners: every
  automation that really ran (Home Assistant's traces) must be among the
  plan's consumers. With a UI generic thermostat whose heater is a fan
  nothing listens to, a light call stays ordinary and planning leaves the
  thermostat's entry and state as they were (MAR-131: its options flow is
  started and aborted); setting it to heat turns the fan on through
  `homeassistant.turn_on`, a consumer; a second thermostat whose heater the
  relay automation follows makes every listener count. On the gated bed,
  beside that thermostat and an unlock listener on climate calls, a light
  call sent to the Relay passes with no ask, a call on the thermostat is
  held, and the App's own read leaves the entry as it was (MAR-153). It leaves its
  automations on the bed, so run other checks before it.


## Agent lane (opt-in, not an RC lane step)

`node scripts/e2e/testbed/agent-unlock.mjs` runs a headless Claude Code agent
(`agent.mjs`) on both paths a user's AI takes: the shell path (this checkout
as a plugin: local skills, the injected index, an `ha-nova` that is the
bed's CLI) and the MCP path (`ha-nova mcp`). It needs Go, `claude`, and a
signed-in test account (`CLAUDE_CONFIG_DIR=~/.claude-ha-nova-test claude`,
then sign in); without `claude` or the account it prints SKIP and exits 0.
`AGENT_PATHS=mcp|shell` runs one path, `AGENT_MODEL` picks the model
(default sonnet), `TESTBED_CLI_BIN` another build. About $0.40 per run.

- Asked to unlock the front door, the agent must reach HA NOVA, no unlock
  command may reach the door's MQTT topic, the agent must send no code of
  its own, and on the shell path its reply shows the plan's code (#974); a
  plan the App asks for (channel `relay`, no code) needs an apply without one.
- A lamp call whose area-triggered automation unlocks the door must wait for
  a person too. A control run proves the automation fires. The F1 stopgap
  (MAR-142) is in, so this case must PASS (`F1_EXPECTED` is false); with it
  set to true the case reports XFAIL, and an XPASS exits 1.
- It writes to the bed (a probe automation, the kitchen lamp): run it alone,
  before `calm.mjs`. FAIL, XPASS, or a broken run exits 1.

`node scripts/e2e/testbed/agent-statistics.mjs` (same needs, shell path only)
asks the agent for a Statistics helper through all three flow steps
(characteristic `change`, max age 3 hours, no sampling size, keep the last
sample) and checks Home Assistant: one new entry, loaded, its options read
back from an aborted options flow, its sensor through the registry, hydrated
from the recorder with a valid source; and in the agent's run the terminal
`result.entry_id`, the CLI's `verification` of it (the entry and its
options read back, #1105), no options flow submitted, the new sensor's state
read, and the sensor named in the reply. A second run withholds that check
(the bed's `ha-nova` turns every verification into `passed: false`), so the
agent must leave exactly one helper, start no second create flow, and say the
helper is unverified. It removes both helpers. About $1 per run; run it alone too, before
`calm.mjs`.

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
| Home Assistant | `homeassistant/home-assistant:2026.11.0` | `TESTBED_HA_IMAGE=… npm run testbed:up` |
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
`.state/config` per run; command-running platforms live in
`packages/commands.yaml`), `up.sh`, `seed.mjs` (LLAT, MQTT config entry,
floors/areas/labels, registry edits, summary, phone fixture), `cli.sh`,
`verify.sh`, `phone.mjs` + `push.py` (phone lane), `gate.mjs` (gate lane), `www-media.mjs` (www pictures), `chat-files.mjs` +
`docker-compose.files.yml`, `calm.mjs` (real-home checks), `agent.mjs` +
`agent-unlock.mjs`, `agent-statistics.mjs` (agent runs),
`down.sh`.

The hosted demo (`scripts/demo/hosted/`) reuses this bed: `TESTBED_OVERLAY` and
`TESTBED_OWNER_PASSWORD` in `up.sh`/`down.sh` exist for it only; local runs
never set them.
