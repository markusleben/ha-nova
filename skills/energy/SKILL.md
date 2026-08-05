---
name: energy
description: Use when analyzing Home Assistant energy data — consumption, solar production, battery, grid costs, per-device usage — or safely editing Energy dashboard sources and tracked devices through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Energy

## Scope

Analysis (read-only):
- energy config overview + misconfiguration triage (`energy/validate`)
- consumption/production/cost reports over bounded periods
- per-device ranking and cost (fixed or dynamic prices)
- solar/battery/grid KPIs: self-sufficiency, self-consumption, battery round-trip
- period comparisons (previous period, same period last year)

Config (gated writes):
- add/update/remove energy sources and tracked devices via `energy/save_prefs`

Not in scope:
- dashboard cards/views (`ha-nova:dashboard`)
- generic entity history/logbook (`ha-nova:history`)
- statistics repair (sum spikes, orphaned statistics): `ha-nova:maintenance`
- creating sensors or integrations that produce new energy data (energy sensor from a power sensor → `ha-nova:helper`, type `integration`)

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based requests: `ha-nova relay ws --data-file <payload-file>`; `--out <result-file>` for large responses. WS body: `.data` (`skills/ha-nova/relay-api.md` → Standard Envelope).
Schemas, KPI formulas, and analysis recipes: `skills/energy/energy-reference.md` — read it before any config write or KPI computation.

## Flow

### Overview
WS `{"type":"energy/get_prefs"}` → sources + tracked devices. Error `ERR_NOT_FOUND` ("No prefs") means the dashboard was never configured — offer initial setup instead of failing. Report grouped: sources first (grid/solar/battery/gas/water), then devices (count + notable entries). Run WS `{"type":"energy/validate"}` in the same pass; its issue lists are parallel to config order — map every issue to its source/device and name the concrete fix (issue table in the reference).

### Analysis
Resolve statistic IDs from prefs, then query `recorder/statistics_during_period` with `types:["change"]` and `units:{"energy":"kWh"}` over a bounded window (default: last 30 days). Follow the statistics contract in the reference exactly: HA-local day boundaries (weeks start Monday), ms-epoch timestamps, missing buckets count as 0, negative change is legal, `5minute` period only reaches back ~10 days.
Compute KPIs with the formulas in the reference so numbers match the HA UI; label documented approximations as such. Present cost figures as estimates — HA cost math never equals the utility bill (standing charges, VAT, official metering).
Never: diff raw `total_increasing` states (use statistics `change`), report peak watts from energy-only statistics (only hourly averages exist — say so), present a partial day/month as a complete period (label it and compare like-for-like), or rebuild an EV manager's session history (evcc and similar expose their own statistics entities — read those).

### Config edit (read → merge → save)
1. Fresh `energy/get_prefs` immediately before composing the save — never reuse an earlier read.
2. Detect the grid schema generation from the data (`flow_from` present = legacy nested; flat keys = HA 2026.3+). Emit the same generation; never mix. With no existing grid source to detect from, pick by HA version (`GET /api/config` → `version`; 2026.3+ = flat, else legacy). Preserve every field you do not edit verbatim, including unknown keys (`power_config`, `stat_soc`, `name`).
3. Mutate the returned list. `save_prefs` replaces the entire list for every key present — send only the top-level keys you touched, each as its complete list (canonical payload in the reference).
4. Preview the planned change: the exact entries added/changed/removed plus the count invariant (count before ± changes = count after). Say explicitly: removing a device stops tracking only — its recorded statistics stay.
5. Confirmation bound to this exact preview (context skill → Active Preview Confirmation): natural for a single add/change/remove; a save that empties or wholesale-replaces a list on a configured instance is destructive — typed confirmation code (context skill → Confirmation Tiers). Before any save that REMOVES entries (a single source/device removal included) or empties/wholesale-replaces a list, capture the auto config snapshot of the fresh pre-save `get_prefs` read (category `energy`, fixed name `auto-prefs` — the prefs are one document without item ids; `skills/ha-nova/config-snapshots.md`; on capture failure follow its capture-failure stop). Then WS `energy/save_prefs` (fails unless the App's configured HA token has admin rights).
6. Verify: re-read `get_prefs` (the save response echoes the new prefs — still re-read), confirm the invariant and that every untouched entry is deep-equal to the fresh pre-save read, then run `energy/validate` — a successful save does NOT prove the config works (dangling statistic IDs are accepted silently). Mention that other open dashboard tabs need a reload.

Initial setup after `ERR_NOT_FOUND`: treat every list as empty — compose only the keys being populated (count before = 0) and continue at step 4.

Write guards (schema details in the reference):
- every grid source needs `cost_adjustment_day` (default `0`); at most one of `entity_energy_price`/`number_energy_price` per direction
- never set price fields on an external statistic (`:` in the ID) unless `stat_cost` is already set — HA 2026.4+ rejects the save
- never invent `stat_cost` — leave it `null` with a price field set and HA creates the cost sensor itself; resolve actual cost statistics via `energy/info` → `cost_sensors`
- `included_in_stat` must reference a `stat_consumption` present in the same list, without cycles — HA does not validate this
- referenced statistics must exist (`recorder/list_statistic_ids`), should belong to a live entity (validate reports `entity_not_defined` otherwise), and must meet the sensor prerequisites in the issue table

## Error Handling

- `ERR_NOT_FOUND` on `get_prefs`: unconfigured dashboard, not a failure.
- Save rejected: report HA's message verbatim; re-read prefs before any retry — another client may have written meanwhile.
- Full relay error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md`. Write previews, delete confirmations, and results render as the Cards defined there.

- `Energy` / `Status`
- `Planned change`
- `Options`
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Analysis answers render the Report shape; rankings render the List Frame (output-rules.md) — never raw JSON.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- Config writes: preview + confirmation per save (natural for single edits; typed confirmation code for empty/wholesale-replace saves — step 5); verify by read-back + validate.
- No update-revert — recovery after an entry-removing save is the `auto-prefs` config snapshot (restore = corrective `save_prefs` of the loaded document, `skills/ha-nova/config-snapshots.md`); otherwise a corrective save or HA Backups (see `skills/ha-nova/write-safety.md`).
- Analysis flows are strictly read-only; never call `save_prefs` while answering an analysis question.
