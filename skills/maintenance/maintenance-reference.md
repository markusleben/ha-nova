# HA NOVA Maintenance Reference

Single source of truth for recorder-statistics repair payloads, the issue matrix, purge semantics, and orphan-removal gates. Verified against Home Assistant core source (2026-07) and a live instance.

All payloads below are WS request bodies — write them to a payload file and send via `ha-nova relay ws --data-file <payload-file> --out <result-file>`.

## Statistics Issue Matrix (`recorder/validate_statistics`)

```json
{"type":"recorder/validate_statistics"}
```

Response: `{statistic_id: [{type, data}]}`. The complete issue-type vocabulary:

| Issue type | Meaning | Default remediation | Destructive? |
|---|---|---|---|
| `no_state` | statistics exist, entity gone from the state machine | clear after preview (only real fix) | YES — irreversible |
| `entity_not_recorded` | sensor excluded by recorder filter; statistics never compile | report only; fix is `recorder:` YAML config | no |
| `entity_no_longer_recorded` | statistics frozen — recorder filter now excludes the sensor | default KEEP (data preserved); clear only on explicit request | clear = YES |
| `state_class_removed` | sensor lost its `state_class` (integration change, lost customization) | diagnose the sensor first; clear only after confirm | YES |
| `units_changed` | stored unit incompatible with the sensor's current unit | relabel via `update_statistics_metadata` (keep numbers); clear is option 2 | relabel: no |
| `mean_type_changed` | arithmetic vs. circular mean mismatch (HA 2025.4+) | clear — old means are mathematically invalid | YES |

`units_changed` only fires when units are non-convertible; a convertible change (Wh→kWh) never raises an issue.

## Repair Payloads

Relabel (metadata only, numbers untouched; `unit_class` REQUIRED — omission breaks in HA 2026.11):

```json
{"type":"recorder/update_statistics_metadata","statistic_id":"sensor.x","unit_of_measurement":"kWh","unit_class":"energy"}
```

