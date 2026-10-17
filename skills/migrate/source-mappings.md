# Migrate: Source Mappings

Reference for `../migrate/SKILL.md`. Read only the section of the source at hand. Every mapping below is a starting point: the behaviour list decides, and every entity goes through `ha-nova find` / `ha-nova get`.

## Every source

- **Re-trigger while waiting** decides `mode`: ignored → `mode: single` + `max_exceeded: silent`; timer restarts → `mode: restart`; each event runs its own copy → `mode: parallel` (or `queued` when order matters).
- "Off N minutes after X" is two different rules: N minutes after X *started* (a fixed `delay`) or N minutes after X *ended* (`wait_for_trigger` with `for:`, or a trigger `for:`). Never swap one for the other.
- A state trigger with `to:` also fires from `unavailable`/`unknown`; add `from:` or `not_from:` only when the source excluded those.
- A state trigger with neither `from:` nor `to:` also fires on attribute changes; `to: ~` limits it to state changes.
- Running delays end at a Home Assistant restart. A timer helper with `restore: true` survives one — use it only where the source survived restarts too.
- Variables stored by the source (flow/global context, Logic variables, Private Boolean, hub or global vars) become helpers: `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`.
- State shared by several rules (variables, modes, `link` targets, subflows): migrate those rules together, or name what the rules left behind lose.
- Modes and location modes become an `input_select`; rooms and zones become areas — never create areas without asking.
- Times and cron strings run in the source host's time zone; confirm it matches Home Assistant's.
- A blueprint fits only when its expanded config meets the behaviour list. The built-in `homeassistant/motion_light.yaml` turns off a set time after motion *clears*, restarts on new motion, and triggers only `from: "off"`.

## Node-RED

**Export:** Menu (☰) → Export → select the flow or nodes → JSON → Copy to clipboard or Download. Node-RED App users can also place the file into `/config`. `flows_cred.json` is never needed. **Disable:** double-click the flow tab → Enabled off (or select the nodes → Disable) → **Deploy**; nothing changes before Deploy.

The file is a JSON array of nodes: `id`, `type`, `z` (the flow tab), `name`, `wires` (one array per output). Skip nodes with `d: true` and every node on a tab with `disabled: true`; follow `link in`/`link out`/`link call` and `subflow:<id>` instances into their definitions. A `comment` or `debug` node changes nothing.

| Node `type` | Home Assistant |
|---|---|
| `server-state-changed` (Events: state) | state trigger on the node's entities — older nodes: `entityId` + `entityIdType`; version 6: `entities.entity`, `entities.substring`, `entities.regex`; `ifState` with `ifStateOperator` `is` → `to:`, `is_not` → `not_to:`, numeric operators → a state trigger plus a numeric condition or `choose` (Node-RED tests every event; `numeric_state` fires only on crossing) — the node then has TWO outputs, the second fires on every other state: check which outputs are wired; `for` → trigger `for:`; `ignorePrevState*` → `not_from:`; `ignoreCurrentState*` → `not_to:`; `outputInitially` → also a `homeassistant` start trigger and an `automation_reloaded` event trigger (it ran on every Node-RED deploy — name the difference) |
| `trigger-state` | state trigger + conditions from its constraints |
| `poll-state` | `time_pattern` + condition only when the periodic output matters; otherwise a state trigger (name the difference) |
| `ha-time` | time trigger `at: <input_datetime or timestamp sensor>` with `offset` |
| `inject` | `crontab` (`mm hh * * dow`) → time trigger with `weekday:`; `repeat` → `time_pattern`; `once` → `homeassistant` start trigger |
| `api-call-service` (Action) | the action: `action` (older nodes: `domain` + `service`), targets from `entityId`/`areaId`/`deviceId`/`labelId`, `data` (JSONata data → a template); unless `blockInputOverrides` is true, an incoming `msg.payload` with `action`/`data`/`target` overrides the node — trace upstream |
| `api-current-state` | a condition; two outputs like the events node |
| `ha-wait-until` | `wait_for_trigger` or `wait_template` with the node's `timeout`; its timeout output → `if: "{{ not wait.completed }}"` (for `wait_template`) or `wait.trigger is none`; timeout output unwired → `continue_on_timeout: false` |
| `trigger` (core) | sends `op1`, waits `duration` `units`, sends `op2`. `extend: false` (default): re-triggers are ignored while it waits → `mode: single` + `delay`. `extend: true` reschedules only `op2`, never resends `op1`: `mode: restart` + `delay` re-runs `op1`'s action on every extension — fine when it is idempotent (`turn_on`), otherwise guard it so a re-trigger only reschedules. `bytopic` other than `all`: one timer per topic value — one automation per topic, or **not migrated**. `duration: 0` waits for a reset message |
| `delay` | `pauseType: delay`: every message is delayed on its own — two motions give two separate offs → `mode: parallel` + `delay` keeps that; `rate`: `drop: true` → `mode: single` + `delay`, `drop: false` → `mode: queued` + `delay` |
| `switch` | `choose` / `if`; `checkall: "true"` (the default) runs every matching branch |
| `change` | sets message values; a `flow.`/`global.` target becomes a helper |
| `rbe` / `filter` | "block unless value changes": nothing, state triggers fire on change only; deadband/narrowband modes → a template condition on the change size |
| `function` | JavaScript: a pure expression → a template; a sequence → script steps; timers, loops over many entities, HTTP, or file access → **not migrated**: offer a script or keep it in Node-RED |

