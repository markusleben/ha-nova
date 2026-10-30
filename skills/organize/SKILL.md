---
name: organize
description: Use when organizing Home Assistant areas, floors, labels, categories, and entity/device metadata, or deleting a device, through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Organize


## Scope

Organization and registry metadata only:
- areas: list/create/update/delete
- floors: list/create/update/delete
- labels: list/create/update/delete
- categories: list/create/update/delete
- entity metadata updates: rename, move to area, assign/clear/add/remove labels, assign/remove categories, disable AND re-enable, hide, aliases, icon, "Show as", display precision, unit
- entity category assignment/removal per scope
- device metadata updates: rename, move to area, assign/clear/add/remove labels, disable
- device delete

Device categories do not exist; offer entity categories instead.

Not in scope:
- entity removal (`ha-nova:maintenance` — dead registry entries only)
- zones, persons, tags (`ha-nova:admin`)
- energy management (`ha-nova:energy`) or calendar management (`ha-nova:calendar`)

Hand off per line; anything else goes to `ha-nova:fallback`.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based WS requests only: `ha-nova relay ws --data-file <payload-file>`, plus `--out <result-file>` for large results.

Registry commands owned here:
- `config/area_registry/list|create|update|delete`
- `config/floor_registry/list|create|update|delete`
- `config/label_registry/list|create|update|delete`
- `config/category_registry/list|create|update|delete`
- `config/entity_registry/get|list|update`
- `config/device_registry/list|update` (`remove`: Delete a Device)

## Flow

1. Resolve the exact target type and identity.
   - areas/floors/labels/categories: use the corresponding `list`
   - every category registry call must include the exact `scope`
   - do not call `config/category_registry/list|create|update|delete` without `scope`
   - entities: prefer `config/entity_registry/get` when the entity_id is known
   - devices: use `config/device_registry/list` and match by current name or linked entities
2. If multiple candidates remain, ask one blocking question.
3. For create/update:
   - preview exactly the fields of the smallest valid payload; confirm that preview first
   - rich metadata owned here:
     - area: `name`, `floor_id`, `icon`, `picture`, `aliases`, `labels`, temperature/humidity sensor
     - floor: `name`, `level`, `icon`, `aliases`
     - label: `name`, `color`, `icon`, `description`
     - category: `name`, `icon`, exact `scope`
   - category assignment/removal is entity-only in this skill and uses one scope per request
   - category assignment uses `categories: {"<scope>":"<category_id>"}`
   - category removal for one scope uses `categories: {"<scope>": null}`
   - do not send `categories: {}` when the goal is to clear one existing scoped category
   - label updates on entities/devices/areas may be:
     - replace all labels
     - add labels
     - remove labels
     - clear labels
4. For delete:
   - preview impact first
   - areas/floors: show linked areas or assignments that will lose placement; run WS `search/related` with the matching item type and name automations/scripts/scenes that target it — the delete leaves those targets dangling
   - labels: run WS `search/related` (`item_type: "label"`) and name the automations/scripts that reference the label
   - categories: show affected entity count and examples
   - require the one-time code
   - same-family batch deletes follow `../ha-nova/batch-safety.md`, only when related-item impact is complete for every target
5. Execute exactly one mutation (or the confirmed batch manifest or grouped change set per `../ha-nova/grouped-change-set.md`, sequentially).
6. Read back and verify the requested fields:
   - area/floor/label/category: re-list and match by canonical id
   - entity/device: re-read the updated registry entry
   - category delete: also verify affected entity categories are cleared for that scope
7. Report the final state.

## Bulk Target Resolution

For a SET selected by prefix, domain, area, or label, save before previewing, and resolve the shortlist per `../ha-nova/bulk-patterns.md` (Selector Semantics + Discovery Pipeline); device shortlists use `config/device_registry/list`, not `.data.entity`. The manifest binds to that exact saved id list — never to the raw selector.

## Entity / Device Update Fields

Use only exposed metadata fields:
- entity: `name`, `new_entity_id`, `area_id`, `labels`, `categories`, `disabled_by`, `hidden_by`, `aliases`, `icon`, `device_class`, `options_domain`+`options`
- `icon`, `device_class`, `options`, and area labels or sensors: read `display-fields.md` first
- device: `name_by_user`, `area_id`, `labels`, `disabled_by`

