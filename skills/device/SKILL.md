---
name: device
description: Use when replacing a Home Assistant device with a new one (a dead Zigbee sensor, a new phone, a lamp moved to another integration) and re-pointing the automations, scripts, scenes, dashboards, helpers, groups, energy sources, notify targets, and person trackers that use it; also for onboarding a new device where its siblings are used, or retiring one with an impact check.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Device

## Scope

Device lifecycle: **swap** (re-point everything that names the old device to the new one), **onboard** (a new device joins where its siblings are used), **retire** (impact check before a device leaves).

This skill plans the whole task and writes two families itself: automation/script re-points (`ha-nova plan write` + `apply`) and the entity_id takeover (entity registry `new_entity_id`). Every other family goes to its owning skill (Hit Routing).

Not in scope: removing or re-pairing a device (Home Assistant UI, the integration's device page; leftover entity rows: `ha-nova:maintenance`); one entity rename without a swap (`ha-nova:organize`); groups and bindings inside ZHA or Zigbee2MQTT (the integration's UI).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`. An unreachable Relay answers exit 1 `RELAY_ERROR` (then `ha-nova setup`).

## Relay Contract

- Scan (read-only): `ha-nova get <entity_id> --references` covers the entity's whole device. `result.references`: `device`, `entities[]` (mapping hints: `platform`, `translation_key`, `original_name`, `device_class`, `unit`, `state`, `registry_id`), `notify_service`, `hits[]` (`family`, `id`, `config_id`, `managed`, `needle`, `paths`, `template`), `scanned`, `not_scanned[]`, `not_covered[]`; `result.related` is search/related. An entity that no longer exists is scanned by its id (`resolved[0].by` = `absent`).
- Resolve: `ha-nova find "<phrase>"`, `ha-nova get <entity_id> --config`; registry reads (`config/entity_registry/get`, `config/device_registry/list`) per `../organize/SKILL.md`.
- Re-point: `ha-nova plan write --domain automation|script --id <config_id> --file <draft.json>`, then `ha-nova apply --plan <id> [--confirm <code>]` (`../write/SKILL.md` Phase 2-3 rules).
- Takeover: `ha-nova relay ws --data-file <payload-file>` with `{"type":"config/entity_registry/update","entity_id":"<new id>","new_entity_id":"<old id>"}`.

## Hit Routing

| Hit | Route |
|---|---|
| `automation` / `script` without `managed` | this skill: re-point group |
| any hit with `managed: yaml` | `ha-nova:yaml-config` |
| `managed: blueprint` (the blueprint body names it, not the instance) | the blueprint: `ha-nova:fallback` |
| `dashboard` | `ha-nova:dashboard` |
| `energy` | `ha-nova:energy` |
| `entity` on `scene.*` / `person.*` / `group.*` | `ha-nova:scene` / `ha-nova:admin` / `ha-nova:yaml-config` |
| other `entity` (group, min_max, utility meter, derivative, integral, threshold helper) | `ha-nova:helper`; no helper → `ha-nova:fallback` |
| old entity's Assist exposure (a takeover keeps the new entity's own) | compare; differences: `ha-nova:assist` |
| a `related` item missing from `hits` (a blueprint naming the entity) | read its config; treat it as a hit |

## Access Check

A swap can hand a door to a new device without anyone editing a lock automation: a takeover re-points every unlock, disarm, or open that names the old id, a new phone's tracker can feed the person or group an unlock waits for, and an area or label can widen what an access automation targets. Decided in the plan card, before the old device is removed, a step takes the one-time code (minted for a rename; the owning flow's code for a hand-off) when:
- the entity is a lock, an alarm panel, or a cover not of class awning, blind, curtain, damper, shade, shutter, or window — or sits on a device (old or new) with one;
- any automation/script consumer plans `typed` (a consumer the swap re-points: its re-point plan's tier; any other: plan its current config unchanged (`get --config` → `plan write --id`), read only the tier, never apply it). Consumers include one level up: the `related` automations and scripts of every `entity` hit (a person, a group);
- a consumer's tier cannot be read (`managed: yaml` or `blueprint`, a `not_scanned` automation or script): it counts as typed.

It covers takeover renames, person and group hand-offs, and carrying an area or label onto such an entity.

## Flow

Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

### Swap

1. **Scan both devices first.** `get <old entity> --references` and `get <new entity> --references` BEFORE the old device is removed: afterwards its device and registry ids are gone and device-level references cannot be found. A phone whose device is already gone: scan its old ids the same way, including its notify service (`get notify.mobile_app_<old name> --references`; hits confirm the name).
2. **Map** old → new from `entities[]`: same domain, then `device_class`, `translation_key`, `unit`, `original_name` without the device name. One-to-one; ambiguity is one question. An old entity with hits but no counterpart is named with the choice: drop the reference, or point it at an entity the user names. Remotes and buttons: `../ha-nova/input-capability-preflight.md` first.
3. **Choose the path**:
   - **Takeover** — smaller when the old device is gone for good and the mapped entities share a domain: remove the old device, then rename each new entity to its old entity_id. Every reference by entity_id keeps working (dashboards, energy, groups, persons, scenes, templates) and history and long-term statistics continue, because the old id's recorder rows stay. Never rename the old entity away to free the id: its history and statistics move with it, and the energy dashboard loses that source's past.
   - **Rewrite** each consumer — when the old device stays in use, a domain differs, the old entity has no registry row, or the user wants the new ids. A rewritten energy source starts its history fresh: say so.
   - Both paths rewrite device-level hits (`needle` is the device id or a `registry_id`: device triggers, conditions, actions, `device_id` targets) and the notify service. Across integrations a device trigger rarely maps 1:1: use a state trigger on the mapped entity and name the change.
4. **Plan card** (one Report): the mapping, the path and why in one sentence, hits by route with counts, the steps in order (the old device's removal is a user step in the HA UI), the Access Check result for every rename and hand-off, and coverage — `scanned` counts, every `not_scanned` row, the `not_covered` line. Never claim "everything" while either applies.
5. **Execute** in this order (Re-point Group below):
   a. Device-level re-points — and on the rewrite path every re-point — before anything is removed: the new ids are known now, and a removed device's ids can never be scanned again.
   b. Takeover only: the user removes the old device in Home Assistant. Verify each mapped old id: `ha-nova get <old id>` exits 5 `NOT_FOUND`; a leftover row goes to `ha-nova:maintenance`.
   c. Takeover renames, one registry update per pair on the NEW entity, per `../organize/SKILL.md` → Entity / Device Update Fields (metadata snapshot first); tier per Access Check, ordinary renames in one grouped change set. Carry the old entity's `area_id` and `labels` (area and label targets reach a device only through them) and, when the user wants, its `name`, `icon`, `aliases`. Both paths: the old device's area and labels go to the new device through `ha-nova:organize`.
   d. Takeover only, after the renames are verified: re-point every holder that names a NEW entity's own id (the new scan's hits) to its taken-over id; a holder that named both ids now names one entity twice — the user picks which reference stays.
   e. Hand-offs, one owning skill at a time (Hit Routing), each with exact holders, paths, and the mapping; their previews, snapshots, and tiers stay, raised by the Access Check where it applies. The Work Ledger carries the open families.
6. **Verify**: re-scan the new device. Every holder of both scans shows the new needles or was dropped by the user's choice; on the rewrite path the old device's re-scan has no hits. Report applied, handed off, still open, and the coverage limits. A pure re-point needs no further review; a converted device trigger gets the write skill's Phase 4 review.

**Re-point Group.** Draft = the current config with each mapped needle replaced, templates included, a hit's `keep` devices kept beside it, nothing else changed (key order kept); the takeover path replaces device ids, registry ids, and the notify service only, because the old entity ids will name the new entities. One grouped change set (`../ha-nova/grouped-change-set.md`) of at most 5 updates. The undo store keeps the last 5 updated targets across every write of the swap — re-points, typed plans, helper hand-offs: when the swap makes more, the earliest lose `revert`; say so in the plan card and offer `ha-nova:backup` before the first write. The CLI tiers each plan by what the config performs, whichever part changed; a typed plan leaves the group with its own code. Diff rows verbatim.

Partial failure: each group stops at the first failure and reports its ledger; nothing is atomic. Applied updates keep their `revert` until the undo store evicts them (the `undo` receipts name evictions). A failed rename after the old device's removal leaves the old id unclaimed — the state the dead device left: if `get <old id>` still exits 5, retry that rename; if the id is taken again, stop and re-scan. Never rename anything else to cover. Undoing a takeover renames the entity away and takes the old id's history with it: say so before such a revert.

### Onboard

1. Name, area, labels: `ha-nova:organize`; for a lock, alarm panel, or access cover the Access Check comes first.
2. Siblings: same area and domain (`ha-nova find --area <area_id> --domain <d>`); `get <sibling> --references` shows where they are used. Offer at most two holders to add the new entity to, through the routes above — always as an added target beside the sibling, never a replacement; a change to trigger or condition logic goes through `ha-nova:write`.
3. Identify a light on request, after the user says they are watching: `ha-nova call light.turn_on <entity_id> --set flash=short`.

### Retire

`get <entity> --references` on the device, then `../ha-nova/safe-refactoring.md` → Pre-Delete Impact Check with these hits as the consumer list and the coverage limits named. Removing the device stays in the HA UI.

## Output Format

Apply `../ha-nova/output-rules.md`. The plan is a Report (`../ha-nova/output-cards.md`); the mapping reads `old → new` per line, holders by alias or title, paths only on request. Writes keep their owning flows' Preview and Result Cards; a group closes per `../ha-nova/grouped-change-set.md`.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's one-time code from HA NOVA, or their window click or phone tap; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any change to target, payload, endpoint, or scope voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- A scan after the old device's removal finds entity_id references only: say so, and never call device-level coverage complete.

## Guardrails

- A group holds at most 10 operations and at most 5 automation/script updates.
- Never down-tier: CLI tiers, the Access Check, and every owning skill's tier stand.
- A re-point changes only the mapped needles; anything else the scan exposes is reported, not fixed.
- One owning skill at a time.

## References

- `../write/SKILL.md` (Phase 2-3), `../ha-nova/write-safety.md`, `../ha-nova/grouped-change-set.md`, `../ha-nova/smallest-solution.md`
- On demand: `../ha-nova/safe-refactoring.md` (retire), `../ha-nova/input-capability-preflight.md` (remotes, buttons), `../organize/SKILL.md` (takeover fields)
