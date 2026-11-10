# Images and videos in /config/www: list, delete with a kept copy, restore

Status: active

Date: 2026-11-10. Issue MAR-128 (Markus, 09.11.: "Warum ist der denn gesperrt? Das macht doch keinen Sinn" → "Wie oben, aber erst in 0.30"). Owner: Relay `/files` (`nova/src/http/handlers/files*.ts`, new `files-www.ts`), raw plans (`cli/plan_raw_files.go`, `plan_raw.go`, `plan_raw_tier.go`, `plan_raw_say.go`, `plan_raw_apply.go`), the gate worker (`cli/gate_verdict.go`). Target: 0.30 (release/0.30). High-risk: a new delete path in a folder every browser is served from, and a binary copy store in the App. The App changes.

## Today

- `www` is a denied segment in `files-paths.ts`: list, read, write, and delete under `/config/www` answer `FILE_PATH_DENIED`, so a user can't even clean up old camera snapshots there.
- `delete_file` outside `www` checks the write extension list (`.yaml` … `.md`), so a `.jpg` is refused anyway (`FILE_TYPE_DENIED`).
- A raw plan's file delete needs a backups save of the file's text first (`fileKept`, `plan_raw_files.go`). Reads are UTF-8 only and at most 1 MiB, so a binary file can never get that copy, and the config snapshot store is 50 MiB in total. Binary media needs its own undo.
- The gate worker rates every `files delete_file` typed (`file_delete`) and every `write_file` typed (`file_write`). Both stay.

## Rules (scope, Astra-checked on MAR-128)