Convert stored values (not used by this skill's flows — listed for completeness; only within the same unit class; `old_unit_of_measurement` must match current metadata, mismatch = silent no-op):

```json
{"type":"recorder/change_statistics_unit","statistic_id":"sensor.x","old_unit_of_measurement":"Wh","new_unit_of_measurement":"kWh"}
```

Clear (IRREVERSIBLE — deletes all long-term and short-term statistics for the IDs):

```json
{"type":"recorder/clear_statistics","statistic_ids":["sensor.x","sensor.y"]}
```

The WS call waits ~10 s, then may return a timeout error while the job still completes in the background — verify by re-running `validate_statistics`, never blind-retry.

Metadata / inventory:

```json
{"type":"recorder/get_statistics_metadata","statistic_ids":["sensor.x"]}
{"type":"recorder/list_statistic_ids"}
```

Metadata fields: `has_sum`, `mean_type` (0 none / 1 arithmetic / 2 circular; `has_mean` is deprecated), `unit_class`, `statistics_unit_of_measurement`, `display_unit_of_measurement`, `source`. Data span for a preview — first/last returned bucket (`start`/`end` are ms since epoch):

```json
{"type":"recorder/statistics_during_period","start_time":"2015-01-01T00:00:00+00:00","statistic_ids":["sensor.x"],"period":"day","types":["change","mean"]}
```

## Sum-Spike Repair (`adjust_sum_statistics`)

```json
{"type":"recorder/adjust_sum_statistics","statistic_id":"sensor.x","start_time":"2026-07-08T13:00:00+00:00","adjustment":-42.5,"adjustment_unit_of_measurement":"kWh"}
```

Semantics: adds `adjustment` to the `sum` of the bucket starting at `start_time` AND every later bucket, in both the hourly and 5-minute tables. `state`/`mean`/`min`/`max` stay untouched. For a spike of +X in bucket R: `adjustment = −X`, `start_time = R.start`. Reversible: call again with `+X`.

- Locate the bucket with hourly `statistics_during_period`; 5-minute resolution exists only for ~10 days. The located bucket's `start` is ms since epoch — convert it to an ISO timestamp with offset for `start_time`.
- The adjustment is applied asynchronously via the recorder queue — an immediate re-read can show stale sums (live-verified); allow a few seconds before verifying. The inverse call restores values up to float rounding.
- The unit parameter is the unit the delta was computed in; non-convertible unit → `invalid_units` error, no partial write.
- Energy statistics often have a paired cost statistic (`energy/info` → `cost_sensors`) — a spike usually corrupted both; offer the paired adjustment.
- `utility_meter` entities: prefer the `utility_meter.calibrate` service over sum adjustments.

## Recorder Purge (services, via REST)

```json
{"keep_days": 30, "repack": false, "apply_filter": false}
```

POST `/api/services/recorder/purge`. Deletes states, events, and short-term statistics older than the cutoff. **Long-term (hourly) statistics are never purged.**

```json
{"entity_id": ["sensor.dead"], "keep_days": 0}
```

POST `/api/services/recorder/purge_entities` (also accepts `domains`, `entity_globs`; at least one selector required). `keep_days: 0` (default) deletes ALL state history for the match. Does NOT touch statistics — pair with `clear_statistics` consciously.

Both services are fire-and-forget: they return before the work finishes; verify via follow-up reads and `recorder/info` (`backlog` normal, `recording: true`, `migration_in_progress: false`).

Repack: SQLite `VACUUM` (needs up to ~2× database size free disk, blocks writes, roughly an hour per 10 GB), PostgreSQL plain `VACUUM`, MariaDB `OPTIMIZE TABLE`. Interrupting a repack risks corruption — recommend a recent backup first. HA auto-purges nightly (04:12) and auto-repacks every second Sunday; manual purge is for immediate wins only. Database size is not readable via a plain `system_health/info` call (subscription-based, returns `null`) — do not promise a size number; best-effort size lives in `ha-nova:health`'s event-collection path.

## Orphan-Removal Gate Checklist (ALL must pass)

1. State is `unavailable` AND has attribute `restored: true` (registry ghost this session). `unavailable` without `restored` = integration loaded, device offline → NEVER remove. `restored` is a state attribute — join `/api/states` with `config/entity_registry/list`; it is not a registry field.
2. Owning config entry (registry `config_entry_id`) is missing, or its state is permanently failed (`setup_error`, `migration_error`) — never `setup_retry`/`setup_in_progress`/`loaded`; treat `not_loaded`/`failed_unload` as inconclusive → report, do not remove (check `{"type":"config_entries/get"}`).
3. Not `disabled_by` (a disabled entity is absent from states — not an orphan).
4. Not part of a whole-config-entry outage: many `restored` entities sharing one config entry = integration problem; report only — never removal.
5. `{"type":"search/related","item_type":"entity","item_id":"sensor.x"}` returns no automations, scripts, scenes, or dashboards. Caveat to state in the preview: YAML-mode dashboards and templates inside automations are not covered by this check.
6. `config_entry_id: null` (template/YAML/helper platforms): gates 2 and 4 do not apply — gate 1 still does (a template computing to `unavailable` without `restored: true` is a broken source, not a ghost — never remove). `restored: true` here means the platform failed to claim the entity at the last startup. Require restart persistence: estimate the last restart from the `last_changed` cluster shared by restored entities and confirm the ghost predates it; if no restart boundary can be established, gate 6 fails — report only, and never restart or reload HA from this skill to create one. When in doubt, ask the user for one restart and re-check.

Removal:

```json
{"type":"config/entity_registry/remove","entity_id":"sensor.dead"}
```

There is no backend aliveness check — a live integration simply re-registers the entity. Deleted entries stay restorable ~30 days (entity_id and customizations return if the integration comes back), then purge permanently. Recorder data is NOT removed: states age out via purge; statistics remain and surface as `no_state` — name this chain in the preview so a statistics cleanup is a conscious follow-up.

Devices: there is no generic device-delete command; `config/device_registry/remove_config_entry` works only when the integration supports it. Report device orphans and route to the owning integration or the HA UI.

## Long-Unavailable Timestamp Confidence

| Source | Reliability |
|---|---|
| Last LTS bucket with data (`statistics_during_period`) | high, years back — numeric sensors only; doubles as "has statistics worth preserving" |
| State history transition to `unavailable` | exact, but only within recorder retention (default ~10 days) — REST `GET /api/history/period/<iso>?filter_entity_id=<id>&minimal_response&no_attributes`; an empty response means the transition predates retention → fall through to the next source |
| `last_changed` | lower bound only — resets to boot time on every restart; never report it as the outage start |
