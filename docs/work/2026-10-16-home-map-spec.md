# Home map: `ha-nova map` + `home-map` skill — Spec (2026-10-16)

Status: active

## Problem

- Users ask "what do I have?", "where is this sensor used?", "review my whole
  setup", "write down why it is built this way". Models asked to count invent
  numbers and names (forum 1028637: 30 invented automation names).
- Today the answers are spread over the context skill's Home Overview and
  Structure Check, `health`, `review` aggregate mode, and `get --related`. Each
  run makes the model aggregate raw registry and state dumps itself.
- Onboarding wants a "first look" right after pairing: the house in one line,
  one or two useful observations, and three example prompts that use the
  user's real names. It needs this read-only and in under 5 s.

## Behavior

### 1. `ha-nova map` (read verb)

```
ha-nova map [--section areas|integrations|automations|unavailable|batteries]
            [--entity <entity_id>] [--first-look] [--limit N]
```

- A typed read verb: `runReadVerb` envelope (`schema_version` 1, `verb:"map"`),
  `--server`/`--via` like every verb, the I1 allowlist. No new Relay logic; one
  new WS read type on the allowlist: `config/floor_registry/list`.
- Reads (in parallel, once per invocation): `config/floor_registry/list`,
  `config/area_registry/list`, `config/device_registry/list`,
  `config/entity_registry/list`, `config_entries/get` (all entries, and
  `type_filter:["helper"]` to tell helpers from integrations), and
  `GET /api/states`. `--entity` adds `search/related` for the entity, its
  device, and the rest of the device's family (§3), plus `get_panels` and
  each `lovelace/config` (a failed dashboard read is a `not_checked` line, never an exit). Any other failed read → exit 1, no partial map (counts are never guessed).
- At most one mode: default summary, one `--section`, `--entity`, or
  `--first-look`. `--limit` (1–200) caps row lists in `--section` (default 50)
  and `--entity` (default 20); totals always stay exact.

### 2. Counting rules (the definitions the skill renders)

