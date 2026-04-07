---
name: dashboard
description: Use when listing, reading, creating, updating, or deleting storage-backed Home Assistant dashboards, Lovelace resources, and targeted card changes through HA NOVA Relay.
---

# HA NOVA Dashboard


## Scope

Storage dashboard work only:
- list dashboards
- read one dashboard config
- list Lovelace resources
- inspect the current dashboard structure: views, cards, badges, header cards
- create a new storage dashboard shell
- update dashboard metadata
- create, update, and delete Lovelace resources
- find a specific dashboard element before changing it
- add, update, move, and delete cards inside existing views
- delete an existing storage dashboard

This skill is safety-first:
- always resolve the target from `lovelace/dashboards/list`
- only write/delete when the dashboard `mode` is `storage`
- always read the full current dashboard before any content write
- always save the full merged config, never a guessed partial fragment
- always read back and verify the intended change

Not in scope:
- raw broad Lovelace editing without a concrete requested change
- view create/delete/reorder
- non-storage dashboard writes/deletes
- freeform new custom-card creation
- energy dashboard preferences

If the user asks for a broad redesign instead of a concrete safe change, narrow the request first. Do not guess.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based WS requests only:
- `ha-nova relay ws --data-file <payload-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>`
- `ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>`

Relevant WS types:
- `lovelace/dashboards/list`
- `lovelace/dashboards/create`
- `lovelace/dashboards/update`
- `lovelace/dashboards/delete`
- `lovelace/config`
- `lovelace/config/save`
- `lovelace/resources`
- `lovelace/resources/create`
- `lovelace/resources/update`
- `lovelace/resources/delete`

Critical behavior:
- `lovelace/config/save` is a full-document overwrite
- there is no partial update endpoint
- omitted views/cards are lost
- `lovelace/dashboards/list` is the source of truth for `dashboard_id`, `url_path`, and `mode`
- `lovelace/config/delete` is not the dashboard delete path for this skill
- `lovelace/resources` shows installed Lovelace resources, but that alone is not proof that a custom-card schema is safe to invent

## Flow

1. Resolve the dashboard target.
   - Always list dashboards first with `lovelace/dashboards/list`.
   - Match by `url_path`, title, or current identity.
   - Keep both identifiers:
     - `dashboard_id` for `lovelace/dashboards/update|delete`
     - `url_path` for `lovelace/config|save`
   - Ask one blocking question only if more than one dashboard still matches.
2. Check write/delete eligibility.
   - use the matched dashboard `mode` from `lovelace/dashboards/list`
   - only `mode=storage` is writable/deletable here
   - if `mode` is not `storage`, stop and explain that this skill will not write or delete it
3. Choose the mutation path.
   - create shell: preview `title`, `url_path`, `icon`, `require_admin`, `show_in_sidebar`, then call `lovelace/dashboards/create`
   - metadata update: preview the exact metadata fields, then call `lovelace/dashboards/update` with `dashboard_id`
     - only send changed metadata fields supported there: `title`, `icon`, `show_in_sidebar`, `require_admin`
     - do not resend `url_path`, `mode`, or unrelated config fields in the update payload
   - resource inventory: use `lovelace/resources`
   - resource create/update:
     - preview `res_type` and `url`
     - call `lovelace/resources/create|update`
   - resource delete:
     - preview the exact resource identity
     - require exact token confirmation `confirm:<token>`
     - call `lovelace/resources/delete` with `resource_id`
   - content update / card operation:
     - read the current dashboard config with `lovelace/config`
     - build a compact inventory of views, cards, badges, and header cards
     - resolve the exact target by view, title/heading text, entity reference, card type, or explicit position
     - merge the requested change in memory
     - preview a concise diff/excerpt
     - save the full merged config with `lovelace/config/save`
     - new cards may be created only from this built-in allowlist:
       - `entity`, `entities`, `button`, `tile`, `gauge`, `sensor`, `markdown`, `history-graph`
     - existing custom cards may only be moved, deleted, or shallow-updated when the exact field already exists
   - delete:
     - preview the exact dashboard identity
     - require exact token confirmation `confirm:<token>`
     - call `lovelace/dashboards/delete` with `dashboard_id`
4. Read the current dashboard when content changes are involved.
   - use `lovelace/config` with the chosen `url_path`
5. Read back and verify:
   - create / metadata update / delete: verify through `lovelace/dashboards/list`
   - resource create/update/delete: verify through `lovelace/resources`
   - content update: verify through `lovelace/config`
   - content update must confirm both the intended field change and unrelated-view survival
6. If verification fails, stop and report the mismatch. Do not retry by guessing.

## Output Format

For list/read:
- `Dashboard`
- `Target`
- `Summary`
- relevant config or inventory excerpt only

For create/update/delete:
- `Dashboard`
- `Mode`
- `Planned change`
- `Verification`
- `Next step`

Do not dump the full dashboard JSON/YAML by default.

## Safety

- No guessed `url_path` or `dashboard_id` values.
- Create/update uses natural confirmation after preview.
- Dashboard/resource delete uses exact token confirmation only.
- If the requested change would require a broad re-layout instead of a targeted edit, say so before writing.

## Guardrails

- Never call `lovelace/config/save` with a partial config.
- Never use `lovelace/info` to decide whether a dashboard is writable.
- Never use `lovelace/config/delete` as the dashboard delete path.
- Never verify only by save success; always read back the dashboard.
- Never probe a different dashboard's config just to infer behavior for the target dashboard.
- Never invent a new custom-card schema just because a resource exists.
- If the dashboard target is still ambiguous after one clarification, stop and explain the remaining ambiguity.
