# Images and videos in /config/www

On demand from `SKILL.md` when the user wants to see or clean up pictures or videos in `/config/www` (dashboards show them at `/local/<path>`). The Relay never reads or writes there; it lists, deletes images and videos (`.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`, `.mp4`) after keeping a copy, and puts a copy back. `/config/www/community` belongs to HACS: never touch it.

## List

`ha-nova relay files` with `{"action":"list_dir","path":"/config/www/<folder>"}`: `name`, `type`, `size`, `modified`. `truncated: true` means more than 500 entries exist and the rest were not seen: say so, never claim the list is complete. Show names, sizes, and dates; never the picture itself (the content stays in Home Assistant). Before offering a cleanup, add up the sizes: at most 20 files per plan, and all copies together at most 100 MiB; say up front what doesn't fit.

## Delete

Before a delete, check that no dashboard or entity picture uses the file (`/local/<path>`): `ha-nova relay ws` `lovelace/config` per dashboard, and the states' `entity_picture`. Name what uses it in the preview.

One `ha-nova plan raw` item per file, at most 20 per plan: `{"endpoint":"files","payload":{"action":"delete_file","path":"/config/www/<file>"}}`. No backups save first: the App keeps the copy itself. Every plan asks like an unlock (code or phone). Follow the plan's `say` and `next_step`; the apply's `next_step` holds the undo item for each file.

Limits: 25 MiB per file, 100 MiB for all copies, each copy kept 30 days. A plan refused for size or a full store: tell the user they can wait until copies expire, put one back, or delete the file themselves in Home Assistant's File editor (no undo there). Folders and other file types are not deleted; empty folders stay.

## Undo

"Undo" or "put it back": `ha-nova relay files` with `{"action":"list_deleted"}` lists `copy`, `path`, `deleted_at`, `kept_until`. Plan `{"endpoint":"files","payload":{"action":"restore_file","path":<path>,"copy":<copy>}}`; it asks like a delete, because the file becomes reachable at `/local/<path>` again.

- `COPY_NOT_FOUND` or not listed: the copy expired (30 days) or was put back already; a Home Assistant backup from before the delete still holds `www`.
- `FILE_EXISTS`: a file has that name again; the user renames or removes it first. Never overwrite.
- `FILE_NOT_FOUND`: its folder is gone; the user recreates it (the Relay creates no folders in `www`).
- `FILE_CHANGED`: the file changed during the delete; nothing was deleted. Plan again.
- `FILE_ACCESS_READONLY`: file access is set to `read`; listing works, deleting and putting back don't (`SKILL.md` → Bootstrap).
- `FILE_NOT_PERMITTED` or `UNDO_STORE_UNAVAILABLE`: this Relay can't change the file or keep a copy (often the standalone container); nothing changed. The user deletes it in Home Assistant's File editor.
- `FILE_NOT_REGULAR`: a link or a file with several names; HA NOVA doesn't delete it.