| Field | Definition |
|---|---|
| `devices` | device-registry rows not disabled; `devices_disabled` separately |
| `entities` | enabled entity-registry rows + entities with a state but no registry row (`unregistered`, counted inside `entities` and also on their own); `entities_disabled` separately |
| `domains` | `entities` per domain, every domain present |
| area membership | entity `area_id`, else its device's effective area; a child device (`parent_device_id`) with null `area_id` inherits its parent's (membership-resolution.md → Device rows). Same rule as `ha-nova find --area`, so "show me" follow-ups list exactly the counted rows |
| area `devices` / `entities` | enabled devices / enabled entities with that effective area |
| `devices_without_area` / `device_entities_without_area` | enabled devices with no effective area / enabled entities on such a device without their own area; `entry_type: service` devices (Supervisor Apps, sun, met, …) and device-less entities (automations, persons) never count |
| automations | from states: `on`, `off`, `unavailable` (Home Assistant could not load the config: it never runs); `restored` = `unavailable` with `restored: true` (the config is gone, only the registry row remains: an orphan, never counted as `unavailable`); `disabled` = registry-disabled rows (no state); `never_triggered` = `on` with `last_triggered` null |
| scripts / scenes | state rows; scripts also `unavailable` and `disabled` |
| helpers | storage helper entities (`input_*`, `counter`, `timer`, `schedule`) by domain + config-entry helper entries (`type_filter:["helper"]`) by domain |
| integrations | config entries except `source:"ignore"` and helper entries, grouped by domain: `entries`, `devices` (owning entry: `config_entry_id`, else `primary_config_entry`, else the first `config_entries` element), `entities` (registry `config_entry_id`, as on the integration page); `problems` = entries in `setup_error`, `setup_retry`, `migration_error`, `failed_unload`, or `not_loaded` while not disabled |
| unavailable | states `unavailable`, and `unknown` outside stateless domains (`tag`, `button`, `input_button`, `event`, `scene`, `stt`, `tts`, `conversation`, `notify`, `wake_word`, `ai_task`, `image`, `assist_satellite`); never `person`, `device_tracker`, `zone`, `geo_location` (presence, §5); `restored` counts rows Home Assistant restored without a provider (orphans); `age` = time since `last_changed` — the current state row's age, a lower bound, never the outage length; `at_least_7d` counts ages ≥ 7 d |
| batteries | battery ENTITIES (one device may carry a level and a low-battery binary sensor) with `device_class: battery`; low = numeric `< 20`, or state `low`, or a `binary_sensor` battery `on` (health skill's detector). Kinds: `phone` (`mobile_app`) → `phones_low`; `device` (`entity_category: diagnostic`, how Zigbee/Z-Wave/Matter device batteries are registered) → `low`; anything else is a charge level (a car, home storage, unclassified) → `levels_low`, never a fault |

### 3. Output shapes (`result`)

Default summary (token budget: ≤ 8 KB JSON on a 60-area / 5,000-entity home,
pinned by a Go test):

```json
{"counts":{"floors":2,"areas":9,"devices":42,"devices_disabled":1,"devices_without_area":3,
  "entities":512,"entities_disabled":30,"unregistered":14,"device_entities_without_area":11},
 "domains":{"sensor":210,"light":18},
 "floors":[{"floor_id":"ground","name":"Ground floor","areas":4}],
 "areas":{"total":9,"shown":9,"rows":[{"area_id":"kitchen","name":"Kitchen","floor_id":"ground","devices":5,"entities":31}]},
 "automations":{"total":40,"on":35,"off":3,"unavailable":1,"restored":0,"disabled":1,"never_triggered":4,
  "examples":[{"entity_id":"automation.x","name":"X","state":"unavailable"}]},
 "scripts":{"total":12,"unavailable":0,"disabled":0},"scenes":{"total":6},
 "helpers":{"total":25,"by_domain":{"input_boolean":8,"template":3}},
 "integrations":{"entries":38,"domains":31,"shown":10,"rows":[{"domain":"hue","entries":1,"devices":12,"entities":40}],
  "problems":[{"domain":"x","state":"setup_retry","entries":1}],"problems_total":1},
 "unavailable":{"total":12,"unavailable":9,"unknown":3,"at_least_7d":5,"restored":2,
  "examples":[{"entity_id":"sensor.x","name":"X","state":"unavailable","age":"9d"}]},
 "batteries":{"total":20,"low":2,"levels_low":1,"phones_low":1,"threshold":20,
  "examples":[{"entity_id":"sensor.door_battery","name":"Door battery","level":"7%","low":true,"kind":"device"}]},
 "more":["ha-nova map --section areas", "..."]}
```

- Capped lists: `areas` 20 rows (by entities, then name), `floors` 20,
  integration rows 10 (by entities), `problems` 5 (+ `problems_total`), every
  `examples` list 3 (device batteries only). Each says `total`/`shown`;
  `more` names the exact command for the full list (Progressive Detail).
  Measured: 7.4 KB on a synthetic 60-area / 5,300-entity / 150-entry home with
  long German and CJK names (Go test bound 8 KiB).
- `--section areas`: every area grouped by floor order, with a `domains` map
  per area. `integrations`: every integration row + problems. `automations`:
  every automation `{entity_id,name,state,last_triggered_age,issue}`, problems
  first (unavailable/restored, then never ran), registry-disabled ones last
  with `state:"disabled"`. `unavailable`, `batteries`: every row, sorted
  (longest age first; lowest level first). Past the 200-row ceiling `more`
  says so (narrow with `find`) instead of naming the same command again.
- `--entity <id>` (where-used): `{entity_id,name,area,device,integration,
  automations,scripts,scenes,groups,helpers,dashboards,via_device,not_checked}`. Each list is
  `{total,shown,rows:[{entity_id,name}]}` from `search/related` on the entity;
  groups also from group states listing it. Automations and scripts that
  reference its device come from `search/related` on the device MINUS what
  `search/related` returns for the rest of the device's family — its child
  devices and every family entity except this one (a device read includes
  them all) — and carry `"via":"device"`; scenes and groups never come
  from the device read. A family with more than 30 other entities and child devices skips the device
  read and says so in `not_checked`. `helpers`: other entities whose state
  attributes name it (a person's trackers, a meter's source); an unavailable
  source helper hides them and adds `N unavailable helpers (source hidden)`. `dashboards`:
  `{total,shown,rows:[{dashboard,url_path,view,card}]}` — every Lovelace panel
  from `get_panels` (storage, YAML, the default), each card, badge, or header
  card naming the id anywhere inside (each nested match in a stack or
  conditional is its own row with the path to its innermost card; no `card` = only view or dashboard settings).
  `not_checked` always lists what was not read (`templates`, `helpers that
  hide their source`, `integration options`, `disabled or unloadable
  automations and scripts`, `area, floor, and label targets`), plus
  `dashboards` when the panel list fails or `N dashboards (could not be
  read)`, so "not used" is never claimed beyond what was read. Unknown entity → exit 5 `NOT_FOUND`.

### 4. First-look contract (for the onboarding PR)

`ha-nova map --first-look` — read-only, the same reads as the summary (no
`search/related`, no config reads), < 5 s on a local Relay (measured locally on
the test bed; the session's own 30 s budget still applies). Exit 0 with:

```json
{"home":{"floors":2,"areas":9,"devices":42,"entities":512,"automations":40,"scripts":12,"scenes":6,"integrations":31},
 "observations":[
   {"kind":"battery_low","entity_id":"sensor.door_battery","name":"Door battery","area":"Hall","value":"7%","count":2},
   {"kind":"automation_unavailable","entity_id":"automation.x","name":"X","count":1}],
 "seeds":{"area":{"area_id":"living_room","name":"Living room"},
   "light":{"entity_id":"light.sofa","name":"Sofa","area":"Living room"},
   "climate":{"entity_id":"sensor.living_temp","name":"Living temperature","area":"Living room"},
   "cover":{"entity_id":"cover.terrace","name":"Terrace blind","area":"Living room"},
   "automation":{"entity_id":"automation.morning","name":"Morning"}}}
```

- `home`: the one-line counts (same definitions as §2; `automations` = total).
- `observations`: at most 2, distinct kinds, in this priority:
  `battery_low` (lowest device battery; never a phone or a charge level) →
  `automation_unavailable` (it can never run; never a restored orphan) → `integration_problem`
  (`domain`, `state`; no title) → `automation_never_triggered` →
  `unavailable_long` (≥ 7 d). Each carries `count` of its kind. Empty list = nothing worth saying; never invent one.
- `seeds`: real names for example prompts; each key only when it exists.
  `area` = the area with most entities (ties by name); entity seeds skip
  hidden and `entity_category` rows; `light` = a non-group light (no
  `entity_id` list, no `is_hue_group`), preferring
  that area; `climate` = a `climate` entity, else a temperature sensor
  (`device_class: temperature`, never a `mobile_app` phone sensor),
  preferring that area; `cover` likewise, but only a class that needs no
  code (awning, blind, curtain, damper, shade, shutter, window — a garage
  door or gate is never an everyday example);
  `automation` = the most recently triggered `on` automation. Never persons,
  device trackers, or zones.
- The onboarding skill renders: one line from `home`, the observations in
  plain words, up to three prompts (one per seed present) built from `seeds`
  in the user's language. Any
  non-zero exit → skip the first look silently (pairing already succeeded).

### 5. Privacy

- Output carries ids, friendly names, area/floor names, integration domains,
  counts, ages, and battery levels only; presence domains never appear in any
  list. No other attributes, no config-entry
  titles (they can hold account e-mails), no reasons/URLs, no coordinates, no
  person/tracker state.
- No secrets exist on these surfaces; the verb never reads configs or files.

### 6. Skill: `skills/home-map/SKILL.md`

- Jobs: "what do I have" (summary), "where is X used" (`--entity`), "review my
  whole setup" (audit report that runs `ha-nova map` first, then ranks findings
  and hands each fix to its owning skill — `health`, `review`, `maintenance`,
  `organize`, `updates`/`hacs`), "document why it is built this way".
- Never count by hand: every number in an answer comes from a map result of
  this session (or a fresh one after a write).
- Persistent notes (minimal): the "why" of an automation or script lives in
  its own `description` field — Home Assistant's native, UI-visible store —
  written through `ha-nova:write` (preview + confirmation as usual) and read
  back with `ha-nova get <id> --config`. No new notes file or store: a
  house-wide document is rendered in chat for the user to keep. A notes file
  in `/config` is deferred until users ask for it (it would need opt-in file
  access and a new file convention).
- Dispatch: the context skill routes "show me my home / what do I have / where
  is X used / audit my whole home / document my setup" to `ha-nova:home-map`;
  its Home Overview uses `ha-nova map`.

### 7. MCP

`ha_map` (read-only) maps the same flags; the `ha_skill` header maps
`ha-nova map` → `ha_map`.

## Non-goals

- Energy prefs and notify targets in where-used (`get --references` covers them).
- Automation descriptions in the map (N config reads; `get --config` per item).
- Stale-by-`last_updated` heuristics (a quiet sensor is not a fault).
- Any write, cache, or Relay change beyond the allowlist entry.

## Tests

- Go: summary counts on a fixture home (child-device area inheritance,
  disabled rows, unregistered states, stateless `unknown`, phones vs device
  batteries, ignored/helper entries); every section; `--entity` with device
  via; `--first-look` priority and seeds; flag conflicts; I1 (only allowlisted
  reads); size bound on a synthetic big home.
- Contract: skill inventory (check-docs count, PROJECT.md, architecture),
  routing row, 100% English.
- E2E: the local Docker test bed only, under the shared bed lock.