Entity ids in the export are Home Assistant ids from the time of export: `ha-nova get` each one. `entityIdType` `substring`/`regex` matches entities by pattern, including future ones — resolve today's matches with `ha-nova find` and name that a fixed list replaces the pattern.

## Homey

**Disable:** the flow's toggle in the Homey app. **Export:** flows have no file export. The Homey Developer Tools API playground (tools.developer.homey.app) returns them as JSON (`await Homey.flow.getFlows()`, `await Homey.flow.getAdvancedFlows()`); otherwise a screenshot or a written When / And / Then list per flow.

| Homey | Home Assistant |
|---|---|
| When card | trigger |
| And cards (`inverted`: "is not"; OR-groups) | conditions (`not`, `or`) |
| Then / Else cards | they run side by side, each card's delay counted from the flow start: `parallel:` branches, each with its own `delay`; with Else cards the And cards become the `if:` (as automation `conditions:` they would make Else unreachable) |
| a card's `duration` | the card's own argument (milliseconds): a `transition` for dim and light cards, otherwise read what that card does with it; never a delay |
| runs that overlap | each run delays on its own → `mode: parallel` |
| Advanced Flow `any` / `all` cards | they join execution paths of the same run — not `or` / `and` conditions; `all` → the incoming paths as `parallel:` branches, the following cards after that block (it ends when every branch is done); `any` → **not migrated** unless one path can run alone |
| Logic variables | helpers |
| zones | areas |

Card ids name the device by UUID (`homey:device:<uuid>:<card id>`): map by the device's name and what the card reads or sets. Scales: `dim` 0–1 → `brightness_pct` 0–100; `alarm_contact` true = open (`on`); `windowcoverings_set` 0–1 → `position` 0–100.

## SmartThings

**Disable:** the routine's toggle in the SmartThings app. **Export:** the SmartThings CLI lists API rules as JSON (`smartthings rules --json`); a Routine made in the app may be missing there — then a screenshot or a written If / Then list.

| SmartThings | Home Assistant |
|---|---|
| a condition in `if` (`trigger` omitted = `Auto`, or `Always`) | trigger — with `Auto` the server picks which operands trigger (`remains` over `equals` in an `and`): read the rule, never assume |
| a condition with `trigger: Never`, a routine's Precondition | condition only (it never starts the rule) |
| `changes` | fires only on becoming true — a state trigger with `to:`, or a `numeric_state` trigger, already does that |
| `every` (`specific` time, `interval`) | time trigger / `time_pattern` |
| `sleep`, routine delay | `delay` |
| `command` (capability + command) | the matching action on the mapped entity |
| location mode | `input_select` |

