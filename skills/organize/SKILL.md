---
name: organize
description: Use when organizing Home Assistant areas, floors, labels, categories, and entity/device metadata through HA NOVA Relay.
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
- entity removal
- removing config entries from devices
- device category assignment
- zones, persons, tags
- energy or calendar management

For those surfaces, hand off to `ha-nova:fallback`.

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
   - areas/floors: show linked areas or assignments that will lose placement
   - labels: show linked items when quickly resolvable
   - categories: show affected entity count and a small example set when resolvable
   - require token confirmation `confirm:<token>`
5. Execute exactly one mutation.
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

## Output Format

- `Target`
- `Current metadata`
- `Requested change`
- `Impact` for deletes
- `Verification`
- `Next step`

Default to a compact field summary, not raw registry JSON.

## Safety

- Never guess ids.
- Create/update uses natural confirmation after preview.
- Delete uses token confirmation only.
- If the user asks for entity removal, device category assignment, or config-entry detachment, stop and hand off to `ha-nova:fallback`.

## Guardrails

- One resource at a time.
- One category scope at a time.
- Metadata updates only; no destructive registry admin beyond area/floor/label/category delete.
- Verify the changed field values, not just the WS success response.
- If a delete impact preview is incomplete, say so explicitly before asking for confirmation.
