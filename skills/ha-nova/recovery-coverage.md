# HA NOVA Recovery Coverage

Canonical path: `../ha-nova/recovery-coverage.md` (moved out of
`../ha-nova/write-safety.md`, which keeps the pointer).

## Safety-Mechanism Availability by Skill

Diff and revert coverage is deliberately uneven. Never imply a mechanism a
skill does not have; when only Backups remain, say so before the write. For
far-reaching changes (irreversible deletes, full-document overwrites, bulk
operations) offer to create one first via `ha-nova:backup` — its safety-backup
flow checks for a recent full backup before creating, because full backups are
expensive. EXCEPTION: when the operation auto-captures a config snapshot
(`../ha-nova/config-snapshots.md`) and the store is available, the snapshot
IS the recovery net — skip the full-backup offer for that config-level op.
Never suggest a backup for routine small edits.

Config-level recovery: relays with the snapshot store (`POST /backups`) let the
covered families auto-capture a config snapshot before deletes/full-document
overwrites and restore selectively through their own write path —
`../ha-nova/config-snapshots.md` is the SSOT (capture rules, restore
fidelity, honest limits). The full-backup offer stays for system-level
operations and for relays whose `/backups` answers 404.

| Skill / family | Pre-write diff | Update-revert | Fallback recovery path |
|---|---|---|---|
| `write` (automation/script) | yes (`plan write` diff) | yes (`plan revert`; last 25 targets per server, saved by `apply`) | config snapshot (taken by `apply` before delete, identity-preserving restore); HA Backups |
| `helper` storage family | yes | yes (verified updates, last 25 targets) | config snapshot (auto before delete; recreate mints a new id); HA Backups |
| `helper` config-entry family | diff only | no (multi-step options flow) | HA Backups |
| `integration-setup` | flow-step preview + config-entry read-back; credential-recovery reload previews the entry reload (non-destructive, entities briefly drop) | no (multi-step add/reauth flow) | cancel only an unfinished add flow started in this session; after entry creation, manage or remove it in Home Assistant UI |
| `calendar` | event-field preview + bounded event read-back | no (provider event mutation) | provider recycle bin when available; otherwise recreate from the approved preview |
| `hacs` | repository/category/version preview + category-appropriate read-back | no (package lifecycle) | reinstall the previous pinned version from recorded pre-change state; HA Backup for migrations (config entries sit outside config snapshots) |
| `dashboard` | yes (`plan write` view/card diff) | yes (`plan revert`; saved by `apply`) | config snapshot (taken by `apply` before every save and delete; a deleted dashboard restores partially); HA Backups |
| `scene` | preview + read-back verify | no | config snapshot (auto before delete, identity-preserving restore); HA Backups |
| `todo` | preview + read-back verify | no (list delete irreversible) | re-add items; HA Backups for lists |
| `updates` | preview + entity re-read verify | no (updates not downgradable) | HA Backups (offered pre-install for core/OS) |
| `energy` | change preview + read-back & validate verify | no (corrective save) | config snapshot (auto before entry-removing saves, whole-doc restore); HA Backups |
| `maintenance` | grouped preview + re-validate verify | spike adjust only (inverse call); clears/purges/removals irreversible | HA Backups (offered pre-bulk) |
| `organize` | field preview | no (registry deletes irreversible) | config snapshot (auto before rename/disable/unit change, in-place field write-back); registry deletes: HA Backups |
| `service-call` | state-delta preview | no (runtime action, not config) | re-run corrective service call |
| `admin` (persons/zones/tags/users) | field preview | no (deletes irreversible; user credentials/tokens unrecoverable) | recreate from previewed fields (tags keep their physical `tag_id`; other recreates mint new ids); HA Backups |
| `assist` (pipelines/exposure) | change preview | no | re-toggle exposure / resend prior pipeline fields; deleted pipelines recreate with a new id |
| `yaml-config` | File-Change Preview + `check_config` | one-step `.bak` restore (writes only; a second write overwrites it) | config snapshot with stored path (auto before overwrites/deletes, multi-step history); `.bak`; HA Backups |
| `esphome` | File-Change Preview (masked) | one-step `.bak` restore | config snapshot with stored path; the device changes only on the next install, and a broken key or OTA setup needs USB |
| `mqtt` (publish) | payload preview | no (broker/device state) | clear a retained message by publishing an empty retained payload; corrective publish |
| `backup` (delete) | delete preview | no | none — a deleted archive is gone |
| `fallback` (experimental writes) | payload preview + read-back verify | no | HA Backups |