Map devices by name and capability (`switch`, `switchLevel` 0–100 → `brightness_pct`, `motionSensor`, `contactSensor`). Temperatures follow the location's unit; compare with the sensor's `unit_of_measurement`.

## Hubitat Rule Machine

**Disable:** the rule's Pause button. **Export:** the rule's own page shows its text (Required Expression, Triggers, Actions) — copy it, or use the rule's export file where the hub offers one.

| Rule Machine | Home Assistant |
|---|---|
| Trigger Events | triggers; "and stays N minutes" → trigger `for:` |
| Required Expression | conditions; "cancel pending actions when false" → a second trigger `id: cancel` on that entity with `mode: restart`; the first step stops the run when `trigger.id == 'cancel'` |
| Delay ... (cancelable) + Cancel Delayed Actions on re-trigger | `mode: restart` + `delay` |
| Wait for Event / Wait for Expression | `wait_for_trigger` / `wait_template`, both with `timeout:` |
| Repeat | `repeat` |
| Private Boolean, local and hub variables | helpers |
| hub mode | `input_select` |
| Motion Lighting / Room Lighting apps | one automation, or the motion light blueprint when its timing matches |

`level` 0–100 → `brightness_pct`. Fade/dim over time → `transition`.

## openHAB

**Disable:** Settings → Rules → the rule → Disable (file rules: rename the file away). **Export:** DSL files from `$OPENHAB_CONF/rules/*.rules`; JS Scripting from `automation/js/`; UI rules: Settings → Rules → the rule → Code tab (YAML).

| openHAB | Home Assistant |
|---|---|
| `Item X changed [from A] [to B]` | state trigger with `from:`/`to:` |
| `Item X received update` | fires on every update, also unchanged — a state trigger fires on changes only: name the difference |
| `Item X received command` | no exact equivalent: nearest is an event trigger on `call_service` for that entity, or a state trigger on the result — name it |
| `Member of G changed` | the group's members today, as a list — future members are not included: name it |
| `Time cron "<s m h dom mon dow>"` | time trigger or `time_pattern` (the first field is seconds); day-of-month or month fields → a template condition on `now().day` / `now().month`, else **not migrated** |
| `System started` | `homeassistant` start trigger |
| `sendCommand` | the action on the mapped entity |
| `postUpdate` | a helper value; on a device item it changes only openHAB's view — no equivalent |
| `createTimer` with `reschedule` | `mode: restart` + `delay`; guarded by "timer is null" → `mode: single` + `delay` (a timer runs outside the rule, so re-triggers are not queued) |
| `Thread::sleep` | `delay` + `mode: queued` (openHAB queues re-triggers of a running rule) |
| global `var` / `val` | helpers / literals |
| JS `rules.JSRule({ triggers, execute })`, `rules.when()...then()` | same mapping per trigger and action; arbitrary JavaScript as for Node-RED `function` |

Items map by their label and the device they are bound to. `PercentType` 0–100 → `brightness_pct`; `QuantityType` carries its unit — convert to the sensor's unit.

## Legacy YAML

Syntax-only — identical behaviour, no soak; UI items as an in-place `plan write` update, YAML items through `ha-nova:yaml-config`:

- `service:` → `action:`; `service_template:` → `action:` with the template
- trigger `platform:` → `trigger:`
- top-level `trigger:` / `condition:` / `action:` → `triggers:` / `conditions:` / `actions:`
- `data_template:` → `data:`
- `entity_id` inside `data:` → `target:` — only for an action that takes a target; where `entity_id` is a data field (e.g. `recorder.purge_entities`) it stays

A migration with a soak — the entity id or the behaviour changes:

- legacy `platform: template` entities → the `template:` integration (`ha-nova:yaml-config`) or a template helper (`ha-nova:helper`); the entity id moves to the new one: re-point consumers (`ha-nova get <entity_id> --references`)
- old-style `group.*` → a group helper of one domain; mixed domains stay a `group.*` or split
- a package or `automations.yaml` item without `id` moved into the UI: a create plus the old item disabled, then removed through `ha-nova:yaml-config`
