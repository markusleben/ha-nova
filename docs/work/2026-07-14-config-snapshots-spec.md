# Spec: Config Snapshots (Wave 2 — Relay `/backups` + skill layer)

Status: `active` — implementation spec for masterplan-2026-h2 Wave 2; Relay 0.5.0.

## Problem

Full HA Backups (GB-scale, system-level restore) are the only recovery path for 6+ config families; deletes have no revert anywhere; the update-revert stack (`cli/snapshot.go` → `undo-snapshots.json`) is client-local, capped at 5 targets, updates only. A deleted automation, scene, or dashboard card today means "restore a full backup or rebuild by hand."

## User experience

- *"Snapshot my automations before we clean up"* → named, targeted snapshot (KB-sized, seconds).
- **Auto-snapshot before destructive ops**: before a delete or full-document overwrite in a covered family, the owning skill captures the affected items automatically — deletes become recoverable for the first time. Replaces the Wave-0 safety-backup offer for config-level items (full HA Backups stay for system-level: updates, recorder purges).
- *"Restore the kitchen automation from yesterday"* → selective per-item restore through the normal diff-preview → confirm → write → verify path.
- *"What snapshots do I have?"* → list with name, age, category, item count. Auto-prune: default 30 days / 100 files; named (user-requested) snapshots exempt.

## Relay endpoint — `POST /backups` (generic blob store, zero HA semantics)

Bearer-authed like the other routes; body is `{"action": ...}`:

| action | request | response |
|---|---|---|
| `save` | `category` (path-safe slug), `name` (path-safe slug), `data` (JSON value) | `{file, bytes, created_at}` — stored gzip'd as `<category>/<name>-<utc-timestamp>.json.gz` |
| `load` | `file` | `{data, created_at, bytes}` |
| `list` | optional `category` | `[{file, category, bytes, created_at}]`, newest first |
| `delete` | `file` | `{deleted: true}` |
| `prune` | `max_age_days` (default 30), `max_files` (default 100), optional `category`, `keep_named` (default true) | `{deleted: [files]}` |

Rules (mirror `files.ts` hardening):
- Storage root: App data dir (`/data/ha_nova_snapshots/` in the App; `SNAPSHOT_DIR` env for the standalone container). Swept into full HA Backups automatically on Supervised installs.
- Path containment via realpath; `category`/`name` validated `[a-z0-9-]+`; no traversal, no symlinks.
- Caps: the per-snapshot ceiling is the RAW request — the server JSON-parses and caps every POST body at 1 MiB before route dispatch (`nova/src/http/server.ts`), so a `save` whose envelope exceeds that fails with the standard 413 before the handler runs. Document that: a single config item over ~1 MiB raw is out of snapshot scope (HA Backups cover it); the handler itself enforces only the totals — 500 files, 50 MiB stored (gzip'd) — and fails loud at those with a prune hint.
- Named-vs-auto distinction: auto-snapshots use the reserved name prefix `auto-`; `prune` treats everything else as named (`keep_named`).
- The relay never parses `data` — opaque JSON in, opaque JSON out. No HA calls, no domain logic (charter test pins this).
- Feature-gating: endpoint always on (no opt-in — it stores only what skills already read via the relay). `GET /health` gains `snapshots: {files, bytes}` counters (Wave-5 health extension lands together).

## Skill layer

New shared reference `skills/ha-nova/config-snapshots.md` (SSOT, loaded on demand), consumed by the owning mutation skills:

- **Capture**: before a destructive op in a covered family, `save` the full HA-normalized read-back of the affected item(s) under the family category (`automations`, `scripts`, `scenes`, `dashboards`, `helpers`, `energy`, `yaml`, `metadata`, ...). One item per snapshot for selective restore; batch ops save one snapshot per item plus the manifest.
- **Restore**: `load` → diff against live state (`ha-nova diff` where the family supports it) → normal preview/confirm → in-place write via the family's own API — NEVER delete+recreate — → read-back verify. Restore of a deleted item = create with the original identity key where the API allows it.
- **Honesty labels per family** (from the audited matrix in masterplan-2026-h2 Wave 2):
  - FULL: automations, scripts, scenes (upsert by id), dashboard content (save by `url_path`), energy prefs, YAML files, tags, assist exposure, entity/device metadata.
  - PARTIAL (item restores, identity/graph does not): storage-helper deletes, Lovelace resources, persons, zones, pipelines, to-do lists, dashboard deletes — restore preview must name the orphaned inbound references.
  - NOT covered (skills must not offer): config-entry helpers, areas/floors/labels/categories, users, recorder data, updates, backup archives.
- A snapshot restores the ITEM, never the reference graph — the restore preview says so whenever consumers were involved.
- Relation to update-revert: the client-local stack stays the quick-undo for verified updates; snapshots are the durable, named layer. No migration in this wave.
- Naming: user-facing term is "config snapshot" (localized); never conflicts with bulk-review's ban on building matched-set snapshots (`review/SKILL.md`) — that ban is about review worksets, and review stays read-only.

## CLI

`ha-nova relay backups --data-file <payload-file>` passthrough (pattern: the `files` case in `cli/relay.go`, ~5 lines + test).

## Out of scope (this wave)

- Restore across HA instances, encryption (the store holds config the LLAT can read anyway; file permissions follow the data dir).
- Scheduled/periodic snapshots (Sentinel/Briefing territory, backlog).
- Migrating update-revert onto the store.

## Tests

- Relay unit: action routing, path containment (traversal/symlink), caps + loud failure, gzip roundtrip, prune policy incl. `keep_named`, envelope + version header (extend the error-envelope suite).
- Charter: no HA-domain logic in the handler (extend the docs fact-check "no backup/streaming endpoints" check — it must now allowlist `/backups` as a generic store while still failing on domain logic; update `scripts/` fact-check accordingly in the same PR as the endpoint).
- Skills: contract tests pinning the capture-before-destructive lines in the covered mutation skills and the honesty labels in `config-snapshots.md`.
- E2E: disposable-HA scenario — create automation → snapshot → delete → restore → verify identity preserved.

## Sequencing

1. Relay endpoint + CLI passthrough + tests (one PR; `nova/config.yaml` relay version 0.5.0, `.goreleaser` untouched).
2. `config-snapshots.md` + capture/restore wiring in write/scene/dashboard/helper (FULL families first) (one PR).
3. PARTIAL families + auto-prune UX + fallback/dispatch rows (one PR).
4. Wave 5 (diagnosability) lands in the same Relay 0.5.0 release train before the release-prep PR bumps `min_relay_version`.