1. Writes to `/config/www` stay denied: no `write_file` there, ever.
2. Listing and metadata are allowed. File content is not served: `read_file` in `www` stays denied (see "Not in this change").
3. Delete only regular files with a media extension: `.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`, `.mp4` (case-insensitive). Never a folder, never recursive, never a symlink. Typed confirmation (code or phone), as every file delete today.
4. Before the delete, the Relay keeps a binary copy outside `www`, in its own data folder, with a per-file cap, a total quota, and a retention period. When the copy can't be kept (too large, store full, any error), nothing is deleted.
5. Restore only from that copy, only to the path it was deleted from, never over an existing file.
6. `/config/www/community` (HACS's frontend cards) stays fully denied.

## Threat model

The caller is an agent that may be prompt-injected; it can send any `/files` body. Writers to the config folder itself (Home Assistant, the File editor, Samba, the user) are trusted, as for every `/files` action today: one of them swapping a folder for a link in the microseconds between the Relay's checks and its file call is out of scope. Bind mounts inside `/config` need host root; they are handled by identity where it is cheap (below), not promised in general.

## Relay contract (`POST /files`)

**The www area.** A target is in the www area when its logical path's first segment under `/config` is `www`, or when its real location (realpath of the target, or of its parent for a delete or restore) lies inside the real `<root>/www` folder, compared by device and inode up the folder chain (so a symlink or a bind mount of `www` under another name counts too). `<root>/www/community` is denied the same two ways: a second segment `community` (case-insensitive, so a case-insensitive mount can't alias it), or any folder on the real chain with its device and inode. Every other deny rule still applies inside the area (`.git`, `.ssh`, `secrets*`, `*.log*`, the Relay's data folder by identity). A nested `www` segment inside the area is not a new deny.

| Action | In the www area | Mode |
|---|---|---|
| `list_dir` | allowed | read, readwrite |
| `read_file` | `FILE_PATH_DENIED` (unchanged) | |
| `write_file` | `FILE_PATH_DENIED` (unchanged) | |
| `delete_file` | media files only, copy first | readwrite |
| `restore_file` (new) | from a kept copy only | readwrite |
| `list_deleted` (new) | lists the kept copies | read, readwrite |

`list_dir` entries gain `modified` (the entry's mtime, ISO; `null` for a link) next to `name`, `type`, `size`, everywhere; still at most 500 entries (`truncated`). Metadata only, so "delete the pictures older than a month" can be answered; not a drift basis. A `www` segment outside the area (`/config/foo/www`) stays denied.

**`delete_file` in the www area.** Every store action (delete, restore, `list_deleted`, and the prune each one runs first) holds one lock per store, so nothing ever sees a copy half written.

1. Resolve as today for a delete (parent by realpath, leaf not followed). `lstat` the leaf: a directory is `FILE_IS_DIRECTORY`; anything but a regular file with one link (a symlink, a hard-linked file) is `FILE_NOT_REGULAR`; a name without a media extension is `FILE_TYPE_DENIED`; above 25 MiB is `FILE_TOO_LARGE`.
2. Prune expired copies. If the copies' total plus this file passes 100 MiB: `UNDO_STORE_FULL` (409), nothing deleted. Its message says what to do: put a copy back or wait until copies expire (dates via `list_deleted`), or delete the file in Home Assistant's File editor.
3. Open the file without following a link, check the opened inode is the one `lstat` saw, and read exactly its size into memory (bounded: at most 25 MiB; a file that grew is `FILE_CHANGED`, 409). Write `<id>.bin` (temp name, fsync, rename), then `<id>.json` (path, size, mode, SHA-256, deletion time; temp, fsync, rename), then fsync the store folder. The copy files are `0600`.
4. Take the file out of `www`: rename the name to a hidden `.nova-delete-<id>` in the same folder (one atomic step that takes whatever is at the name now), check that it is the copied inode, unchanged in size and mtime, then unlink it. Anything else (it was replaced or written meanwhile) links it back under its name (never over a new file), removes the copy, and answers `FILE_CHANGED`, nothing deleted.
5. Answer `{deleted: true, copy: "<32 hex>", kept_until: "<ISO>"}`.

An error before step 4 removes the copy and deletes nothing. So nothing leaves `www` unless its exact bytes are durably in the store.

Outside the www area `delete_file` is unchanged: write extensions only, no copy.

**`restore_file`** `{action, path, copy}`:

1. `copy` matches `^[0-9a-f]{32}$` and is kept, else `COPY_NOT_FOUND` (404). Its record's path must equal the request's path after the Relay's own decode and normalization, and its bytes must hash to the record, else `COPY_INVALID` (400), nothing written.
2. The path resolves as a delete does (parent by realpath, leaf not followed), lies in the www area, outside `community`, and has a media extension. The parent folder must exist (`FILE_NOT_FOUND`; the Relay creates no folders in `www`).
3. Create the file exclusively (`O_CREAT|O_EXCL`: an existing entry, a link included, is `FILE_EXISTS`, 409, never replaced), write the bytes, set the recorded mode, fsync it and its folder. An error after the create removes the new file.
4. Only then remove the copy. Answer `{restored: true}`.

**`list_deleted`** `{action}` (no path): answers `{copies: [{copy, path, bytes, deleted_at, kept_until}], used_bytes, limit_bytes, file_limit_bytes}`, copies newest first.

**Without a data folder** (a Relay built without one) a delete, restore, or list in `www` answers `UNDO_STORE_UNAVAILABLE` and changes nothing. A Relay that may not rename or create the file (another owner) answers `FILE_NOT_PERMITTED`, nothing changed.

**The store.** `<data>/www-undo/` (`/data` in the App), a real folder (not a link): `<id>.bin` and `<id>.json` only; only regular files count. A copy is restorable for 30 days; per file at most 25 MiB, 100 MiB in total; constants in code, no option. Expiry is lazy: an expired copy can't be restored and is removed on the next delete, restore, or `list_deleted` (no timer). A `.bin` without its record is a commit that never finished (the original was never taken out of `www`), so the prune removes it, and a record without its `.bin`, too. The data folder is already never reachable through `/files` (`assertOutsideData`). The App excludes the store from Home Assistant backups (`backup_exclude`): a deleted picture should not live on in every backup.

## The gate

The worker rates the exact bytes, as for every request:

- `delete_file`: typed (`file_delete`), unchanged, so a delete in `www` runs only with a person's approval (code or phone).
- `restore_file`: typed (`file_restore`, its own words: it puts the file back where anyone with its address can open it, since `www` is served without sign-in at `/local/`). Astra and the KISS review both asked for typed; a restore is rare, so the extra ask costs little.
- `list_deleted`: a read, as `list_dir`.
- The worker recognizes only the six `files` actions; any other is refused. Today every `files` action is recognized and an unknown one rates preview, so a new Relay action could pass a worker that doesn't know it. The worker ships inside the App image with the Relay, so a matching pair is the only one that runs; the strict list keeps it fail closed anyway.

## CLI (raw plans)

- `plan raw` accepts `restore_file`; `list_deleted` is a read (`ha-nova relay files`, `ha_read`). Chat-app levels don't change: `ha_read` and raw plans stay Manage everything only (`mcp_serve_levels.go`). No MCP tool text changes (`TestMCPToolTextStable`): the apply's `next_step` hands over the exact restore item.
- A delete in the www area (by its logical path): no backups save is asked for (the Relay keeps the copy) and nothing is read as text. A name without a media extension is refused at plan time with words that say which files HA NOVA deletes there. The plan reads `list_deleted` once: an older App answers `VALIDATION_ERROR`, and the plan refuses with "update the NOVA App" before anyone types a code. Before anyone confirms, the plan also lists the file's folder: a file that isn't listed (in a listing that isn't cut at 500), isn't a plain file, is above the per-file limit, or doesn't fit the copies' room (`used_bytes`, `limit_bytes`) is refused with its next step. The Relay checks all of it again at send time. `fileUnchanged` and `copyNeeded` skip a www delete or restore; the "a plan changes the file once" check stays.
- `restore_file`: the plan needs the copy in `list_deleted` with exactly that path, else it refuses. That the path is free is the Relay's check (`FILE_EXISTS`).
- Wording: the delete reads as today ("delete the file /config/www/…"); a restore uses a new verb `restore` (`raw.verb.restore`, en and de).
- After a done delete in `www`, the apply's `next_step` names the copy, until when it is kept, and the undo: a `plan raw` with `{"action":"restore_file","path":…,"copy":…}`.
- `copyNeeded` / `--no-snapshot` don't ask for a copy for a www media delete, and the chat-app step (`mcp_steps.go`) doesn't tell the agent to read and save it first.
- The user's `say` after the apply adds that a copy is kept for 30 days.

## Skills and docs

- `skills/yaml-config/SKILL.md`: the "relay refuses www" line becomes a pointer to a reference file `skills/yaml-config/www-media.md` (list, delete via `plan raw`, undo, store full, no content read). The word budget isn't raised.
- `skills/ha-nova/relay-api.md`: the `/files` line names the two new actions and the limits; `skills/fallback/relay-ready.md` (the unmatched-request route) points at `www-media.md`. The router (`skills/ha-nova/SKILL.md`) gets no row: its route budget has no room, and fallback reaches the reference.
- `docs/reference/safety.md` (a row), `bridge-architecture.md`, `ha-api-matrix.md`, `skills/hacs/SKILL.md` (`www/community`), `nova/DOCS.md` (file access row; uninstalling the App or restoring a backup drops the copies), `docs/work/next-release-body.md` and `.goreleaser.yml` (the "refuses a file" example becomes `custom_components`; one new line in the draft), `nova/config.yaml` (`backup_exclude`).
- Lanes that pinned `www` as denied move to `custom_components`: `chat-files.mjs` (bed and VM lane), `verify.sh`, `relay_refusal_test.go`.

## Tests

- Vitest (`tests/http/files-www.test.ts`): list in `www`; read and write in `www` denied; a delete of each media extension keeps a copy (bytes, record) and answers its id; non-media, folder, symlink, hard link, too large refused with nothing changed; `community` refused by name, by `Community`, by `www/./community`, by an encoded slash, by a symlink in `www` that points into it, and by a symlink outside `www` that points into it (list and delete); a symlink outside `www` that points into `www` gets the www rules; store full → `UNDO_STORE_FULL`, nothing deleted; expired copies pruned and not restorable; a file replaced between copy and take-out keeps the new one and drops the copy; restore puts back bytes and mode, removes the copy; restore over an existing file or link refused; restore with another path or a corrupt copy refused; read-only mode refuses delete and restore; `list_deleted` waits for a running delete.
- Go: the gate rates a www delete and a restore typed, `list_deleted` a read, an unknown `files` action refused; `plan raw` for a www media delete needs no backups save and is typed; a non-media name refused at plan time; an old App (no `list_deleted`) refused at plan time; a restore plan refuses without a listed copy.
- Test bed (`scripts/e2e/testbed/www-media.mjs`, bed with `TESTBED_FILES=1`, fake phones): list `www`; a delete is held until the phone approves, then lands and its copy is listed; a restore is held, approved, and puts it back; filling the store (four deletes of 25 MiB) makes the fifth answer `UNDO_STORE_FULL` with the file still there; `community` refused for list and delete. `verify.sh`'s "a file delete in www … FILE_PATH_DENIED" row becomes a `community` delete.

## Not in this change

- **Image content to the AI.** `read_file` in `www` stays denied. Showing a picture to the model needs its own step: an explicit request by the user, a notice that the picture goes to their AI provider, and a binary answer through the MCP tools. A separate follow-up, so this change adds no content path at all (Astra agrees: cleanup needs metadata, not pictures).
- `/media` (not mounted in the App; a separate extension).
- Writing or uploading into `www`; deleting folders; deleting other types (`.js`, `.css`, `.html`: code the browser runs).
- Emptying the copy store by hand (it would be an irreversible delete; copies expire after 30 days).

## Review (before build)

- Astra (gpt-6-astra, medium): agrees with changes. Taken: restore typed; one lock for every store action, `list_deleted` included; durable commit order (copy, record, folder fsync) before the file leaves `www`, and restore fsyncs before it consumes the copy; a bounded read; lazy expiry worded honestly; `community` and `www` also by device and inode (bind mounts, links from outside); hard-linked files refused; the take-out by rename and inode check, so a file replaced at the last moment is never deleted without its copy; the worker refuses unknown `files` actions; no plan basis from a truncated `list_dir`. Not taken as a promise: folder swaps by trusted local writers between checks (threat model above).
- Fable (fable-5-2, medium): agrees with changes, no P1. Taken: the sibling paths (chat-app step, `ha_read`, confirmed-actions table, lanes and docs that pinned `www` as denied, release-notes example); `file_restore` words; plan-time refusals for a full store, a missing, non-plain, or too large file (the person never approves something the Relay then refuses); `modified` in `list_dir` (reversing that KISS point: cleanup by age needs it); `UNDO_STORE_UNAVAILABLE` and `FILE_NOT_PERMITTED`; the reference file's next steps per refusal; `say` names the copy. Not taken: a router row (route budget; fallback reaches the reference), MCP tool text changes (the apply's `next_step` carries the restore item), a plan-time read-only check (an existing gap for every file write), decoding encoded paths in the CLI (an encoded www path fails closed at the text read).
- KISS (sonnet-5-5): agrees with changes. Taken: no 200-copy cap; no owner restore (mode only); no list-based drift basis (the Relay checks the file at send time); the restore plan checks only the copy; `COPY_INVALID` for both mismatch and corruption. Not taken: dropping `list_deleted` (the undo must work in a later session, and it doubles as the plan's App-version probe); dropping the bed's store-full row (the Captain asked for it).

## Review (before PR)

- Fail-open (sonnet-5-5): a compile error (fixed); a failure after the take-out's rename could drop the copy while the file sat under its hidden name (fixed: anything after the rename puts the file back first, and the copy is dropped only once it is back). Not taken: `backup_exclude`'s glob is checked on the throwaway VM before the App promote, not in a lane; a link inside `www` to another folder gets the www rules (trusted writers only, threat model).
- UX and sibling paths (sonnet-5-5): the `ha_read` test text, the room check over the whole plan (fixed: summed per plan), the undo items also after a stopped apply, `docs/releasing.md`'s VM-lane line, the reference file's next steps per refusal and the plan's limits up front, the German verb ("wiederherstellen"), `list_deleted` creating the store in read mode (fixed: it creates nothing), and a chat-app lane row for a www delete (added to `chat-files.mjs`).
