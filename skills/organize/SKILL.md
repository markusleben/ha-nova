---
name: organize
description: Use when organizing Home Assistant areas, floors, labels, categories, and entity/device metadata through HA NOVA Relay.
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
- entity metadata updates: rename, move to area, assign/clear/add/remove labels, assign/remove categories, disable, hide, aliases
- entity category assignment/removal per scope
- device metadata updates: rename, move to area, assign/clear/add/remove labels, disable

Not in scope:
- entity removal (`ha-nova:maintenance` — dead registry entries only)
- removing config entries from devices
- device category assignment
- zones, persons, tags
- energy management (`ha-nova:energy`) or calendar management (`ha-nova:calendar`)

Hand off to the skill named per line; surfaces without one go to `ha-nova:fallback`.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based WS requests only:
- `ha-nova relay ws --data-file <payload-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>`

Registry commands owned here:
- `config/area_registry/list|create|update|delete`
- `config/floor_registry/list|create|update|delete`
- `config/label_registry/list|create|update|delete`
- `config/category_registry/list|create|update|delete`
- `config/entity_registry/get|list|update`
- `config/device_registry/list|update`

Use `search/related` when a delete needs a quick impact preview on linked items.

## Flow

1. Resolve the exact target type and identity.
   - areas/floors/labels/categories: use the corresponding `list`
   - every category registry call must include the exact `scope`
   - do not call `config/category_registry/list|create|update|delete` without `scope`
   - entities: prefer `config/entity_registry/get` when the entity_id is known
   - devices: use `config/device_registry/list` and match by current name or linked entities
2. If multiple candidates remain, ask one blocking question.
3. For create/update:
   - build the smallest valid payload
   - preview the exact fields that will change
   - confirm this exact preview before executing
   - rich metadata owned here:
     - area: `name`, `floor_id`, `icon`, `picture`, `aliases`
     - floor: `name`, `level`, `icon`, `aliases`
     - label: `name`, `color`, `icon`, `description`
     - category: `name`, `icon`, exact `scope`
   - category assignment/removal is entity-only in this skill and uses one scope per request
   - category assignment uses `categories: {"<scope>":"<category_id>"}`
   - category removal for one scope uses `categories: {"<scope>": null}`
   - do not send `categories: {}` when the goal is to clear one existing scoped category
   - label updates on entities/devices may be:
     - replace all labels
     - add labels
     - remove labels
     - clear labels
4. For delete:
   - preview impact first
   - areas/floors: show linked areas or assignments that will lose placement; run WS `search/related` on the area and name automations/scripts/scenes that target it — the delete leaves those targets dangling
   - labels: run WS `search/related` (`item_type: "label"`) and name the automations/scripts that reference the label; show other linked items when quickly resolvable
   - categories: show affected entity count and a small example set when resolvable
   - require token confirmation `confirm:<token>`
   - same-family batch deletes follow `skills/ha-nova/batch-safety.md`, only when related-item impact is complete for every target
5. Execute exactly one mutation (or the confirmed batch manifest, sequentially).
6. Read back and verify the requested fields:
   - area/floor/label/category: re-list and match by canonical id
   - entity/device: re-read the updated registry entry
   - category delete: also verify affected entity categories are cleared for that scope
7. Report the final state in plain language.

## Entity / Device Update Fields

Use only exposed metadata fields:
- entity: `name`, `new_entity_id`, `area_id`, `labels`, `categories`, `disabled_by`, `hidden_by`, `aliases`
- device: `name_by_user`, `area_id`, `labels`, `disabled_by`

Do not guess unsupported fields.

`new_entity_id` and `disabled_by` are NOT low-risk metadata: a rename breaks every automation, scene, script, dashboard, and template that references the old id, and disabling removes the entity from the state machine with the same effect on consumers. Before either, run WS `search/related` on the entity and name the consumers in the preview — and say plainly that it covers automations, scripts, groups, persons, and scenes only: template references and dashboards (storage AND YAML mode) are NOT indexed and cannot be ruled out this way (a storage-dashboard scan via `ha-nova:dashboard` is the thorough follow-up). After a rename, offer to update the found consumers through their owning skills.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

- `Target`
- `Current metadata`
- `Requested change`
- `Impact` for deletes
- `Save status` / `Delete status` before confirmation
- `Options` / confirmation token
- `Verification`
- `Next step`

Use stable localized slot labels in this order for previews; omit empty slots, but do not invent ad-hoc headings.

Metadata reads render the Report shape; registry inventories render the List Frame (output-rules.md). Default to a compact field summary, not raw registry JSON.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Delete uses token confirmation only, even for cleanup of items created earlier in the same session.
- Registry deletes (areas, floors, labels, categories) are irreversible and a recreate mints a new id — inbound references stay broken. Offer a safety backup via `ha-nova:backup` before them (never for metadata edits).
- If the user asks for entity removal, stop and hand off to `ha-nova:maintenance` (dead registry entries only — for live entities offer disable here instead). For device category assignment or config-entry detachment, hand off to `ha-nova:fallback`.

## Guardrails

- One resource at a time — except a confirmed batch manifest per `skills/ha-nova/batch-safety.md` (its `confirm:batch-...` code replaces the single-target token for that batch).
- One category scope at a time.
- Metadata updates only; no destructive registry admin beyond area/floor/label/category delete.
- Verify the changed field values, not just the WS success response.
- If a delete impact preview is incomplete, say so explicitly before asking for confirmation.
- Registry deletes are irreversible and have no `revert`. Before confirming a delete, say so plainly and point to Home Assistant Backups (Settings > System > Backups) as the recovery path.
