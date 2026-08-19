---
name: dashboard
description: Use when listing, reading, creating, updating, or deleting storage-backed Home Assistant dashboards, Lovelace resources, and targeted card changes through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
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

Read and follow `../ha-nova/session-bootstrap.md`.
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
- `lovelace/config/save` is a full-document overwrite. Payload shape: `{"type":"lovelace/config/save","url_path":"<url_path>","config":{"views":[...]}}` — the whole document goes under `config`, and whatever is missing from it is gone. For the DEFAULT dashboard omit `url_path` entirely; the field takes a string, so an explicit `null` is rejected and the save never lands
- there is no partial update endpoint
- omitted views/cards are lost
- `lovelace/dashboards/list` is the source of truth for `dashboard_id`, `url_path`, and `mode`
- `lovelace/config/delete` is not the dashboard delete path for this skill
- `lovelace/resources` shows installed Lovelace resources, but that alone is not proof that a custom-card schema is safe to invent

## Flow

Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.


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
   - create shell: preview `title`, `url_path`, `icon`, `require_admin`, `show_in_sidebar`, confirm this exact preview, then call `lovelace/dashboards/create`
   - metadata update: preview the exact metadata fields, confirm this exact preview, then call `lovelace/dashboards/update` with `dashboard_id`
     - only send changed metadata fields supported there: `title`, `icon`, `show_in_sidebar`, `require_admin`
     - do not resend `url_path`, `mode`, or unrelated config fields in the update payload
   - resource inventory: use `lovelace/resources`
   - resource create/update:
     - preview `res_type` and `url`
     - confirm this exact preview
     - call `lovelace/resources/create|update`
   - resource delete:
     - preview the exact resource identity
     - require exact confirmation code `confirm:<token>`
     - call `lovelace/resources/delete` with `resource_id`
   - content update / card operation:
     - read the current dashboard config with `lovelace/config`
     - build a compact inventory of views, cards, badges, and header cards
     - jq filters must null-guard absent structure keys: empty dashboards have no `views` — iterate `(.views // [])[]`, never bare `.views[]`; same for `.cards` and `.badges`
     - resolve the exact target by view, title/heading text, entity reference, card type, or explicit position
     - merge the requested change in memory
     - preview a concise diff/excerpt plus a plain-language behavior line (write-safety → Behavior narrative)
     - confirm this exact preview
     - drift check between confirmation and save: if the conversation paused since the preview, re-read the live config and compare the FULL document against the merge basis — including the very view/card being edited; on any foreign change, STOP — confirmation expired; re-merge onto the fresh read and re-preview (the full-document save would silently revert the external edit)
     - capture the auto config snapshot of the pre-save document when the save removes views or cards (`skills/ha-nova/config-snapshots.md`; on capture failure follow its capture-failure stop)
     - save the full merged config with `lovelace/config/save`
     - new cards may be created only from this built-in allowlist:
       - `entity`, `entities`, `button`, `tile`, `gauge`, `sensor`, `markdown`, `history-graph`
     - existing custom cards may only be moved, deleted, or shallow-updated when the exact field already exists
     - persisted card removal is destructive and requires exact confirmation code `confirm:<token>`; only discarding an unpersisted draft card is non-destructive
   - delete:
     - preview the exact dashboard identity
     - require exact confirmation code `confirm:<token>`
     - capture the auto config snapshot first — data = `{shell: <the dashboard's lovelace/dashboards/list entry>, config: <the full lovelace/config>}`, so a later-session restore can recreate the shell (url_path, title, icon, sidebar, admin flag) before saving the content (`skills/ha-nova/config-snapshots.md`; on capture failure follow its capture-failure stop)
     - call `lovelace/dashboards/delete` with `dashboard_id`
     - multi-item deletes follow `skills/ha-nova/batch-safety.md`; dashboards, resources, and cards are separate families — one family per manifest; a card batch within one dashboard executes as ONE merged `lovelace/config/save` (single-call path — sequential per-card saves would overwrite each other), verified by one read-back
   - an entity reference on an existing dashboard may join a non-destructive grouped change set ONLY as the downstream operation of a dependency-bound set (`skills/ha-nova/grouped-change-set.md` → Dependency-Bound Outputs); every other dashboard write stays single-operation
4. Read the current dashboard when content changes are involved.
   - use `lovelace/config` with the chosen `url_path`
5. Read back and verify:
   - create / metadata update / delete: verify through `lovelace/dashboards/list`
   - resource create/update/delete: verify through `lovelace/resources`
   - content update: verify through `lovelace/config`
   - content update must confirm both the intended field change and unrelated-view survival
6. If verification fails, stop and report the mismatch. Do not retry by guessing.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there.

For list/read:
- `Dashboard`
- `Target`
- `Summary`
- relevant config or inventory excerpt only

For create/update/delete:
- `Dashboard`
- `Mode`
- `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / confirmation-code prompt
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots, do not invent ad-hoc headings. List/read renders the Report shape; dashboard and resource inventories render the List Frame (output-rules.md).

Do not dump the full dashboard JSON/YAML by default.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No guessed `url_path` or `dashboard_id` values.
- Dashboard/resource/card delete uses exact confirmation code only, even for items created earlier in the same session. Dashboard writes have no `revert` — recovery for dashboard/card deletes is the auto config snapshot (next bullet); resources recover via Home Assistant Backups.
- Content-removing saves capture an auto config snapshot — that is the recovery net. Dashboard deletes capture one too, but restore is PARTIAL: recreate the dashboard (reusing the `url_path`), then save the snapshot content — say that in the delete preview. LOVELACE RESOURCE deletes are NOT snapshot-covered — they keep the safety-backup offer. Otherwise offer a backup via `ha-nova:backup` only when the snapshot store is unavailable (never for routine small edits).
- If the change needs a broad re-layout instead of a targeted edit, say so before writing.

## Guardrails

- Never call `lovelace/config/save` with a partial config.
- Never use `lovelace/info` to decide whether a dashboard is writable.
- Never use `lovelace/config/delete` as the dashboard delete path.
- Never verify only by save success; always read back the dashboard.
- Never probe a different dashboard's config just to infer behavior for the target dashboard.
- Never invent a new custom-card schema just because a resource exists.
- If the target stays ambiguous after one clarification, stop and explain.
