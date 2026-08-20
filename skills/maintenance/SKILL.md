---
name: maintenance
description: Use when repairing Home Assistant recorder statistics (orphaned statistics, unit mismatches, sum spikes), purging recorder history, cleaning up dead entity-registry entries, or answering how long an entity has been unavailable or dead — through HA NOVA Relay. For what is unavailable right now, use ha-nova:health.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Maintenance

## Scope

Read (always allowed):
- statistics issue triage (`recorder/validate_statistics`, grouped by issue type)
- recorder health (`recorder/info`: `backlog`, `recording`, `migration_in_progress`)
- long-unavailable entity report with confidence-labeled timestamps
- orphaned registry-entry candidates (report first, remove only via the gated flow)
- recorder growth triage: which domains/entities dominate recorder growth (report only; the `recorder:` filter fix is `ha-nova:yaml-config`)

Gated writes:
- clear orphaned statistics, relabel statistic units, repair sum spikes
- recorder purge (`recorder.purge`, `recorder.purge_entities`)
- remove dead entity-registry entries

Not in scope:
- energy source/device config (`ha-nova:energy`)
- repairs/status overview and current unavailability summaries (`ha-nova:health`); the long-unavailable report here exists to qualify cleanup candidates, not to answer "is everything OK"
- backups (`ha-nova:backup`), updates (`ha-nova:updates`)
- device deletion — no generic API; report and route to the owning integration/UI
- MQTT ghost entities recreated by a retained discovery message — the broker-side cleanup is `ha-nova:mqtt` (retained-discovery cleanup); registry removal here only sticks after that topic is cleared

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based requests: `ha-nova relay ws --data-file <payload-file>`; `--out <result-file>` for large responses. WS body: `.data`; REST body: `.data.body` (`skills/ha-nova/relay-api.md` → Standard Envelope).
Payloads, issue matrix, and gate checklists: `skills/maintenance/maintenance-reference.md` — read it before any write.

## Flow

### Statistics triage
WS `{"type":"recorder/validate_statistics"}` → group results by issue type and report counts per group with examples — never dump hundreds of IDs. Map each type to its default remediation from the reference matrix (relabel vs. clear vs. report-only). Hundreds of `no_state` orphans are normal on older installs.

### Statistics repair (gated)
Grouped clears and per-config-entry registry removals are batch operations per `skills/ha-nova/batch-safety.md` — one issue group or config entry per manifest, `confirm:batch-...` code format, completion ledger; every safety gate below stays unchanged. A registry removal may instead ride a cross-family destructive cleanup manifest (`skills/ha-nova/grouped-change-set.md`) with every gate below intact.
- `units_changed` → default is the non-destructive relabel (`recorder/update_statistics_metadata`, `unit_class` REQUIRED) — only after confirming the values are the same quantity merely mislabeled; if the sensor changed what it measures, relabel corrupts data — then clear or keep.
- Orphan cleanup (`no_state` etc.) → `recorder/clear_statistics` is IRREVERSIBLE. Preview per group: the FULL ID list (write long lists to a file the user can open — a capped sample never authorizes the uncapped set), each ID's data span and `has_sum`, and — mandatory — the `energy/get_prefs` cross-check: whether any ID appears anywhere in that JSON or in `energy/info` → `cost_sensors` (warn loudly — clearing erases its energy history), plus a scan of storage dashboards for the IDs (YAML dashboards stay a stated caveat). Manifest-bound typed confirmation in the `confirm:batch-...` format (`skills/ha-nova/batch-safety.md`) — it binds to one issue group and the manifest's full enumerated ID set; a group above the batch cap splits into multiple manifests over disjoint subsets of that group, each with its own preview and code; never one confirmation for multiple groups. Execute in one call, but verify per ID by re-running validate; a WS timeout after ~10 s is NOT failure — the job finishes in the background, re-read instead of retrying.
- Sum spike → confirm the jump is spurious (glitch, meter swap) and not real usage — when unsure, ask. Locate the bad bucket via `statistics_during_period` (hourly; `5minute` only if <10 days old), derive the corrected `change` from neighboring buckets or user input, then `recorder/adjust_sum_statistics` with `adjustment = corrected − original` at the bucket's start. Reversible via the inverse call — offer that rollback. Check `energy/info` → `cost_sensors` for a linked cost statistic; verify each of the pair before offering the paired fix. Verify by re-reading the bucket.

