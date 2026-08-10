---
name: entity-discovery
description: Use when searching or resolving Home Assistant entities by name, room, or domain through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Entity Discovery


## Scope

Use for:
- listing entities by domain
- searching entities by user phrase
- bulk inventory by `prefix`, `domain`, `area`, or `label`
- resolving likely targets before writes

Read-only behavior.
- No `POST`, `PUT`, `PATCH`, or `DELETE` relay writes.
- If the user moves from discovery to mutation, stop after resolution and hand off to the write-capable skill.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based relay requests by default:
- `ha-nova relay ws --data-file <payload-file>`
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` when a body is needed
- `--jq-file <filter-file>` for non-trivial filters; keep inline `--jq` for short selectors only
- `--out <result-file>` for large reads
- On Windows PowerShell, never chain commands with `&&` or `||`; run separate shell commands instead.
- Never call external `jq`; use relay-native filters or `ha-nova relay jq --file <result-file> ...`.

## Flow

### Step 1: Search entity registry

Entity registry uses compact abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id.

Search both entity_id and name. Use short keyword stems to handle spelling variants. Always limit to 20 results.

For bulk selectors, follow `skills/ha-nova/bulk-patterns.md`.
- `prefix`: case-insensitive prefix match on the entity_id suffix and display name
- `domain`: exact domain filter
- `area`: resolve the area, then use `search/related` as the primary shortlist source; use `ai` only as optional extra evidence when present
- `label`: escalate to the full registry only when label evidence is required; `config/entity_registry/list` returns the entity array directly in `.data`
- `helper` + `area`: not a first-class bulk selector contract; do not imply room-owned helper discovery unless live helper-area semantics are explicitly defined

For domain counts or domain shortlists:
- count only the requested domain unless the user explicitly asks for heuristics or related domains
- use `--jq-file <filter-file>` for the count filter
- if you need a follow-up count from a saved file, use `ha-nova relay jq --file <result-file> length`

Create `<payload-file>` with `{"type":"config/entity_registry/list_for_display"}`, then run:

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with:

```jq
[.data.entities[] | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]
```

This generic `test("KEYWORD";"i")` example is for free-text search, not explicit `prefix` matching.
For an explicit prefix selector, match the suffix and display name with `startswith(...)`, not loose substring search.

**If the compact search does not resolve a suitable target** — no results, or only matches the user rejects or that plainly are not what they meant — escalate ONCE to the full registry (`config/entity_registry/list`) and match against `aliases[]` too — `list_for_display` does not carry them, and an alias is exactly where a household keeps the name it actually says (a household's own word for `light.floor_lamp_living`). Only then try synonyms, alternative terms, or shorter keyword stems. Use OR for multiple variants: `test("kw1|kw2|kw3";"i")`. When a resolved entity had no matching alias and the user's word was clearly their habitual name, offer once to store it as an alias via `ha-nova:organize`.
**Diacritics:** `test(...;"i")` folds case, not accents — a name with `é`/`ü`/`ö` does not match its plain-ASCII spelling. Whenever the keyword or likely entity names carry accents or umlauts (common in non-English homes), put the transliterated variants into the OR-pattern: `test("café|cafe";"i")`, and for umlauts include both the `ue`/`oe`/`ae` and bare-vowel forms.
**If too many:** narrow with AND: `test("kw1";"i") and test("kw2";"i")`.
**Cap honesty:** the `.[0:20]` cap can drop the target — when exactly 20 results return, say the list is capped and narrow further instead of treating it as complete.
**Never** dump entire domains without a user-intent keyword.

When the task is multi-target inventory:
- save the shortlist with `--out <result-file>`
- do not trim to 20 inside the initial selector filter
- dedupe first, then sort deterministically, then compute the exact matched count, then apply the 20-row display cap
- keep ordering deterministic: domain, then entity_id
- return the exact matched count separately from the displayed rows

### Step 2: Get state or config

```text
# State
ha-nova relay core --method GET --path /api/states/{entity_id}

# Automation/script config — always resolve unique_id first (see relay-api.md → ID Types)
ha-nova relay ws --data-file <payload-file> --out <registry-file>
ha-nova relay jq -r --file <registry-file> '.data.unique_id'
ha-nova relay core --method GET --path /api/config/automation/config/{unique_id} --jq-file <filter-file> --out <result-file>
# For scripts: use script.{slug} and /api/config/script/config/{unique_id}
```

For `<filter-file>`, prefer copying `skills/ha-nova/config-body-filter.jq`; if the canonical file is unavailable (flat-copy installs), recreate it with exactly:

```jq
if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
```

### Step 3: Find automations related to a device or area

Automations rarely have reliable direct `area_id` data in the compact registry. When user asks "automations for X in room Y":

1. Resolve room name to area_id:
   `ha-nova relay ws --data-file <payload-file>` then filter by name with `--jq`
2. Query the area directly:
   `{"type":"search/related","item_type":"area","item_id":"<area_id>"}`
3. Treat the response as a keyed object, not an array:
   - automation discovery uses `.data.automation`
   - script discovery uses `.data.script`
   - entity discovery uses `.data.entity`
4. If automation/script keys are absent and only area entities are present, use `search/related` on those entities to derive the automation/script shortlist:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```

