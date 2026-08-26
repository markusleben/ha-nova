# Starter Proposals — "Analyze my home and suggest automations"

Owned by the context skill. Evidence-gated: every proposal names the concrete
entities that justify it; a pattern with no matching hardware is never
proposed. Read-only until the user accepts an item.

## Inventory (bounded, read-only)

1. One `/api/states` pull with `--out <result-file>`; extract per-domain
   entity lists via `relay jq` (never print the dump).
2. One automation inventory from the same pull (`automation.*` rows) for the
   id list — a states row proves nothing about WHAT an automation does.
2b. Duplicate gate, config-evidence only: before a candidate enters the
   Suggestion Block, read the existing automations' configs (one bounded
   pass over the step-2 ids) and drop any candidate whose role entities
   already appear paired in a trigger/action; when the pass is capped, say
   the duplicate gate was partial — never claim "not automated yet" from
   states rows or aliases.
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
| Movie scene | media_player AND lights in the same area, no such scene | `ha-nova:scene` |
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
  what hardware would unlock which pattern.
- Each accepted number hands to the owning skill INDIVIDUALLY — one normal
  preview/confirm flow per item, never a batch write. Skip/decline is always
  valid; nothing runs from the menu itself.
