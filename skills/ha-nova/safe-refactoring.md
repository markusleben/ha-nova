# HA NOVA Safe Refactoring

Workflows for safely renaming, deleting, and cleaning up entities, automations, scripts, and helpers.

## Pre-Delete Impact Check

Before deleting any automation, script, or helper, check what depends on it.

### Step 1: Find Consumers

Use `search/related` to find all automations, scripts, and scenes that reference the entity:

Create `<payload-file>` with the `search/related` request, then run:

```text
ha-nova relay ws --data-file <payload-file>
```

Parse the response for `automation`, `script`, and `scene` entries in the related items.

### Step 2: Assess Impact

- **No consumers found:** Safe to delete.
- **Consumers found:** List them for the user with their aliases. Example:

  ```
  This entity is used by:
  - automation.morning_routine ("Morning Routine") — action: turns on this light
  - script.welcome_home ("Welcome Home") — action: sets brightness

  Deleting it will break these configs. Should I update them first,
  disable it instead (reversible), or leave everything as it is?
  ```

### Step 3: Disable vs Delete

Prefer disabling over deleting when the user is unsure:

```text
# Disable entity (reversible), or re-enable it later:
# one config/entity_registry/update item, then ha-nova apply per its next_step
ha-nova plan raw --file <requests.json>
```

Only proceed with permanent deletion after the delete's typed confirmation (see write skill → Phase 2).

## Entity Rename Workflow

Renaming an entity id breaks every automation, script, dashboard, and template that names the old id; a name change alone breaks none of them. Several renames at once: Rename Scheme below.

### Step 1: Find All References

```text
ha-nova get <entity_id> --references
```

It also reads templates, dashboards, and energy sources, which `search/related` does not index. It is not complete either: name its `not_scanned` rows and its `not_covered` line (template entities, Node-RED, YAML-only integrations, and more) — never call a rename safe because the scan is empty.

### Step 2: Show Impact

List every holder that names this entity by name. For each, note whether the reference is in:
- Triggers (entity_id in trigger config)
- Conditions (entity_id or template referencing entity)
- Actions (target entity_id or template)

Template references (`states('old.entity_id')`, `is_state(...)`) are NOT auto-updated by HA — they break silently.

### Step 3: Rename

```text
# one config/entity_registry/update item with new_entity_id, then ha-nova apply per its next_step
ha-nova plan raw --file <requests.json>
```

### Step 4: Update Consumers

For each consumer automation/script found in Step 1:
1. Read the config
2. Replace all occurrences of the old entity_id (in entity_id fields AND in templates)
3. Preview the updated config to the user
4. Apply via write skill flow (Phase 2+3)

### Step 5: Verify

Re-run `ha-nova get <old entity_id> --references`: no hit may remain outside what the user chose to leave, and its coverage limits are said again.

## Rename Scheme (many entities in one pass)

For "give all kitchen lights the same naming scheme", "rename these sensors and their ids", and any request that renames two or more entities or devices. Owner: `ha-nova:organize`. One pass is one raw plan: one preview, one confirmation.