This is more reliable than keyword search or assuming `.ai` is populated for room-based queries.

### Step 4: Return shortlist

- `entity_id`, `friendly_name`, `state` (if fetched), short relevance reason
- for bulk inventory: filter used, matched count, displayed rows
- do not fetch full YAML for every matched item in one response

**IMPORTANT:** Never dump raw `get_states` — it returns thousands of entities with full attributes.

## State Snapshot Queries

"is everything closed?", "who is home?", "what is running right now?" — these ask
about STATE across many entities, not about finding one. They are reads and
belong here; `ha-nova:health` answers what is broken, not what is on.

One call answers them: `ha-nova relay core --method GET --path /api/states --out <result-file>`,
then filter by domain plus `device_class` and state:

```jq
[.data.body[]
 | select(.attributes.device_class == "window" and .state == "on")
 | {e: .entity_id, n: .attributes.friendly_name}]
```

- open windows/doors: `binary_sensor` with `device_class` `window`/`door`/`garage_door`/`opening` (the generic contact class many integrations use), state `on` — AND `cover.*` in state `open`/`opening`/`closing` — a cover mid-close is still
  open — with the COVER device classes, which are named differently:
  `garage` (not the binary-sensor's `garage_door`), plus `door`, `window`,
  `gate`. A motorized window or garage door is a cover, not a binary_sensor, so checking one family answers "is anything open?" wrong
- who is home: `person.*` with state `home` (this skill owns person STATE reads;
  `ha-nova:admin` owns creating and editing them)
- what is ON — the question "is everything off?" asks: `light`/`switch`/`fan`
  with state `on`, `media_player` in any state other than
  `off`/`unavailable`/`unknown` (a paused or idle player is not off), and the
  comfort domains that have their own off state: `climate`, `water_heater` and
  `humidifier` in any state but `off`/`unavailable`/`unknown`, `vacuum` in
  `cleaning`/`returning`, `valve` not in `closed`, and `cover` in
  `opening`/`closing` — a thermostat still heating, an open valve, or a robot
  mid-clean is
  the clearest possible no to "is everything off?", while an unreachable one is
  not a yes either: it joins the could-not-read count
- what is RUNNING — a different question, and a narrower answer: the same
  lights, switches and fans, but `media_player` only in `playing`/`buffering`,
  AND the domains whose "running" is not `on`: `vacuum` in `cleaning`/
  `returning`, `climate` whose `hvac_action` is PRESENT and is anything but `off`/`idle` (heating, cooling, drying, fan, preheating, defrosting — listing only the first two misses a dehumidifier mid-cycle; an absent `hvac_action` means the device does not report one, which is not the same as running), `valve`
  in `open`/`opening`/`closing` (the actuator is running and flow may continue
  until it seats), `cover` in `opening`/`closing` (a motor is turning; a
  cover simply left open is not running), `humidifier` in `on`, `water_heater` whose `hvac_action` is PRESENT and not `off`/`idle` — an
  absent one means the device does not report activity, which is not the same
  as running, exactly as for `climate` — never count `unknown` or `unavailable` as running. Reporting "nothing is running" while
  the heat pump runs is the same wrong answer as missing an open window
- unlocked doors: `lock.*` in a state that is neither `locked` NOR
  `unavailable`/`unknown` — an unreadable lock is not an unlocked one, it
  joins the could-not-read count like every other unreadable entity here —
  AND `binary_sensor` with
  `device_class: lock` in state `on` — that class reports `on` for UNLOCKED,
  which is the reverse of every other binary sensor here — `unlocked` obviously, but
  also `unlocking`, `opening` and `jammed`. The question is whether the door
  is secured, and a lock that is jammed or mid-travel is not; say which state
  each one is in rather than flattening them all to "unlocked"

An entity that is `unknown` or `unavailable` is neither open nor closed:
count it separately and say so. "Everything is closed" with three sensors
offline is the reassurance a user should not get — report "none open, 3
could not be read" instead.

Answer count-first ("2 windows open: kitchen, bathroom"), then the names, in the List
Frame. This is a summary, not the banned domain dump: `output-rules.md` asks
for counts, groups and a few examples exactly here. A follow-up "and which are
closed?" is a fresh read, not a cached list.

## Matching Rules

- area-first bulk discovery by room/area uses `search/related` on the resolved area before keyword heuristics
- exact `entity_id` match wins
- keyword match on entity_id + name second; rank whole-word and name matches above bare substring hits

If ambiguity remains: present top candidates (max 10) and state the match basis for each (name, entity_id, alias, or area) — the user must see WHY a candidate matched; then ask one selection question.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Render the Step 4 shortlist as the List Frame (output-rules.md); never dump raw `get_states` output.

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- Read-only — this skill never modifies Home Assistant state or config.
- No `POST`, `PUT`, `PATCH`, or `DELETE` relay writes.
- All communication with Home Assistant goes through `ha-nova relay` exclusively.

## Guardrails

- never guess entity IDs
- cap displayed shortlist rows at 20 only after exact matched-count computation for bulk inventory
- no writes
- no proactive doctor before real failure
