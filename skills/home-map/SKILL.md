---
name: home-map
description: Use when the user wants to understand, document, or audit their whole Home Assistant home with exact numbers — "what do I have", "show me my home", "where is this entity used", "review my whole setup", "write down why it is built this way", or the first look after pairing.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Home Map


## Scope

Read-only understanding of the whole home through `ha-nova map`, which
computes every count, so you never count by hand:
- inventory with exact counts
- where-used for one entity (automations, scripts, scenes, groups, helpers, dashboards)
- one ranked "review my whole setup" report
- a plain-language document of how the home is built and why
- the first look right after pairing

Not in scope (hand off):
- fixing anything: `ha-nova:organize` (areas, names), `ha-nova:write`
  (automations, scripts, their descriptions), `ha-nova:maintenance`
  (orphans, statistics), `ha-nova:updates` / `ha-nova:hacs`
- repairs, system health, battery classes in depth: `ha-nova:health`
- why one item failed: `ha-nova:diagnose`
- what is on or open right now: `ha-nova:entity-discovery`
- automation quality findings: `ha-nova:review`

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
`ha-nova map` needs no `ha-nova relay health` probe: an unreachable Relay
answers exit 1 with `RELAY_ERROR`; then `ha-nova setup`.

## Relay Contract

- `ha-nova map` — the summary; capped lists carry `total` and `shown`,
  and `more` names the command for each full list
- `ha-nova map --section areas|integrations|automations|unavailable|batteries [--limit N]`
  — one full list (default 50, max 200 rows; `total` stays exact)
- `ha-nova map --entity <entity_id> [--limit N]` — where-used; resolve a
  name first with `ha-nova find "<phrase>"`
- `ha-nova map --first-look` — `home`, up to two `observations`, `seeds`
- `ha-nova get <automation.x|script.x> --config` — one item's config and
  `description`

A failed read fails the whole map (exit 1): say so, never estimate.

## Flow

Every number you state comes from a map result of this session; after any
write, read it again. Never add, subtract, or count rows
yourself — the fields are the totals.

Definitions to keep when you explain a number:
- `entities` are the enabled ones (YAML entities without a registry row
  included, also counted as `unregistered`); disabled ones are
  `entities_disabled`. Home Assistant's own lists may differ when their
  filters include disabled rows — say so, never "correct" either number.
- an area holds an entity through the entity's own area, else its device's
  area (a child device inherits its parent's area) — the same rule as
  `ha-nova find --area`, which lists exactly the counted rows.
- automation `unavailable`: Home Assistant could not load it, so it never
  runs; `restored`: its config is gone, only the registry row remains (an
  orphan for `ha-nova:maintenance`). `never_triggered`: enabled, no run yet.
- `age` on an unavailable entity is the age of its current state — "for at
  least 9 days", never "since 9 days ago it broke". `restored` rows are
  orphans too.
- batteries are low battery levels: `low` counts device batteries;
  `levels_low` are charge levels (a car or home storage — low is normal
  there, not a fault); `phones_low` are phones — never suggest a
  replacement for either.

### What do I have / show me my home

Run `ha-nova map`; then one line for the house (floors,
rooms, devices, automations), then what needs attention (open problems), a
compact table of the largest rooms (devices; no other table; none when no
room has devices), and one line on automations (how many are off or can't
run). Leave out the `entities`, `entities_disabled`, `domains`, and
`integrations` numbers (no row, no aside) unless asked. Close with one offer ("Want me to look at a room or the
problems?").

### Where is X used

Resolve the entity, then `ha-nova map --entity <id>`. List every hit by
name (dashboards: dashboard › view › card); mark `via: device` rows
as "through its device". Always name the `not_checked` surfaces in one line: an
empty result means "nothing found where checked", never "unused" or "safe to delete".
Deletes and renames: the owning skill.

### Review my whole setup

1. Run `ha-nova map`.
2. Rank what it proves, most urgent first: integration `problems` →
   unavailable automations → entities unavailable for at least 7 days → low
   device batteries → enabled automations that never ran → devices without
   an area.
3. One row per finding: what, how many, an example only where the map
   gives one, and who fixes it.
4. Close with the deeper checks the map does not run, as next steps:
   repairs and system health, automation quality, orphan and statistics
   cleanup, pending updates.

### Document why it is built this way

1. Scope first: one floor, one area, or at most 10 automations per chunk
   (Progressive Detail in the output rules). Run `ha-nova map` (and
   `--section areas` for per-area domains).
2. For each automation or script in scope, `ha-nova get <id> --config` and
   read its `description` and behavior.
3. Write the document in plain words: per area, what is there and what runs
   on its own. Intent comes only from a `description` or from the user; when
   neither says why, write "purpose not recorded" and ask — never invent a
   reason.
4. Persistent notes: the "why" belongs in the automation's or script's own
   `description` field, Home Assistant's native and visible store, which
   later sessions read back with `get --config`. Offer a proposed
   description per item; saving goes through `ha-nova:write` with its usual
   preview and confirmation. The house-wide document stays in this chat for
   the user to keep; nothing else is stored.

### First look (after pairing)

Run `ha-nova map --first-look`. Render:
1. one line from `home` (rooms, devices, automations);
2. each observation in plain words — `battery_low` (name, area, `value`,
   plus "N more low batteries" from `count`), `automation_unavailable` ("can never
   run"), `integration_problem` (integration domain and state in plain
   words), `automation_never_triggered`, `unavailable_long`;
3. up to three example prompts in the user's language, one per seed
   present, built only from `seeds` names, each a different everyday job
   (control, question, why or change).
No observations: skip that line, never invent one. Any non-zero exit: skip
the first look without comment.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked, no ids, "entity", modes, Relay/HA URLs, snapshot files, raw JSON. Lead with the answer in one or two
lines, then at most one compact table. A capped list follows Progressive Detail; you run the matching
`--section` when asked. No field names such as `never_triggered`.

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.
- Never claim an entity is unused beyond the surfaces that were searched.
- Integration titles, attributes, locations, and presence never appear in
  map output; do not fetch them to decorate an answer.
