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

`ha-nova call` returns the preview: confirm it once, then `ha-nova apply --plan <id>`.

Item writes: `ha-nova call <todo.service> todo.<list> --skill ha-nova:todo [--set k=v]…` (the list is the argument, never `entity_id` data). Reads: `ha-nova call todo.get_items todo.<list> --response` (no `--skill`: read-only). Raw service POSTs are refused.

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
`todo.get_items` is a response service (`--response` adds the mandatory `?return_response`; without it HA returns 400):
```text
ha-nova call todo.get_items todo.<list> --response
```
Items live under `result.service_response["todo.<list>"].items` with `uid`, `summary`, `status` (`needs_action`/`completed`), optional `due`, `description`. Filter by status only when the user asks.

### Add item
`ha-nova call todo.add_item todo.<list> --skill ha-nova:todo --set item="<summary>" [--set due_date=YYYY-MM-DD] [--set description=…]`.
- `due_date` and `due_datetime` are mutually exclusive; include either or `description` only when the Feature Gate allows.
- Check for an existing open (`needs_action`) item with the same summary first; ask before adding a duplicate.
- Read items back to confirm (get the new `uid`).

### Update / complete / rename
`ha-nova call todo.update_item todo.<list> --skill ha-nova:todo --set item="<uid>" …` — `item` = the `uid` from Read items (a summary is ambiguous when duplicated — always prefer `uid`), plus any of `--set status=completed|needs_action`, `--set rename=…`, `--set due_date=…`/`--set due_datetime=…`, `--set description=…`.

### Reorder items (feature bit 8)
No service exists — send WS `{"type":"todo/item/move","entity_id":"todo.<list>","uid":"<uid>","previous_uid":"<uid>"}` via `ha-nova relay ws --data-file <payload-file>`; omit `previous_uid` for the top; read back to confirm.

### Remove items
1. Read items first and resolve exact `uid`s — removing a non-existent item is a raw HA error (400 or 500 by version), not a clean message.
2. `ha-nova call todo.remove_item todo.<list> --skill ha-nova:todo --data-file <payload-file>` with `{"item":[...]}` (one or many uids).
3. "Clear completed": `ha-nova call todo.remove_completed_items todo.<list> --skill ha-nova:todo`. After a conversation pause, re-read and re-preview the completed set first.
4. Read back and confirm — for clear-completed, zero `completed` items remain; otherwise the expected item count.

### Create a list (Local To-do)
1. Start the flow: POST `/api/config/config_entries/flow` with `{"handler":"local_todo"}` — the single step asks for `todo_list_name`.
2. Preview the name and end the turn with it (see context skill → Active Preview Confirmation).
3. Submit: POST `/api/config/config_entries/flow/<flow_id>` with `{"todo_list_name":"<name>"}` → `create_entry` with `entry_id`.
4. Resolve the new `todo.*` entity from the entity registry by matching `config_entry_id` to the returned `entry_id` — never derive the entity_id by slugifying the name (registry lag: retry once).
Other providers configure lists in their own integration — point there instead of guessing flows.

### Delete a list
1. Resolve `entry_id` via the entity registry `config_entry_id` (WS `config/entity_registry/get`, see `../ha-nova/relay-api.md` → Registry Queries), never guess.
2. **Domain gate:** read the config entry and proceed only if its `domain` is `local_todo`. For any other domain (shopping_list, Google Tasks, CalDAV, Alexa) the entry is the WHOLE integration, not one list — deleting it destroys every list plus the account link; refuse and point to that integration's own management.
3. Run WS `search/related` on the `todo.<entity>` first and name automations/scripts that reference this list in the preview — they break when it disappears. Deleting the entry destroys the list AND all its items irreversibly (Local To-do also deletes its stored file) — say so plainly, with the list's open-item count in the preview; require the one-time code — mint four random digits, display them in the Options slot, and proceed only on a reply that is just the code (spaces ignored). Deleting several lists at once follows `../ha-nova/batch-safety.md` (per-list open-item counts in the manifest); item removal keeps the existing flow.
4. DELETE `/api/config/config_entries/entry/<entry_id>`.
5. Verify the entity is gone (states GET 404).

## Error Handling

- `get_items` returning 400 "requires responses": the `?return_response` query parameter is missing.
- Item-service 400/500: the item does not exist (stale uid/summary) — re-read items instead of retrying.
- Read-back missing a just-added item on a cloud provider (Google Tasks, CalDAV): sync lag, not failure — retry `get_items` once before reporting; never re-add.
- HA has no recurring items — for "every week/day" requests, offer a scheduled automation calling `todo.add_item` (`ha-nova:write`).
- Due datetimes use the HA instance timezone unless an offset is given; phrase times for the user locally.
- Full relay error taxonomy: `../ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `../ha-nova/output-rules.md` to all output. Write previews, delete confirmations, and results render as the Cards defined there (`../ha-nova/output-cards.md`).

- `List`
- `Items` / `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / one-time-code prompt
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Item lists render the List Frame (output-cards.md) — never raw JSON.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's one-time code from HA NOVA, or their window click or phone tap; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing speculative.
- `search/related` verdicts fail closed: verify `ok=true` and `data` is an object before projecting family keys (`../ha-nova/relay-api.md` → Parsing rule); a failed or unexecuted scan is inconclusive — never a no-consumer result.

- Declared exception to the core delete rule above: item removes (`todo.remove_item`, `todo.remove_completed_items`) are re-addable data edits, deliberately below the destructive one-time-code tier — they use natural confirmation bound to the compact preview. List deletion stays at the one-time code.
- Item operations have no `revert`; removed items are gone — bulk removals list what will be removed in the preview.
- Never guess uids or entry_ids; resolve via Read items / registry first.
- Item operations on ONE list — adds, completes, renames, updates, up to 10 — may confirm as a single grouped change set (`../ha-nova/grouped-change-set.md`): one manifest listing every item with its duplicate check and one natural confirmation. Re-read the list before EACH operation, not once before the batch, and compare it to the manifest: another client can change the list between two of your own operations just as easily as before the first one, and a single pre-batch snapshot goes stale the moment operation one lands. A post-write read-back does not close this either — it only confirms what this batch wrote, never what someone else did. The read before operation N doubles as the read-back for operation N-1, so this is one read per item, not two — plus one final read after the LAST operation, which has no successor to verify it. Skipping that one lets a provider silently drop the last mutation while the ledger reports the whole batch applied. Any drift stops the batch there and re-previews the rest. A provider that silently ignores the first update must stop the batch there, per the grouped contract's fail-fast ledger; a single trailing read would record the rest as applied anyway. Four items should not cost four rounds. List deletes keep `batch-safety.md` unchanged. One list per mutation; verify by reading back, not by service success alone.
