# Starter Proposals — "Analyze my home and suggest automations"

Owned by the context skill. Evidence-gated: every proposal names the concrete
entities that justify it; a pattern with no matching hardware is never
proposed. Read-only until the user accepts an item.

## Inventory (bounded, read-only)

1. One `/api/states` pull with `--out <result-file>`; extract per-domain
   entity lists via `relay jq` (never print the dump).
2. One automation inventory: the `automation.*` rows from the same pull PLUS
   registry-disabled automations via a FULL entity-registry read
   (`config/entity_registry/list` — the compact list carries no `unique_id`,
   which IS the disabled rows' config key) — disabled automations have no
   states row, and a states row proves nothing about WHAT an automation
   does. A YAML automation without an explicit `id` has no readable config:
   count it and report the duplicate scan as PARTIAL, never as complete. Scene candidates use the `scene.*` rows
   and their `entity_id` member attribute from the same pull.
2b. Duplicate gate, config-evidence only: before a candidate enters the
   Suggestion Block, read the existing automations' configs — the config key
   is each automation's `id` ATTRIBUTE from its states row (registry
   `unique_id` for disabled rows), never the entity id — and scan each WHOLE
   config document: triggers, conditions, actions, selector targets, and
   `use_blueprint.input` all carry entity ids literally; match the
   candidate's role entities AND every selector value that resolves to them
   — `device_id`, `area_id`, `label_id`, `floor_id`, from the candidates'
   own registry memberships (a selector-targeted automation never names the
   entity). A candidate drops only when ONE config references its COMPLETE
   role pairing: the SOURCE role anywhere in triggers OR conditions (a
   time-pattern watchdog keeps its sensor in `conditions`) plus the ACTION
   role in its actions — a single shared entity in an unrelated automation
   is no duplicate. Service-shaped roles (a `notify.*` target) match by
   SERVICE NAME in the config's actions, not by entity id — and a modern
   notify ENTITY also matches as the `target`/`entity_id` of a
   `notify.send_message` call. An action delegated through `script.*` or
   `scene.turn_on` hides the pairing: when a config pairs the source role
   with a script/scene call, resolve that item's members or sequence before
   clearing the candidate; delegation the pass cannot resolve makes the
   duplicate scan PARTIAL, reported as such. When the pass is capped, say the
   duplicate gate was partial — never claim "not automated yet" from states
   rows or aliases. Disabled storage scenes join the scene inventory via the
   same registry-disabled read as automations — and their MEMBERS come from
   reading each disabled scene's config by id (the registry row carries no
   members; only enabled scenes expose them in the states pull).
2c. Area pairing resolves area-first per the architecture reference:
   `search/related` on the area — this covers entities that inherit their
   area from their device; the compact registry's `ai` field alone misses
   inherited membership. `config/area_registry/list` supplies the names.
3. Optional, only when a candidate needs it: one bounded history window (via
   `ha-nova:history`'s bounded read) for ONE entity to check real usage
   (e.g. does the motion sensor actually fire?). Never a per-entity history
   sweep.

## Evidence-gated pattern table

| Propose | Only when the inventory shows | Hands to |
|---|---|---|
| Motion-activated light | motion/occupancy sensor AND light in the same area, no automation pairing them | `ha-nova:write` |
| Door-open-while-away alert | door/window sensor AND a person/device_tracker, no such alert yet | `ha-nova:write` |
| Low-battery notification | battery-level entities AND a notify target — notify ENTITIES or the notify domain's services from `/api/services` (mobile-app targets live only there) | `ha-nova:write` |
| Movie scene | media_player AND lights in the same area, and no existing `scene.*` already grouping them (member check via the scene rows' `entity_id` attribute) | `ha-nova:scene` |
| Presence-simulation while away | lights AND a person/device_tracker (pattern per `automation-patterns.md`) | `ha-nova:write` |
| Stale-sensor watchdog | a sensor whose updates matter (per the user's stated interest) | `ha-nova:write` |

The table is the catalog ceiling: never invent a proposal outside it from a
cold start. Area pairing comes from the entity/area registries, not from
name-string guessing.

## Output

- At most 5 items, rendered as ONE Suggestion Block (💡, numbered); each item:
  short title + the evidence line ("motion sensor + lamp in the living room,
  not paired yet") + what it would do.
- Fewer real candidates than 5 → show fewer; zero → say so honestly and name
  what hardware would unlock which pattern. MORE than 5 → apply Progressive
  Detail (output-rules.md): name how many were omitted and the exact
  follow-up that shows the rest.
- Each accepted number hands to the owning skill INDIVIDUALLY — one normal
  preview/confirm flow per item, never a batch write. Skip/decline is always
  valid; nothing runs from the menu itself.