1. **Targets.** Resolve the set per organize → Bulk Target Resolution (saved id list). A pass holds at most 19 renames (entity or device rows): its plan is 20 items, the snapshot first. More: several passes, each with its own preview and confirmation; say so up front.
2. **Convention.** A scheme the user states wins. Otherwise compare independent devices in the same domain, area, and floor (an area's floor: its `floor_id` in `config/area_registry/list`; `ha-nova find --area <area_id> --domain <domain>`; device names from `config/device_registry/list`), never several entities of one device: entity name, entity id, device name. Infer a pattern only from at least two examples that agree. Examples that disagree: show each pattern with its examples and ask which one, before any draft. One example or none: propose a scheme and say it is a proposal. Never take a household convention from one example.
3. **Map.** Per row: device name (only when asked; once per device), entity name, entity id before → after. Names follow `../organize/display-fields.md` → Names: the full name Home Assistant will show (the device name in front where it adds it), and for a device rename each of its entities' full names that change. A new id stays in its domain. Leave out, and say why: a new id that already exists (`ha-nova get <new id>` finds it, registry or state), two rows with one new id, a chain or swap (a new id that is another row's old id).
4. **Consumers**, for id changes only (a name change keeps every reference by id working): `ha-nova get <old id> --references` once per changed entity id; its hits cover the whole device, so keep only those whose `needle` is exactly that old id, saved as the holder list. A hit whose `needle` is that row's registry id keeps working: list it as unchanged; hits on the device id or a sibling entity are not affected. Its `related` holders (scenes, groups) that no hit names: list them too. Name each holder by its `name` (friendly name or dashboard title), never by its id alone. `managed: yaml` hits go to `ha-nova:yaml-config`, never into a batch. A `not_scanned` automation, script, or dashboard read blocks that row's id change: offer its name change alone.
5. **Preview.** One Report: per row the full name, entity name, entity id before → after; then each consumer by name and what happens to it ("stops working until updated: Morning lights, Hall dashboard"); then the coverage line (`not_covered`). Then one `ha-nova plan raw --file <requests.json>` (a raw plan, not a grouped change set): first `{"endpoint":"backups","payload":{"action":"save","category":"metadata","name":"auto-rename-<slug>","data":[...]}}` (`config-snapshots.md`) with each target's registry row exactly as `config/entity_registry/get` (devices: their `config/device_registry/list` row) returned it, its `id` included, then per row one `{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"<old id>","name":"…","new_entity_id":"…"}}` (both fields together; a device: `config/device_registry/update` with `device_id` and `name_by_user`), in map order. Plan it in the same turn as the Report, before any ask: the copy is the plan's first item, never a separate `relay backups` save. Its `say` closes the Report: one ask for the whole pass, at the tier the CLI decided (a rename that reaches a lock, door, or alarm automation, or more acted-on entities than it can check, asks the person: window, phone, or code). Keep that tier: never split the pass to get a lighter one. Never one ask per row.
6. **Apply** per `next_step`: the snapshot first, then the renames in order, stopping at the first that does not go through. Each rename is bound to the entry the preview read: another entry under its id, or a changed name, is drift (nothing sent) or stops that row. The snapshot fails: nothing is renamed; say so, never offer `--no-snapshot` for this pass. Report `result.items` as a ledger (done, stopped, not sent) and verify each done row from its registry row (`config/entity_registry/get` by the new id; a device: its `config/device_registry/list` row), not its state.
7. **Consumers next.** Right after verified id changes, offer the re-points, each in its own flow — the rename's confirmation never covers them. A yes to that offer covers the offered automations and scripts the reply accepts (a plain yes: all of them): draft those and show their preview in that same turn, never a which-ones question when the reply leaves no choice open; one dropped at the preview is a new plan and preview. Automations and scripts as drafts with each changed id replaced (templates included, nothing else changed), 2 to 20 as one `plan batch` (`batch-safety.md` → CLI Batch; more: one batch per 20, each its own ask), one alone through `ha-nova:write`; a blueprint instance: the id in its input through `ha-nova:write`, never in a batch; dashboards through `ha-nova:dashboard`, one plan each; other families per `../device/SKILL.md` → Hit Routing. Then `ha-nova get <old id> --references` per changed id, filtered the same way: no holder of the saved list may still name it, except those the user chose to leave.
8. **Way back.** Load the pass's own snapshot (`auto-rename-<slug>`). Per done row of its ledger, read the entry now (by its new id): only one whose registry `id` matches the snapshot row and whose touched fields still hold what the pass set goes back, with exactly those fields set to the snapshot's values (a `null` name stays `null`). One pass with those rows (same preview and one confirmation). Once it is applied, plan only the re-points this pass made back: one automation or script: `ha-nova plan revert --target <config_id>`; two or more: drafts with each new id replaced by its old one, as one `plan batch` (revert plans can't be batched, and applied one by one each drifts on the others), one ask. A row or config changed since is shown and asked about, never overwritten.

## Safe Migration Pattern

Use this when replacing an automation, script, or helper with a new target instead of editing in place.

1. Read and review the legacy config.
2. Build the new config through the normal write/helper flow.
3. Verify config read-back plus runtime presence.
4. Disable the legacy item instead of deleting immediately.
5. Let the new item soak.
6. Run consumer check again before final cleanup.

This is a sequential skill pattern: Review -> Write/Helper -> Verify -> Review. Do not keep multiple HA NOVA skills active at the same time.

## Orphan Cleanup

Find and clean up unused helpers. Uses review check H-07.

### Find Orphaned Helpers

For each helper entity, run:

```text
ha-nova relay ws --data-file <payload-file>
```

If no automations or scripts reference it, it's an orphan candidate.

### Bulk Discovery

1. List all helpers:
   ```text
   ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
   ```
   Write `<filter-file>` with:
   ```jq
   .data.entities[] | (.ei | split(".")[0]) as $domain | select(["input_boolean","input_number","input_select","input_text","input_datetime","input_button","counter","timer","schedule"] | index($domain)) | .ei
   ```
2. For each, run `search/related` and collect those with zero automation/script references.
3. Present the list to the user — some "orphans" may be intentionally UI-only (e.g., dashboard controls).

## search/related Signal Strength

- Read discipline: parse consumer checks through `../ha-nova/search-related-consumers.jq` (automation/script/scene projection; recreate per `../ha-nova/relay-api.md` → Parsing rule on flat-copy installs). Only a verified-shape empty result is a no-consumer signal; a filter error or a scan that did not run is inconclusive and never justifies a cleanup verdict.
- Helpers: strong signal for direct consumers
- Automations and scripts: medium signal; direct refs are good, templates can still hide usage
- Scenes: weak signal; always do a manual check before destructive cleanup

### Cleanup

For confirmed orphans, delete via the helper skill flow (one-time code required).

## Safety Rules

- Always run consumer check before any rename or delete
- Keep rename/delete work limited to the requested target and its directly affected consumers.
- Do not rewrite, rename, disable, or delete unrelated configs while fixing references.
- Template references (`states('...')`, `is_state('...')`) are NOT auto-updated — must be fixed manually
- Prefer disable over delete when impact is unclear
- Orphan detection is advisory — some helpers are intentionally UI-only
- All deletes keep their full confirmation (the plan's `say`), including cleanup, undo-create, orphan cleanup, failed-create cleanup, and deleting items created earlier in the same session.
- A delete is not done until follow-up verification confirms the target is gone.
- Do not present a destructive change as complete when consumer impact is still unresolved.
