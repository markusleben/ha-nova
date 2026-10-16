# Dashboard writes through plan/apply — Spec (2026-10-16)

Status: active

## Problem

- Users ask for whole dashboards and views, installed custom cards, and a
  "what is broken on my dashboards" check. `ha-nova:dashboard` refuses view
  create/delete/reorder and new custom cards, and every save is hand-built
  `lovelace/config/save` prose: the agent merges, diffs, drift-checks,
  snapshots, and verifies by hand.
- `lovelace/config/save` overwrites the whole document. A stale merge basis
  silently reverts an edit made in the Home Assistant UI.

## Home Assistant surface (core 2026.10.1, `homeassistant/components/lovelace/`)

| Command | Payload | Source |
|---|---|---|
| `lovelace/dashboards/list` | `{}` → every dashboard with a config: `id`, `url_path`, `title`, `icon`, `show_in_sidebar`, `require_admin`, `mode` (`storage`/`yaml`) | `dashboard.py` `DashboardsCollectionWebSocket.ws_list_item` |
| `lovelace/config` | `{url_path?}` (omitted = default dashboard) → the config; error `config_not_found` when a storage dashboard has no saved config (new shell, auto-generated default) | `websocket.py` `websocket_lovelace_config`, `_handle_errors` |
| `lovelace/config/save` | `{config, url_path?}`, admin; whole-document replace (`LovelaceStorage.async_save`); a YAML dashboard raises `Not supported` | `websocket.py` `websocket_lovelace_save_config`, `dashboard.py` |
| `lovelace/dashboards/create` | `{url_path, title, icon?, show_in_sidebar?, require_admin?}`; `url_path` needs a hyphen; an existing panel is refused | `const.py` `STORAGE_DASHBOARD_CREATE_FIELDS`, `dashboard.py` `_process_create_data` |
| `lovelace/dashboards/delete` | `{dashboard_id}`; also removes the stored content | collection websocket, `__init__.py` `storage_dashboard_changed` |
| `lovelace/resources` | `{}` → `[{id, type, url}]` (storage and YAML resource mode) | `__init__.py`, `resources.py` |

In 2026.10 the default storage dashboard with content is migrated to a
listed dashboard `url_path: "lovelace"` (`_async_migrate_default_config`);
YAML mode lists it with `mode: yaml`. An unlisted `lovelace` is the legacy/auto-generated default:
addressed by omitting `url_path`.

WS command errors reach the CLI as Relay `502 UPSTREAM_WS_COMMAND_ERROR`
(HA answered with an error: nothing was saved). The Relay forwards any
request/response WS type, so the Relay needs no change; the CLI's read (I1)
and write allowlists grow by exactly these types.

## Behavior

1. `ha-nova plan write --domain dashboard --id <url_path> --file <config.json>`
   plans a full-config save of a listed `mode: storage` dashboard (or the
   unlisted default `lovelace`). An update needs `--base <config_digest>`
   from `get --dashboard`: a live config that differs from the draft's base
   is exit 2 `PLAN_DRIFT` (a stale draft would revert a UI edit).
   `--create --title <t> [--icon <mdi:x>] [--require-admin] [--hide-sidebar]`
   plans a new dashboard shell plus its content; it refuses a listed
   `url_path` (`CONFIG_ID_IN_USE`). A `yaml` dashboard is refused
   (`DASHBOARD_NOT_STORAGE`, read-only). An unknown `url_path` is exit 5
   `NOT_FOUND` with the listed dashboards as candidates.
2. Tier: preview (natural confirmation, `CONFIG_WRITE`). `plan delete
   --domain dashboard --id <url_path>` is typed (`CONFIG_DELETE`, CLI code).
3. Draft checks at plan time: an entity the draft adds that has neither a
   state nor a registry row → exit 4 `UNKNOWN_ENTITY` (never a guessed id).
   Issues ride along in `result.issues` and never block: entities without a
   state, and `custom:` types no installed resource seems to provide (file
   or folder name of each `lovelace/resources` URL; layout-card's view
   layouts by name). The heuristic says "unconfirmed", never "missing".
4. Diff: `diff[]` rows `| Field | before | after |` at view and card level:
   dashboard keys, view added/removed (with card count), view order, view
   keys, and per container (cards, section N, badges, header) added/removed
   cards and changed card keys. Capped at 40 rows plus a count row.
5. Apply (`ha-nova apply`): recompute (exit 2 on any change of the live
   config, the dashboard entry, or the draft); auto config snapshot of the
   live config (Relay `/backups`, category `dashboards`, `auto-<url_path>`;
   a delete stores `{shell, config}`); a fresh re-read right before the save
   (drift → exit 2, nothing saved); one `lovelace/dashboards/create` for a
   create; exactly one `lovelace/config/save` (one `dashboards/delete` for a
   delete); fresh read-back. `verified` = read-back content equals the draft
   (create: and the dashboard is listed; delete: no longer listed).
   `SNAPSHOT_FAILED` saves nothing; `--no-snapshot` only after it. A save,
   delete, or create that Home Assistant answers with an error is reported by
   a fresh read-back, never by the error alone (it swaps its in-memory config
   before the disk write): unchanged → exit 6 `failed`; landed anyway → exit 6
   `unverified` with the way back; unreadable → exit 7. A failure after a new
   shell landed names the dashboard it left empty. Whenever a delete was sent
   with a snapshot, its `restore` is named (automations and scripts too). A delete's `restore`
   names the way back: a `--create` with the shell's title and flags, or for
   the default `lovelace` a plain write.
6. An update lands on the undo stack (domain `dashboard`, target
   `url_path`); `plan revert [--target <url_path> --domain dashboard]`
   restores the before-config and refuses `REVERT_DRIFT` when the dashboard
   changed since.
7. `ha-nova get --dashboards` (sweep: every listed dashboard plus the default)
   and `ha-nova get --dashboard <url_path> [--config]` (one dashboard: views
   with card summaries, custom types with their resource) report the same
   issues and each dashboard's `config_digest`, read-only. The walk covers
   bare-id badges, `header.card`, section settings, nested `cards`/`card`/
   `elements`, `image_entity`; statistic cards are not judged as entities.
   The sweep records an unreadable dashboard (a missing YAML file, a non-object
   config) per dashboard and goes on. YAML dashboards are read like any other.

## Known limits

- No compare-and-swap in Home Assistant: an edit landing between the pre-save
  re-read and the save (milliseconds), or a save from a UI editor that was
  opened before the apply, is overwritten. The skill tells the user to close
  open dashboard editors.
- Card types registered by `frontend: extra_module_url` or bundled under an
  unrelated file name show as unconfirmed.
- Card options are not schema-checked (no schema exists outside the browser);
  the skill reads the card's documentation or existing usage first.
- A create whose shell lands but whose save fails leaves an empty dashboard:
  reported `unverified`; the next step is a plain `plan write`.
- Dashboard writes are not exposed through `ha-nova mcp` yet.
- An apply with an unknown outcome still replaces the target's one undo
  checkpoint (its before-config is the pre-apply config), like automations.

## Delivery

PR 1: CLI (plan/apply/revert/delete, get sweep, allowlists, tests).
PR 2 (stacked): `ha-nova:dashboard` skill rewrite and docs.
