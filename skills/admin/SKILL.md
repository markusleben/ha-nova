---
name: admin
description: Use when managing Home Assistant people, zones, tags, and user accounts — creating a person, drawing a zone, adding a tag, or reviewing who has access — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Admin

## Scope

The people-and-places layer of Home Assistant:
- persons (and which device trackers follow them)
- zones (home, work, school — the geofences presence automations depend on)
- tags (NFC/QR tags)
- user accounts: list, and the account lifecycle (create/delete) with hard guards

Not in scope: areas, floors, labels, categories (`ha-nova:organize`), automations that USE presence (`ha-nova:write`), or authentication providers and passwords — those stay in the Home Assistant UI on purpose.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

- `ha-nova relay ws --data-file <payload-file>` for every command here
- `--out <result-file>` for large listings

## Flow

1. **Persons**: WS `person/list` -> `person/create` / `person/update` / `person/delete`.
   - a person links `device_trackers` (which follow them) and optionally a `user_id` (their login)
   - update resends every mutable field (read first, or you drop their trackers), addressed by `person_id` — never put `id` in the body, Home Assistant rejects it. Zone and tag updates work the same way (`zone_id` / `tag_id`).
2. **Zones**: WS `zone/list` -> `zone/create` / `zone/update` / `zone/delete`.
   - a zone is `name`, `latitude`, `longitude`, `radius` (metres), `icon`, `passive`
   - **Zones are presence infrastructure.** Changing a radius or deleting a zone silently changes when every presence automation fires. Before any zone change, run WS `search/related` with `{"item_type":"entity","item_id":"zone.<id>"}` (there is no `zone` item type) and name the automations that depend on it — as an advisory, in the preview.
   - the `home` zone is special: it comes from core config, not the zone registry. Changing home means changing the Home Assistant location (`homeassistant.reload_core_config` territory) — say so instead of trying to delete it.
3. **Tags**: WS `tag/list` -> `tag/create` / `tag/update` / `tag/delete`. A tag's `tag_id` is what the NFC/QR tag physically carries; scanning fires an event automations listen for. Deleting a tag breaks those automations, not the sticker.
4. **Users**: WS `config/auth/list` shows accounts (`id`, `name`, `username`, `is_owner`, `is_active`, `system_generated`, `group_ids`).
   - listing and reviewing access is the safe, common case
   - `config/auth/create` / `config/auth/delete` exist, and are the most dangerous writes in HA NOVA
   - before any delete: WS `auth/current_user` returns the account this relay's token belongs to — that account is never deletable
5. Verify every write by re-reading the list — never from the command response alone.

## User Safety (read before touching accounts)

- **Never delete the owner** (`is_owner: true`). Home Assistant would be left without an administrator. Refuse and explain.
- **Never delete a `system_generated` account** — those belong to integrations, not people.
- **Never delete the account whose token this relay uses.** Identify it first with WS `auth/current_user`; if that call fails, do not delete any account.
- Deleting a user does not delete their person entry, their history, or their long-lived tokens' effects — say so.
- Passwords and authentication providers are deliberately out of scope: send the user to the Home Assistant UI.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Admin specifics:
- `config/auth/*` requires an owner-level token: a permission error here means the LLAT belongs to a non-admin account, not that the command is wrong.
- Deleting a zone that automations reference succeeds — Home Assistant does not stop you. The damage shows up later, which is exactly why the impact advisory runs before the write.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there.

Render the Report shape (output-rules.md); person/zone/user inventories render the List Frame. For persons: name, device trackers, linked user. For zones: name, radius, and which automations depend on them. For users: name, whether they are owner/active/system-generated — never their tokens or credentials. State what was verified after the write.

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

- Zone and person deletes take the typed confirmation code, and the preview must first name the automations that depend on them (`search/related`).
- User deletion is the strictest operation in HA NOVA: owner, system-generated, and the relay's own account are refused outright, and everything else needs the typed confirmation code plus a plain statement of what is lost.
- Creating a user account grants durable system access, so it takes the typed confirmation code too — not the ordinary create tier. The preview names the login name, whether the account is an administrator (`group_ids`), and that the password is set in the Home Assistant UI, never here.
- No delete here has a `revert`. Zones, persons, and tags are recreatable from their previewed fields — a tag keeps its physical `tag_id`, but other recreates mint new internal ids, so inbound references stay broken. A deleted user's password, tokens, and history are unrecoverable; the delete preview must say so.
- Offer a safety backup via `ha-nova:backup` before zone, person, and user deletes (not for tag deletes or routine updates).
- Never surface credentials, tokens, or password state in output.

## Guardrails

- One person, zone, tag, or user per operation.
- Updates resend every mutable field, addressed by the `*_id` key — never `id` in the body; read before write, always.
- Never guess a `person_id`, `zone_id`, `tag_id`, or user `id`.
