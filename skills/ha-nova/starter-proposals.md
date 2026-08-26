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
   count it and report the duplicate scan as PARTIAL, never as complete. Scene candidates use the `scene.*` rows from the same pull for the id
   list only — members come from config reads (rule 2b).
2b. Duplicate gate, config-evidence only: before a candidate enters the
   Suggestion Block, read the existing automations' configs — the config key
   is each automation's `id` ATTRIBUTE from its states row (registry
   `unique_id` for disabled rows), never the entity id. Bound the pass: cap
   the config reads (default 50, newest-updated first) and report anything
   beyond the cap as PARTIAL — the cap line names how many configs went
   unread. Scan each read WHOLE config document: triggers, conditions,
   actions, selector targets, and `use_blueprint.input` all carry entity ids
   literally; a blueprint config has no expanded sections — it drops
   the candidate only when its input NAMES identify the roles (e.g. a
   motion-entity input holding the sensor and a light-target input holding
   the lamp); bare co-occurrence in uninterpretable inputs falls into the
   closed PARTIAL rule, never a drop; match the
   candidate's role entities AND every selector value that resolves to them
   — `device_id`, `area_id`, `label_id`, `floor_id`, from the candidates'
   own registry memberships (a selector-targeted automation never names the
   entity). A candidate drops only when ONE config references its COMPLETE
   role pairing ON ONE EXECUTABLE PATH: the SOURCE role in triggers OR
   conditions (a time-pattern watchdog keeps its sensor in `conditions`)
   plus the ACTION role in the actions that branch actually runs — in
   configs with trigger-id/choose branches, cross-branch co-occurrence does
   not drop the candidate (name it in the evidence line instead). A single
   shared entity in an unrelated automation is no duplicate. Service-shaped roles (a `notify.*` target) match by
   SERVICE NAME in the config's actions, not by entity id — and a modern
   notify ENTITY also matches as the `target`/`entity_id` of a
   `notify.send_message` call. Two more equivalence expansions: a group target
   matches when its membership PROVABLY contains the candidate — the
   `entity_id` states attribute where present, else the group helper's
   config read; membership the pass cannot prove falls into the closed
   PARTIAL rule; a presence source role matches zone-count
   guards (`zone.home` state) and direct `person.*`/`device_tracker.*`
   conditions alike — but only with the POLARITY the candidate needs: an
   at-home guard (zone count > 0, or state `home`) never evidences an away
   alert. CLOSED RULE
   for everything else: any config whose references the pass cannot FULLY
   resolve — dynamic Jinja-computed targets, unexpanded groups, delegation
   through items it could not read — makes the duplicate scan PARTIAL, and a
   PARTIAL scan downgrades every affected evidence line from "not automated
   yet" to "no duplicate found (scan partial)". Never enumerate past this
   rule: unresolvable evidence fails closed into honesty, not into a claim. When the pass is capped, say the
   duplicate gate was partial — never claim "not automated yet" from states
   rows or aliases. Disabled storage scenes join the scene inventory via the
   same registry-disabled read as automations. Scene MEMBER checks — enabled
   and disabled alike — read the scene's CONFIG by id; neither the registry
   row nor a states attribute is member evidence.
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
| Movie scene | media_player AND lights in the same area, and no existing `scene.*` already grouping them (member check via scene CONFIG reads, rule 2b) | `ha-nova:scene` |
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