### Recorder purge (gated)
- `recorder.purge` (service): preview keep_days + deletions (states/events/short-term statistics). Quantify the impact first: read a small bounded pre-cutoff history sample so the preview can state that data older than `keep_days` exists and will actually go. Long-term statistics are NEVER purged — say so. `repack: true` only with an explicit warning: rebuilds the database, blocks recording, needs free disk. `apply_filter: true` additionally deletes data the recorder's include/exclude filters exclude — and for those entities/event types HA purges up to the CURRENT time, not the `keep_days` cutoff, so their recent history is erased too. The preview must say exactly that; when in doubt, offer the plain purge without `apply_filter`. The call is fire-and-forget — verify `recorder/info` (`backlog` normal, `recording: true`) plus a pre-cutoff history read returning empty (allow minutes). Typed confirmation code.
- `recorder.purge_entities`: default `keep_days: 0` = ALL state history for the match. Expand `domains`/`entity_globs` selectors to the concrete matched entity list BEFORE the preview — the preview and confirmation code bind to that exact list, never the raw selector. Say explicitly that statistics are NOT touched (a separate clear is a conscious follow-up). Typed confirmation code. Verify by re-reading a bounded history window for one matched entity (allow minutes); `recorder/info` alone cannot prove deletion.

### Orphan cleanup (gated)
Detection: join `/api/states` with `config/entity_registry/list`. A candidate must pass ALL gates in the reference checklist — headline gates: state is `unavailable` WITH attribute `restored: true`; owning config entry is gone or permanently failed (never `setup_retry`/loading); not `disabled_by`; not part of a whole-integration outage (group those as one finding — report only, never removal); `search/related` returns no automations/scripts/scenes/dashboards — with the honest caveat that YAML dashboards and templates are not covered; entities with `config_entry_id: null` (template/YAML platforms) are removal candidates only when the ghost persists across a restart (method in the reference — if no restart boundary can be established, report only; never restart or reload HA from this skill).
Live/healthy entities: refuse removal — offer disable via `ha-nova:organize` instead; the gates are not negotiable.
Preview per config entry with the LTS consequence named ("registry removal keeps statistics — they then report as `no_state`"; deleted entries are restorable for ~30 days if the integration returns). Manifest-bound typed confirmation in the `confirm:batch-...` format, one config entry per manifest (`skills/ha-nova/batch-safety.md`); execute per entity via `config/entity_registry/remove`; verify by re-reading the registry.

### Long-unavailable report
Per entity, label the timestamp source: LTS last-data bucket (reliable, years back, numeric sensors only), state history transition (only within recorder retention; how-to in the reference), or "since last restart at earliest" (`last_changed` resets on restart — never present it as the outage start).

## Error Handling

- `clear_statistics` timeout: expected on big batches — verify by re-read, never blind-retry.
- Purge services return before finishing — absence of an error is not completion.
- Full relay error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md`. Write previews, delete confirmations, and results render as the Cards defined there.

- `Maintenance` / `Status`
- `Planned change`
- `Options`
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Triage renders the Report shape; grouped ID lists render the List Frame (capped example lists) — never raw JSON.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.
- `search/related` verdicts fail closed: verify `ok=true` and `data` is an object before projecting family keys (`skills/ha-nova/relay-api.md` → Parsing rule); a failed or unexecuted scan is inconclusive — never a no-consumer result. An inconclusive scan fails the ghost-entity removal gate: report only, never remove.

- Every write: preview → confirmation → per-item verification. Clear/purge/registry remove take the typed confirmation code; the reversible spike adjustment and unit relabel take payload-bound natural confirmation — offer the spike rollback.
- Backup gate: before clearing statistics in bulk, purging with low keep_days, repack, or bulk registry removal, check for a recent backup via `ha-nova:backup` (proportional — never for a single small fix).
- Never remove an entity that is merely offline; when in doubt, report instead of removing.
