# Omission checks for raw writes that replace a whole value

Status: active

Date: 2026-11-03. Issue #926 (follow-up to the scene guard, #931). Owner: raw plans (`cli/plan_raw*.go`) and `ha-nova relay core|ws` (`cli/relay.go`). Target: 0.28.1 (App 0.13.16, placeholder).

## Problem

Several allowlisted raw writes (`cli/relay_raw_allow.go`) replace a whole value. A model that skipped the owning skill can leave content out of the body and drop it without noticing: no basis, no omission check, no copy. #931 guards scene saves only.

## Pattern (the scene guard, `cli/plan_raw_scene.go`)

For each guarded write:

1. **Basis.** Read the live value at planning and at apply's recompute, and put it in the plan's basis: a change since the preview is `PLAN_DRIFT`. Read it again right before the write is sent, so a change while an earlier step ran is caught too. A value that can't be read is an error: nothing is planned or sent over unseen content.
2. **Dropped.** Compare the body with the live value. When it leaves out content, refuse (`REFUSED`) unless the same plan saves a copy of exactly the live value first (`backups save`, with no `delete` or `prune` after it). The refusal's `next_step` says how to plan an intended removal (tell the user what goes, then the copy first) and how to keep content left out by mistake (read it, send it all). A live value that holds a value HA NOVA hides (`holdsHiddenValue`) is refused outright: a copy never keeps a hidden value (`guardRaw`), so none could bring it back.
3. **One write per value.** Each write is checked against the live value, not the one before it, so a plan writes each value once.
4. **The copy can't be skipped.** When a needed copy fails, apply answers `SNAPSHOT_REQUIRED`, and `--no-snapshot` is refused (`NO_SNAPSHOT_NOT_OFFERED`).
5. **`ha-nova relay core|ws`.** One request can't carry a copy first, so a write there that drops content is refused (exit 4) with the plan that keeps a copy.
6. **Preview.** A write after its copy names what it drops, in the plan's language (a confirm string Markus approves).

## Routes

Verified against Home Assistant `dev` on 2026-11-03.

