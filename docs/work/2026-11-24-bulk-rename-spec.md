# Rename many entities to a naming scheme in one pass (#979)

Status: active (release/0.36, HANO-84). KISS-shaped by gpt-6.2-sol (high). Skills plus one small CLI change (every rename item of a raw plan is bound to its live registry entry); no Relay change.

## Problem
A rename per entity asks once per entity and, for id changes, edits the same automation once per entity. `safe-refactoring.md` claimed that re-running `search/related` proves every reference was updated, which `organize` already disclaimed (templates and dashboards are not indexed).

## What the user asks
"Give all kitchen lights the same naming scheme", "name my sensors like the living room ones", "rename these 12 entities and their ids".

## What the agent does (`skills/ha-nova/safe-refactoring.md` → Rename Scheme; `organize` points there)
1. Targets: organize's Bulk Target Resolution (saved id list). At most 19 renames per pass.
2. Convention: a scheme the user states wins. Otherwise compare independent devices in the same domain, area, and floor (the floor from the area registry), never several entities of one device; infer only from at least two agreeing examples; disagreeing examples are shown and asked about before any draft; one example or none is a proposal, never "the household's convention".
3. Map: device name (only when asked), entity name, entity id before → after; names as Home Assistant will show them (`organize/display-fields.md` → Names: the device name in front where it adds it; a device rename lists its entities' changed full names). A new id that exists, two rows with one new id, and chains or swaps are left out with the reason.
4. Consumers (id changes only): `ha-nova get <old id> --references` once per changed entity id (a per-device scan leaves out helpers another entry owns), saved as the holder list, each holder named by its `friendly_name`. Hits on the registry id keep working and are listed as unchanged; YAML holders go to `yaml-config`. An unreadable automation, script, or dashboard scan blocks that row's id change.
5. One preview: the bundle (device name, entity name, entity id, consumers by name and what happens to them, coverage), closed by the `say` of ONE `ha-nova plan raw`: a `metadata` snapshot save with every target's registry row exactly as read (its registry `id` included), then one registry update per row (`name` and `new_entity_id` together). One confirmation at the tier the CLI decides.
6. Apply: snapshot first (it fails: nothing renamed, `--no-snapshot` never offered), renames in order, stop at the first that does not go through; a ledger; each done row verified from its registry row.
7. Consumers: offered right after, in their own flows (automations and scripts as one `plan batch` of re-points, dashboards through `ha-nova:dashboard`, other families per the device skill's Hit Routing); then `get <old id> --references` per changed id shows no saved holder still names it.
8. Undo: from the pass's own snapshot, only done rows whose entry still has the snapshot's registry `id` and still holds what the pass set, with exactly the touched fields put back (a `null` name stays `null`); one preview, one confirmation; then only this pass's re-points back through `plan revert --target` or a new batch.

## CLI change (`cli/plan_raw_names_undo.go`)
Every `config/entity_registry/update` or `config/device_registry/update` item of a raw plan that sets `name`, `aliases`, `new_entity_id`, or `name_by_user` puts its entry as it is now (registry `id` plus the fields it sets; `absent` or `unreadable` otherwise) into the plan's basis. Before, only a plan of one name-only item was bound. Apply re-plans, so another entry under the id, or a changed name, is drift and nothing is sent; and right before each item is sent its entry is read again, so an entry an earlier step (or anything else) moved onto a later step's id stops that step. When the plan's leading metadata copy is a list of registry rows, each renamed entry must be in it with the registry `id` and the fields the rename sets as the plan reads them, or the plan is refused: the way back always describes the entry that is renamed. Two items on one entry bind both fields.

`get --references` hits carry the holder's `name` (friendly name, a disabled one's alias, or the dashboard title), and a null registry, state, or panel list, or a holder config without its triggers and actions (a script: its sequence), is a gap instead of a scanned holder.

## Existing commands used
`ha-nova find`, `ha-nova get --references`, `ha-nova plan raw` + `apply` (1 to 20 items, leading snapshot saves, each item checked by the raw guard right before it is sent, stop at the first failure, `result.items` ledger, raw tier with access, listener, and takeover checks), `ha-nova plan write` + `plan batch`, `ha-nova plan revert`, `ha-nova get --dead-references` (#978).

## Shaping decisions
- Taken from Sol: registry renames only in the confirmed plan; consumers listed by name and re-pointed afterwards in their own confirmed flow (the requirement allows "updated in the same batch or listed clearly"). Rewriting consumers in the same batch would need `plan batch` to rate drafts that name ids which don't exist yet, excuse entity drift between members, and bind one grant across registry and config writes — the riskiest code in the tier engine, for one saved confirmation.
- Not taken: Sol's narrow `plan raw --rename-set` CLI mode (sealed identities, apply-time snapshots, about 150-220 Go lines). Its first review then showed the raw plan bound only a single rename to its registry entry; binding every rename item (above, about 50 Go lines) closes that inside the existing raw plan. Home Assistant itself refuses a rename to an id that is taken or from one that is gone. The undo stays a skill step on the snapshot rows.

## Known limits
- Between the rename and the re-point confirmation, automations and dashboards that name an old id don't work; the preview names them.
- A pass is at most 19 renames.
- The undo is a skill step: it checks each row's registry `id` before it puts anything back.

## Not in scope
Renaming automations, scripts, areas, or floors; YAML entities without a registry row; the mobile_app notify service.

## Proof
Skill contracts (convention evidence, one plan and one ask, consumers named, undo, budgets). End-to-end agent run on the test bed: rename several entities with id changes that automations, a script, and a dashboard name; one preview, one confirmation; no config written by the pass; then the offered re-points and the sweep.
