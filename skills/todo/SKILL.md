---
name: todo
description: Use when listing, reading, or managing Home Assistant to-do lists and their items (add, update, complete, remove) through HA NOVA Relay, including creating or deleting local to-do lists.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Todo

## Scope

To-do lists and items:
- discover lists, read items
- add, rename, complete, update, remove items; clear completed
- create/delete Local To-do lists

Not in scope:
- calendar events: `ha-nova:calendar`
- shopping-list legacy integration specifics beyond its `todo.*` entity
- automations acting on lists: `ha-nova:write`

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based:
- `ha-nova relay core --method POST --path <service-path> --body-file <payload-file>`
- `--out <result-file>` for large responses

Relay-core response body: `.data.body` (`skills/ha-nova/relay-api.md` → Standard Envelope).

## Feature Gate (critical)

Providers differ (Local To-do supports everything; cloud providers often do not). Before any item write, read `supported_features` from `/api/states/<todo_entity_id>` and gate the operation:

| Bit | Feature |
|---|---|
| 1 | create item |
| 2 | delete item |
| 4 | update item |
| 8 | move item |
| 16 | set due date |
| 32 | set due datetime |
| 64 | set description |

If the operation or field is not in the mask, name the provider limitation and offer what IS supported — never fire the service anyway.

## Flow

### Discover lists
`ha-nova relay core --method GET --path /api/states --out <result-file>`, then filter `todo.*`: entity_id, friendly_name, `state` (open-item count), `supported_features`. If several lists plausibly match the user's name, ask one blocking question — never guess between similar names.

### Read items
`todo.get_items` is a response service — the query parameter is mandatory, without it HA returns 400:
```text
ha-nova relay core --method POST --path "/api/services/todo/get_items?return_response" --body-file <payload-file> --out <result-file>
```
Body: `{"entity_id":"todo.<list>"}`. Items live under `.data.body.service_response["todo.<list>"].items` with `uid`, `summary`, `status` (`needs_action`/`completed`), optional `due`, `description`. Filter by status only when the user asks.

### Add item
Body: `{"entity_id":"todo.<list>","item":"<summary>","due_date":"YYYY-MM-DD","description":"..."}` → POST `/api/services/todo/add_item`.
- `due_date` and `due_datetime` are mutually exclusive; include either or `description` only when the Feature Gate allows.
- Check for an existing open (`needs_action`) item with the same summary first; ask before adding a duplicate.
- Read items back to confirm (get the new `uid`).

### Update / complete / rename
POST `/api/services/todo/update_item` with `{"entity_id":"todo.<list>","item":"<uid>", ...}` — `item` = the `uid` from Read items (a summary works but is ambiguous when duplicated — always prefer `uid`), plus any of `status` (`completed`/`needs_action`), `rename`, `due_date`/`due_datetime`, `description`.

### Reorder items (feature bit 8)
No service exists — send WS `{"type":"todo/item/move","entity_id":"todo.<list>","uid":"<uid>","previous_uid":"<uid>"}` via `ha-nova relay ws --data-file <payload-file>`; omit `previous_uid` for the top; read back to confirm.

### Remove items
1. Read items first and resolve exact `uid`s — removing a non-existent item is a raw HA error (400 or 500 by version), not a clean message.
2. POST `/api/services/todo/remove_item` with `{"entity_id":"todo.<list>","item":[...]}` (one or many uids).
3. "Clear completed": POST `/api/services/todo/remove_completed_items` with `{"entity_id":"todo.<list>"}` — the service requires the target; without it, nothing happens silently. After a conversation pause, re-read and re-preview the completed set first.
4. Read back and confirm — for clear-completed, zero `completed` items remain; otherwise the expected item count.

### Create a list (Local To-do)
1. Start the flow: POST `/api/config/config_entries/flow` with `{"handler":"local_todo"}` — the single step asks for `todo_list_name`.
2. Preview the name; natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation).
3. Submit: POST `/api/config/config_entries/flow/<flow_id>` with `{"todo_list_name":"<name>"}` → `create_entry` with `entry_id`.
4. Resolve the new `todo.*` entity from the entity registry by matching `config_entry_id` to the returned `entry_id` — never derive the entity_id by slugifying the name (registry lag: retry once).
Other providers configure lists in their own integration — point there instead of guessing flows.

### Delete a list
1. Resolve `entry_id` via the entity registry `config_entry_id` (WS `config/entity_registry/get`, see `skills/ha-nova/relay-api.md` → Registry Queries), never guess.
2. **Domain gate:** read the config entry and proceed only if its `domain` is `local_todo`. For any other domain (shopping_list, Google Tasks, CalDAV, Alexa) the entry is the WHOLE integration, not one list — deleting it destroys every list plus the account link; refuse and point to that integration's own management.
3. Run WS `search/related` on the `todo.<entity>` first and name automations/scripts that reference this list in the preview — they break when it disappears. Deleting the entry destroys the list AND all its items irreversibly (Local To-do also deletes its stored file) — say so plainly, with the list's open-item count in the preview; require exact confirmation code `confirm:<token>` — generate a short code, display it in the Options slot, and proceed only when the user types it back exactly. Deleting several lists at once follows `skills/ha-nova/batch-safety.md` (per-list open-item counts in the manifest); item removal keeps the existing flow.
4. DELETE `/api/config/config_entries/entry/<entry_id>`.
5. Verify the entity is gone (states GET 404).

## Error Handling

- `get_items` returning 400 "requires responses": the `?return_response` query parameter is missing.
- Item-service 400/500: the item does not exist (stale uid/summary) — re-read items instead of retrying.
- Read-back missing a just-added item on a cloud provider (Google Tasks, CalDAV): sync lag, not failure — retry `get_items` once before reporting; never re-add.
- HA has no recurring items — for "every week/day" requests, offer a scheduled automation calling `todo.add_item` (`ha-nova:write`).
- Due datetimes use the HA instance timezone unless an offset is given; phrase times for the user locally.
- Full relay error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all output. Write previews, delete confirmations, and results render as the Cards defined there.

- `List`
- `Items` / `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / confirmation-code prompt
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Item lists render the List Frame (output-rules.md) — never raw JSON.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.
- `search/related` verdicts fail closed: verify `ok=true` and `data` is an object before projecting family keys (`skills/ha-nova/relay-api.md` → Parsing rule); a failed or unexecuted scan is inconclusive — never a no-consumer result.

- Declared exception to the core delete rule above: item removes (`todo.remove_item`, `todo.remove_completed_items`) are re-addable data edits, deliberately below the destructive confirmation-code tier — they use natural confirmation bound to the compact preview. List deletion stays at the typed confirmation code.
- Item operations have no `revert`; removed items are gone — bulk removals list what will be removed in the preview.
- Never guess uids or entry_ids; resolve via Read items / registry first.
- Item operations on ONE list — adds, completes, renames, updates, up to 10 — may confirm as a single grouped change set (`skills/ha-nova/grouped-change-set.md`): one manifest listing every item with its duplicate check and one natural confirmation. Re-read the list ONCE immediately before applying, and compare it to the manifest: the grouped contract's pre-apply check exists because another client can add the item this batch also adds, or rename the item it is about to update, between preview and apply — a post-write read-back cannot see that, it only confirms what this batch wrote. Any drift stops the batch and re-previews. Then read back after EACH applied operation, not once at the end. A provider that silently ignores the first update must stop the batch there, per the grouped contract's fail-fast ledger; a single trailing read would record the rest as applied anyway. Four items should not cost four rounds. List deletes keep `batch-safety.md` unchanged. One list per mutation; verify by reading back, not by service success alone.
