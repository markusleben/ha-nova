---
name: entity-discovery
description: Use when searching or resolving Home Assistant entities by name, room, or domain through HA NOVA Relay, and live state snapshots across a domain — what is open, on, running, locked, who is home.
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

Search both entity_id and name. Use short keyword stems to handle spelling variants. Cap display at 20 rows through the canonical envelope below — never a bare slice.

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

Write `<filter-file>` by copying `skills/entity-discovery/discovery-filter.jq`
(this skill's directory) and replacing `KEYWORD` with the pattern. It returns
`{total, shown, omitted, truncated, matches}` — the 20-row cap applies to
`matches` only; the counts are exact.

This generic `test("KEYWORD";"i")` filter is for free-text search, not explicit `prefix` matching.
For an explicit prefix selector, match the suffix and display name with `startswith(...)`, not loose substring search.

**If the compact search does not resolve a suitable target** — no results, or only matches the user rejects or that plainly are not what they meant — escalate ONCE to the full registry (`config/entity_registry/list`) and match against `aliases[]` too — `list_for_display` does not carry them, and an alias is exactly where a household keeps the name it actually says (a household's own word for `light.floor_lamp_living`). Only then try synonyms, alternative terms, or shorter keyword stems. Use OR for multiple variants: `test("kw1|kw2|kw3";"i")`. When a resolved entity had no matching alias and the user's word was clearly their habitual name, offer once to store it as an alias via `ha-nova:organize`.
**Diacritics:** `test(...;"i")` folds case, not accents — a name with `é`/`ü`/`ö` does not match its plain-ASCII spelling. Whenever the keyword or likely entity names carry accents or umlauts (common in non-English homes), put the transliterated variants into the OR-pattern: `test("café|cafe";"i")`, and for umlauts include both the `ue`/`oe`/`ae` and bare-vowel forms.
**If too many:** narrow with AND: `test("kw1";"i") and test("kw2";"i")`.
**Fail closed on truncation:** while `truncated` is true the result proves neither absence nor uniqueness — the target may sit past the cap. Narrow automatically (area, domain, exact identifier, aliases, extra AND terms) until `truncated` is false before concluding either; report `total`/`shown`/`omitted` with the rows.
**Never** dump entire domains without a user-intent keyword, and never show an unfiltered full-registry dump.

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

Three answers, not two: **yes / no / could not tell**. An entity that is
`unknown` or `unavailable`, and one that is readable but does not report the
thing being asked, both go in the third bucket and get named. "Everything is
closed" with three sensors offline is the reassurance nobody should get —
"none open, 3 could not be read" is the honest form.

Skip group helpers when counting: a Light Group is a real `light.*` entity
beside its members, so it double-counts. They carry an `entity_id` LIST in
their attributes — skip any entity whose `attributes.entity_id` is an array.
(`switch_as_x` mirrors double-count too and carry no such marker; say so if
the count looks high.)

- **open windows/doors** — `binary_sensor` with `device_class`
  `window`/`door`/`garage_door`/`opening` (the generic contact class) in state
  `on`, AND `cover.*` in `open`/`opening`/`closing` (mid-close is still open)
  with the COVER classes, which are named differently: `garage` — not the
  binary sensor's `garage_door` — plus `door`, `window`, `gate`. A motorized
  window is a cover, not a binary_sensor, so checking one family answers the
  question wrong. A cover in those states whose `device_class` is absent or
  outside that list is not evidence of closed: report it as "N open, class
  unknown". Do NOT do the same for unclassed binary sensors — one is
  indistinguishable from a motion sensor and would manufacture false alarms.
- **who is home** — `person.*` in state `home`. Anything else that is not
  `unknown`/`unavailable` is away, including a named zone like `work`: those
  are zone names, not errors. `unknown` means no tracker had usable data —
  third bucket, because "nobody is home" from a dropped phone is how an alarm
  gets armed on someone. (This skill owns person STATE reads; `ha-nova:admin`
  owns creating and editing them.)
- **unlocked doors** — `lock.*` in any state that is not `locked` and not
  `unavailable`/`unknown`, AND `binary_sensor` with `device_class: lock` in
  state `on`, which reports `on` for UNLOCKED — the reverse of every other
  class here. Name the state each one is in rather than flattening them to
  "unlocked": jammed and mid-travel are not secured, and they are what a user
  needs to hear.
- **is everything OFF** — `light`/`switch`/`fan`/`siren`/`remote` in `on`
  (a Harmony-style `remote` in `on` means an AV activity is live);
  `media_player` in anything but `off`/`standby`/`unavailable`/`unknown`
  (paused and idle are not off; `standby` IS — Home Assistant core deprecates
  it as meaning off-or-idle, and users call that TV off);
  `binary_sensor` with `device_class` `running` or `moving` in `on`;
  `script` in `on` and `automation` with `attributes.current > 0` — both are
  actually executing, and the RUNNING list counts them, so leaving them out
  here would let "is everything off?" say yes to something "what is running?"
  names in the same breath;
  `climate`/`water_heater`/`humidifier` in any state but
  `off`/`unavailable`/`unknown` — an unreachable one is not off, it goes in
  the third bucket;
  `vacuum` and `lawn_mower` in `cleaning`/`mowing`/`returning`/`paused`/`error`
  — `error` means stranded mid-floor, not put away, and it is readable so the
  unavailable rule never catches it; `valve` in `open`/`opening`/`closing`;
  `cover` only in `opening`/`closing` — a cover left open is answered by the
  open-windows question above, not by this one, because it consumes nothing.
- **what is RUNNING** — narrower, and everything in it is also in OFF above;
  keep that invariant when either list changes. Same lights, switches, fans,
  sirens and `binary_sensor` `running`, plus `device_class: moving`; but
  `media_player` only in `playing`/`buffering`; `vacuum`/`lawn_mower` only in
  `cleaning`/`mowing`/`returning` (report `error` as stuck, not as running);
  `valve` in `open`/`opening`/`closing`; `cover` in `opening`/`closing` only —
  a motor turning is running, a cover left open is not.
  - `climate` runs when `hvac_action` is PRESENT and not `off`/`idle`
    (heating, cooling, drying, fan, preheating, defrosting).
  - `humidifier` has the same shape under a different key: `attributes.action`
    PRESENT and not `off`/`idle`.
  - `water_heater` has NO action attribute at all — its state is an operation
    mode (`eco`, `performance`, `heat_pump`, ...), which reports enablement and
    never activity. It can never be proven running: enabled goes in the third
    bucket, never in "nothing is running".
  - An ABSENT action attribute is the third bucket too, not a no. Most simple
    thermostats never publish one, and "nothing is running" while the heat pump
    runs is the failure this whole section exists to prevent.
  - `automation` in `on` means ENABLED, not running — exclude it, or the answer
    is "47 things are running". Its real running signal is
    `attributes.current > 0`. `script` in `on` does mean running.

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
