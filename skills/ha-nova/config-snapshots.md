# Config Snapshots (shared reference)

SSOT for the targeted, lightweight snapshot layer on top of the relay's generic
blob store (`POST /backups`). Load this file only when capturing
or restoring a snapshot. The relay stores opaque gzip'd JSON — everything smart
(what to capture, how to restore, what to promise) lives here.

## Relay contract

`ha-nova relay backups --data-file <payload-file>` (file-based, like ws/files).

- Save: `{"action":"save","category":"<family>","name":"<slug>","data":<full read-back JSON>}` → `{file, bytes, created_at}`
- Load: `{"action":"load","file":"<category>/<name>-<stamp>.json.gz"}` → `{data, created_at}`
- List: `{"action":"list"}` (optional `"category"`) → newest first
- Delete: `{"action":"delete","file":...}` — typed confirmation code like any destructive op
- Prune: `{"action":"prune"}` (defaults: 30 days / 100 files, named snapshots exempt)

Names: auto-snapshots MUST use the `auto-` prefix (prune eats them); snapshots
the user asked for by name use a plain slug and survive prune. A `404/NOT_FOUND`
from `/backups` means the relay predates the snapshot store — fall back to the
safety-backup offer instead of failing the task, and surface the relay-outdated
warning when it appears.

## Categories (one per family)

Wired for auto-capture today: `automations`, `scripts`, `scenes`,
`dashboards`, `helpers`. Reserved for the next wiring step (their skills keep
the safety-backup offer until then): `energy`, `metadata` (entity/device
registry fields), `yaml` (file contents) — the fidelity table below already
covers them so restores work the moment capture lands.

## Capture (before destructive ops)

Before a delete, and before a full-document save that REMOVES content
(dropped views, cards, or scene members — routine edits are covered by the
revert stack and read-back verification instead), save the
COMPLETE HA-normalized read-back of each affected item (the same read the
preview was built from — never the draft). One item per snapshot; a batch
saves one snapshot per item. Name: `auto-<item-slug>`, where `<item-slug>` is
the item's id made relay-safe — the store accepts only `[a-z0-9-]`, and HA ids
carry underscores and dots (`automation.kitchen_lights`): lowercase, replace
every other character with `-`, collapse repeats, trim leading/trailing `-`,
and truncate so the full name stays within 64 characters. Say in the result
that a snapshot was taken and how to restore ("restore from snapshot").

Capture is best-effort: a failed save (store full, relay outdated) must WARN
and continue the confirmed operation — the typed confirmation stays the
authority; a snapshot failure never silently blocks or silently vanishes.

## Restore (always through the family's own write path)

1. `list` → resolve the snapshot (ask when ambiguous; show age + name).
2. `load` → treat `data` as the restore draft.
3. Diff against the LIVE state (`ha-nova diff` where the family supports it),
   preview, and confirm — a restore is a normal write, never a blind put.
4. Write IN PLACE via the family's own API — never delete+recreate.
   A deleted item restores as a create that reuses the original identity key
   where the API allows it (see the fidelity table).
5. Read back and verify like any write of that family.

## Restore fidelity (say this in the preview — never overpromise)

| Family | Restore | Identity |
|---|---|---|
| automations / scripts | full | config-API upsert keyed by the original `unique_id` — entity_id survives |
| scenes | full | upsert by original `id`; entity derives from `name` |
| dashboards (content) | full | `lovelace/config/save` by `url_path` while the dashboard exists |
| dashboards (deleted) | partial | recreate via `lovelace/dashboards/create` reusing the previewed `url_path`/title, then `config/save` the snapshot — content returns fully, the internal `dashboard_id` is new |
| energy prefs | full | whole-document `save_prefs` |
| yaml files | full | path-stable `write_file` |
| helpers (storage) | update: full / delete: partial | recreate mints a NEW id — inbound references stay broken; say so |
| metadata (entity/device fields) | full in place | keyed by entity_id/device id; a deleted registry entry is NOT recreatable |

A snapshot restores the ITEM, never the reference graph: when the original op
had consumers (search/related findings), the restore preview repeats that
consumers may still point at nothing. NOT covered (never offer): config-entry
helpers, areas/floors/labels/categories, users, recorder data, update installs,
backup archives — those keep their family's own recovery story.

## Relation to other mechanisms

- `revert` (update-revert stack) stays the quick-undo for the LAST verified
  update; snapshots are the durable, named, delete-covering layer. Offer
  `revert` first when it applies.
- Full HA Backups stay the recovery net for system-level operations
  (core/OS updates, recorder purges) — never replaced by snapshots.
- User-facing name: "config snapshot" (localized); never call it a backup —
  that word stays reserved for full HA Backups.