| Route | What Home Assistant does with a left-out part | "Dropped" | Copy or refuse | Preview line | PR |
|---|---|---|---|---|---|
| `input_select/update` | `_update_data` returns `{id} \| update`: the whole helper is replaced. If the current option goes, the helper keeps it as its current option, so its state reads `unknown` until an option is picked (or the first one after a restart). | Every live option not in the body's `options` as the same text. An option sent as a number counts as left out: the Relay re-encodes it (`1.0` becomes `1`) before Home Assistant stores its text. | Copy: `backups save`, category `helpers`, data = the helper's item from `input_select/list`, unchanged. | New: names the options. | 1 |
| `schedule/update` | Replaced the same way; the WS schema fills a left-out weekday with `[]`. | Every live time block with no block of the same weekday, start, and end in the body, and every block whose `data` loses a key or changes a value. Times compare as `HH:MM:SS` (`8:00` = `08:00:00`). A moved or resized block counts as dropped; data added to a block, or a key added to its data, doesn't. | Copy, as above (`schedule/list`). | New: names weekday and times. | 1 |
| `calendar/event/update` | Not a replacement. The only core calendar with event updates is `local_calendar`, and the event store of its pinned iCalendar library (14.3.1) merges the fields sent (`model_dump(exclude_unset=True)`): a left-out `rrule`, `description`, or `location` stays. Checked with that library version for a whole-series and a this-and-future edit. | Nothing. | No guard. | None. | — |
| `person/update` | Top-level merge; `device_trackers`, when sent, replaces the list. | Live trackers missing from a sent `device_trackers`. | Copy (new `persons` category in `config-snapshots.md`). | New. | 2 |
| `config/entity_registry/update` | `aliases` and `labels`, when sent, replace the lists; `options_domain` + `options` replace that domain's options. | Live aliases and labels missing from a sent list; live option keys missing from the sent `options`. | Copy: `metadata` (already captures entity fields). | New. | 2 |
| `config/device_registry/update` | `labels`, when sent, replaces the list. | Live labels missing from the sent list. | Copy: `metadata`. | New. | 2 |
| `config/area_registry/update`, `config/floor_registry/update` | `aliases` (and an area's `labels`), when sent, replace the lists. | Live entries missing from a sent list. | Copy: `metadata`, the row from its list (PR 2 changed this from preview-only: one rule for every registry, and the row restores in place). | New. | 2 |
| `assist_pipeline/pipeline/update` | `Pipeline(id=item.id, **update)`: every field left out goes back to its default. Eleven fields are required, so Home Assistant refuses their omission itself. | `prefer_local_intents` or `acknowledge_media_id` held (not false or null) and left out. | Copy (new `assist` category). A guard of about 50 lines exists on #931's branch, ready to reuse. | New. | 2 |
| `files write_file` on an existing YAML file | The file is replaced; only the Relay's one-step `.bak` remains. | Not diffed: YAML text has no stable item identity. Every overwrite of an existing file counts. | Copy: `yaml` (`{path, content}`), as `config-snapshots.md` already asks. | The file line exists; no new line. | 3 |
| Other helper updates (`input_number`, `input_text`, `input_datetime`, `input_boolean`, `input_button`, `counter`, `timer`) | Replace the whole item (`{id} \| update`, verified 2026-11-07). | Optional fields left out (icon, unit, initial) go back to their defaults: scalars. | No guard: apply saves an update-revert checkpoint for every helper update. | None. | — |
| `zone/update`, `tag/update`, `lovelace/dashboards/update` | Merge (`{**item, **update}`, verified 2026-11-07). | Nothing. | No guard. | None. | — |
| `backup/config/update` | `create_backup.agent_ids`, when sent, replaces the list of backup locations. | Live locations missing from a sent list. | Not a snapshot family: a known limit. | None. | — |
| `energy/save_prefs` | Whole document. | Already guarded: basis plus the `energy_emptied` reason. | — | — | — |

## PR 1 (this branch)

`input_select` options and `schedule` time blocks, in `cli/plan_raw_helper.go`, wired where the scene guard is: `computeRawPlan` (planning and recompute), `executeRaw` (the re-read before the update, the copy that can't be skipped), and `ha-nova relay ws` (`relayKept`, which checks the body and a `collect_events` envelope's `message`, as the raw guard does). The copy check (`copySaved`), the copy's name (`autoName`), and the copy-required texts are shared with the scene guard. The helper's live item enters the basis as `helpers` (`<collection>/<id>`). The existing update-revert read (`rawHelperItem`) uses the same read. An update of a helper its list doesn't hold is `NOT_FOUND`, naming where its id comes from (an entity_id is the usual mistake): at planning for these two helpers, and at apply's undo read for every other helper (it answered `UNDO_FAILED` and offered `--no-snapshot` before).

An update after its copy names what it removes in the preview (`helperNote`), with the strings Markus approved on 2026-11-03: `raw.options_drops` (the options, by name), `raw.schedule_drops` with `raw.schedule_block` or `raw.schedule_block_data` (a block whose data alone goes), and `raw.day.*`; times show as HH:MM.

Calendar recurrence is out: see the table. The calendar skill's "omit `rrule` to clear recurrence" (`skills/calendar/SKILL.md`, Create, update, or delete step 3) doesn't hold for `local_calendar`, which keeps the rule. That fix is separate.

## PR 2

Persons, registry rows, and voice assistants, in the same guard as PR 1: `rawReplaced` (`cli/plan_raw_helper.go`) maps each guarded WS update type to how its live item is read, what it drops, and its copy's category; the new routes live in `cli/plan_raw_replace.go`. A registry update that sends no list it could drop from reads nothing for it. The live item enters the basis under `helpers` (`<type without /update>/<id>`). An update of a person, device, area, floor, or voice assistant its list doesn't hold is `NOT_FOUND` at planning; an entity whose registry entry can't be read plans nothing. List entries compare as text (aliases trimmed, tracker ids lowercased, as Home Assistant stores them); a list sent as null drops nothing. After its copy, the preview names what goes with one new string, `raw.omits` ("Removes {items}." / "Entfernt {items}."). `ha-nova relay ws` refuses a dropping update as `refused raw <type>`.

Each person update now reads `person/list`, so an unreadable list plans nothing (it planned typed before), and an unknown person is `NOT_FOUND` (`ha-nova relay ws` still refuses it as typed first).

## PR 3

YAML file overwrites in raw plans (`cli/plan_raw_files.go`): a `files write_file` of a `.yaml`/`.yml` file reads the file at planning, at apply's recompute, and right before the send. An existing file whose content the write changes needs a `backups save` (category `yaml`) of exactly `{path, content}` earlier in the plan, with `path` as the write names it; a new file, or a write of the same content, needs none. The config check's rollback (the `.bak` written back) needs the copy too, since a `.bak` may be older than the file; the CLI's next step says so. A copy under another category doesn't count. The basis holds the content's SHA-256 under `files`, so an edit since the preview stops the write (the copy no longer matches). A file the Relay can't read (file access off) plans nothing. The ESPHome folder is included: its skill captures the same copy. `ha-nova relay files write_file` is not checked: the computer flow saves the copy and checks drift as two requests (`skills/yaml-config` step 3), and one request can't prove a copy. That is a known limit, as are: a `files delete_file` of a YAML file is not checked here (the chat-app step asks for its copy first; follow-up), two writes to one file under different spellings (percent-encoded) both plan and the second is refused at apply after the first was sent, and an empty file needs a copy too.

## Known limits (PR 1)

- An `icon` or an `input_select`'s `initial` left out is not counted (PR 2 decides the scalar fields).
- A `backups prune` after the update can still take the copy (shared with the scene guard): `copySaved` checks only the items between the copy and the write.
- The window, phone, and form ask for an apply without the copy don't check `copyNeeded` (shared with the scene guard): such an apply is offered, then refused with nothing sent.
- Home Assistant has no conditional write, so another client can still change the value between the last read and the send. `ha-nova relay ws|core` reads it in its own session before the Relay transport is chosen; apply reads it right before the send.
