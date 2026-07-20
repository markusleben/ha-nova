# Config Snapshots (shared reference)

SSOT for the targeted, lightweight snapshot layer on top of the relay's generic
blob store (`POST /backups`). Load this file only when capturing, listing,
deleting, or restoring a snapshot. The relay stores opaque gzip'd JSON —
everything smart (what to capture, how to restore, what to promise) lives here.

## Relay contract

`ha-nova relay backups --data-file <payload-file>` (file-based, like ws/files).
Responses use the standard relay envelope (`relay-api.md` → Standard Envelope):
the payload sits under `.data`, errors under `.error`.

- Save: `{"action":"save","category":"<family>","name":"<slug>","data":<full read-back JSON>}` → `.data.file`, `.data.bytes`, `.data.created_at`
- Load: `{"action":"load","file":"<category>/<name>-<stamp>.json.gz"}` → the snapshot under `.data.data`, plus `.data.created_at`
- List: `{"action":"list"}` (optional `"category"`) → `.data[]` newest first (`file`, `category`, `bytes`, `created_at`)
- Delete: `{"action":"delete","file":...}` — typed confirmation code like any destructive op
- Prune: `{"action":"prune"}` (defaults: 30 days / 100 files, named snapshots exempt) → `.data.deleted[]`. Offer prune when a save fails `SNAPSHOT_STORE_FULL` or the user asks; preview count + age range of the affected auto-snapshots, natural confirmation (they are expendable copies), `keep_named` stays true unless the user explicitly says otherwise.

## Batch delete (explicit opt-in)

The `ha-nova` context skill explicitly supports destructive batch deletion for
the `config-snapshots` resource family through
`skills/ha-nova/batch-safety.md`. Single-file delete stays the default.

- Run an unfiltered `list` immediately before building the manifest. Every
  target must be a literal `.data[].file` value with its category, bytes, and
  created time; never expand a category, age, prefix, wildcard, or other
  selector after the preview.
- Snapshot categories may share one manifest: they are metadata on the same
  generic blob resource, with the same endpoint, delete semantics, and
  recovery limits. Never mix another resource type or a prune action into the
  manifest.
- Cap the manifest at **20** files. Use deterministic bytewise file-path order
  and the confirmation code
  `confirm:batch-delete-config-snapshots-<count>-<digest>`.
- Record the dependency result per target as not applicable: deleting a
  snapshot copy cannot change the underlying Home Assistant config. State
  plainly that the deleted copy itself has no rollback.
- Immediately before execution, run another unfiltered `list` and require
  every selected file and its listed metadata to still match the manifest.
  Missing or changed target evidence expires the confirmation; a new
  unselected file does not.
- Execute one `{"action":"delete","file":"<literal-file>"}` request at a
  time in manifest order. After each response, `list` again and verify that
  exact file is absent before continuing. On timeout, verify first and never
  retry blindly. Fail fast and report succeeded, failed, and not attempted.
- When an unselected snapshot exists, pin one in the manifest and verify it is
  still present at the end as the unrelated-resource invariant.

Prune remains a separate retention flow with its own preview and confirmation.
Never use batch delete to emulate prune or to widen `keep_named`.

Names: auto-snapshots MUST use the `auto-` prefix (prune eats them); snapshots
the user asked for by name use a plain slug and survive prune. A `404/NOT_FOUND`
from `/backups` means the relay predates the snapshot store — handle it via
the capture-failure stop below (offer the safety backup), and surface the
relay-outdated warning when it appears.

## Categories (one per family)

All wired for auto-capture: `automations`, `scripts`, `scenes`, `dashboards`,
`helpers`, `energy` (destructive `save_prefs`), `metadata` (entity registry
fields before rename/disable), `yaml` (file overwrites and deletes).

## Capture (before destructive ops)

Capture triggers, per family: every DELETE; full-document saves that REMOVE
content (dropped views, cards, scene members, energy entries); entity renames
and entity/device disables (`metadata` — consumer-breaking even though nothing
is deleted); every YAML file overwrite or delete (`yaml` — the `.bak` holds
only one step). Routine field edits in the other families stay covered by the
revert stack and read-back verification instead. Save the
COMPLETE HA-normalized read-back of each affected item (the same read the
preview was built from — never the draft). One item per snapshot; a batch
saves one snapshot per item. Name: `auto-<item-slug>`, where `<item-slug>` is
the item's id made relay-safe — the store accepts only `[a-z0-9-]`, and HA ids
carry underscores and dots (`automation.kitchen_lights`): lowercase, replace
every other character with `-`, collapse repeats, trim leading/trailing `-`,
and truncate so the full name stays within 64 characters. Say in the result
that a snapshot was taken and how to restore ("restore from snapshot").

Capture failure is never silent, and never silently skipped: the capture runs
BEFORE the destructive call, so when it fails (404 = store missing, store
full, any error), STOP and tell the user there will be no snapshot — offer
the choices: proceed without one, take a full safety backup first
(`ha-nova:backup`), or prune the store (when full). The already-typed
confirmation stays valid for "proceed without one"; nothing is re-gated,
the user just decides informed.

## Restore (always through the family's own write path)

1. `list` → resolve the snapshot (ask when ambiguous; show age + name).
2. `load` → treat `data` as the restore draft.
3. Diff against the LIVE state (`ha-nova diff` where the family supports it),
   preview, and confirm — a restore is a normal write, never a blind put.
   The restore preview and result render as the Preview and Result Cards
   (`skills/ha-nova/output-rules.md` → Cards), like any write of that family.
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
| dashboards (deleted) | partial | the snapshot carries `{shell, config}`: recreate via `lovelace/dashboards/create` from the shell metadata (`url_path`, title, icon, sidebar, admin flag), then `config/save` the content — content returns fully, the internal `dashboard_id` is new |
| energy prefs | full | whole-document `save_prefs` |
| yaml files | full | data carries `{path, content}`; restore is `write_file` to that exact stored path (the slug name alone is lossy) |
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