Refused here: disabling or enabling a lock, alarm panel, or cover; a device with a lock, alarm panel, or access cover, or a child device with one (disabled with it); or a switch whose device has a `switch_as_x` one (registry `platform: switch_as_x`). Say so before any preview; point at **Settings > Devices & services**.

Re-ENABLING a registry-disabled item depends on WHO disabled it (`disabled_by`): `"user"` or `"integration"` (disabled by default) → the write `{"disabled_by": null}` on the entity record; `"device"` → the entity follows its parent — re-enable the DEVICE record (`{"disabled_by": null}` on the device registry entry, same preview/consumer rules as a device-level disable), never the entity alone; `"config_entry"` (the integration is disabled) → enable the integration itself (`ha-nova:integration-setup`), never the entity. Previewed and confirmed; then read the response: `require_restart: true` → it appears after a restart; `reload_delay: <seconds>` → after ~that delay; never actuate the freshly enabled entity from this skill.

`new_entity_id` and `disabled_by` are NOT low-risk metadata: a rename or disable breaks every automation, scene, script, dashboard, and template that references the entity. Before either, run WS `search/related` on the entity — for a DEVICE-level `disabled_by`, expand the device to its entities first (entity registry by `device_id`, child devices' too) and run `search/related` per entity, because the device relation itself does not collect the child entities' consumers — and name the union of consumers in the preview — and say plainly that it covers automations, scripts, groups, persons, and scenes only: template references and dashboards (storage AND YAML mode) are NOT indexed (`ha-nova get <entity_id> --references` also reads templates and dashboards). After a rename, offer to update the found consumers through their owning skills. Before either write, capture the auto config snapshot of the record actually being mutated — the entity's registry fields for a rename/entity disable, the DEVICE registry record for a device-level `disabled_by` (category `metadata`, `../ha-nova/config-snapshots.md`; on capture failure follow its capture-failure stop). For a RENAME, store the target `new_entity_id` with the old record: restore finds it under the new id and renames it back; otherwise restore writes the fields back in place.

## Delete a Device

HA 2026.9+ (`get_config`; older: **Settings > Devices & services**); the owning entry (`config_entries/get`) needs `supports_remove_device: true`. Refused as for a device disable; also HA NOVA's App. Preview the device, child devices (`parent_device_id`), their entities (same entry and subentry: deleted; others: unlinked), and consumers: `search/related` per device and entity. One `ha-nova plan raw --file <requests.json>`: a `backups` save (`metadata`, `../ha-nova/config-snapshots.md`: full device and entity registry rows), then `{"type":"config/device_registry/remove","device_id":"<id>"}`; `ha-nova apply` per its `next_step`. Unsupported or refused: offer the HA UI or removing its integration entry (`ha-nova:fallback`, own preview: all its devices go). No undo (the copy is a record only); offer `ha-nova:backup` first. Verify it is gone.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked or names clash, no ids, "entity", modes, Relay/HA URLs, snapshots, raw JSON. Previews, deletes, and results use the Cards defined there (`../ha-nova/output-cards.md`).

- `Target`
- `Current metadata`
- `Requested change`
- `Impact` for deletes
- `Save status` / `Delete status` before confirmation
- `Options` / one-time-code prompt
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots.

Metadata reads render the Report shape; registry inventories render the List Frame (output-cards.md). Default to a compact field summary.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing speculative.
- `search/related` verdicts fail closed: verify `ok=true` and `data` is an object before projecting family keys (`../ha-nova/relay-api.md` → Parsing rule); a failed or unexecuted scan is inconclusive — never a no-consumer result.

- Delete uses the one-time code only, even for cleanup of items created earlier in the same session.
- Registry deletes are irreversible (areas, floors, labels, categories), have no `revert`, and a recreate mints a new id — inbound references stay broken. Before confirming one, say so plainly, offer a safety backup via `ha-nova:backup` (never for metadata edits), and name Home Assistant Backups (Settings > System > Backups) as the recovery path.

## Guardrails

- One resource at a time — except a confirmed batch manifest per `../ha-nova/batch-safety.md` (its one manifest code replaces the single-target codes for that batch), a non-destructive grouped change set (max 10 registry updates), or this skill's items inside a cross-family destructive cleanup manifest — both per `../ha-nova/grouped-change-set.md`.
- One category scope at a time.
- Metadata updates only; no destructive registry admin beyond area/floor/label/category and device delete.
- Verify the changed field values, not just the WS success response.
- If a delete impact preview is incomplete, say so explicitly before asking for confirmation.
