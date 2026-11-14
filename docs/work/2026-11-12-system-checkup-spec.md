# System check-up and backup confidence

Status: active

Date: 2026-11-12. Target: 0.31 (release/0.31). Owner: `ha-nova map --section checkup` (`cli/verb_map_checkup.go`, `cli/verb_map_checkup_home.go`), the health skill (`skills/health/checkup.md`), three Supervisor stats reads in the Relay's table. Read-only. Source: plan 0.31 #7.

## Problem

"Is my Home Assistant OK, why is it slow, would my backup work?" needs an admin today. The health skill reads repairs, integration states and entity availability, but nothing about the system underneath (Supervisor health, free space, which App uses the memory) and nothing about backups beyond the update advisor's check before an install. The model would have to read, join and judge a dozen sources itself; the verdicts must not depend on it.

## Design

One more section of the read verb `ha-nova map`, as the update advisor did (`docs/work/2026-11-12-update-advisor-spec.md`): `ha-nova map --section checkup`. Chat apps reach it through `ha_map` (`section` enum gains `checkup`; tool text unchanged, so `TestMCPToolTextStable` holds). `--limit` is refused with it. Every verdict is a fixed rule in Go; the model only words the result.

Strings in the result are fixed codes, the Supervisor's resolution enum words (contexts, unhealthy reasons, issue types as of Supervisor 2026.10 and main; any other word is `other`), Core versions that parse as Home Assistant versions, integration domains checked like log facts, numbers, times HA NOVA formats, and three kinds of display names through `sayText`: App names (the Supervisor's App list), integration names (`manifest/list`), and the fixed names "Home Assistant" and "Supervisor". `sayText` removes formatting, not meaning: a name someone gave a local App passes as that name. Nothing else from Home Assistant or the Supervisor passes: no error text (a failed read is a fixed reason: the Relay's error code, `UNKNOWN_SHAPE`, `NOT_REPORTED`, `VIA_HOME_ASSISTANT_LINK`, `TOO_MANY_APPS`, else `READ_FAILED`), no resolution reference except an App's name, no config entry title (it can hold an account name), no repair title, no backup name, no location label (agent ids are `<domain>.<name>`: only the domain passes), no App slug a backup names but the App list doesn't.

### Reads

In parallel, session-memoized, reusing the advisor's loaders: `loadHome`, `/api/config` and the update entities (`loadPending`), `repairs/list_issues` (now with `is_fixable`), `backup/info`, `manifest/list`, and log facts (`readLogFacts`, #1141; refused on the hosted demo like `ha_read`). Where Supervisor runs (the advisor's rule): `/resolution/info`, `/host/info`, `/addons`, and **new** `/core/stats`, `/supervisor/stats`, `/addons/{slug}/stats` for each started App (at most 40; more is a `not_checked` row).

Relay: the three stats routes join `SUPERVISOR_ROUTES` as `read` and the CLI mirror `rawSupervisorRoutes` (the parity test keeps them equal). The App's `hassio_role: manager` reaches them (Supervisor `ROLE_MANAGER`: `/core/.+`, `/supervisor/.+`, `/addons/<slug>/…`); the replies are numbers and still pass the Relay's redaction. Read verbs allow `/addons`, `/core/stats`, `/supervisor/stats` and `/addons/<slug>/stats` in `checkReadOnlyRequest`'s `supervisor` case (exact `{method: GET, path}`, `self` refused by the route grammar). The verb tests' read oracle also accepts the exact log-facts read, which is answered as facts only.

Compatibility: an App older than 0.31 refuses the stats paths (`SUPERVISOR_REQUEST_REFUSED`): `resources` is `not_checked` and the rest still runs. A 0.30 App's gate refuses the log read (`GATE_REFUSED`): `log_facts` is `not_checked`. Home Assistant Link carries no Supervisor route (`VIA_HOME_ASSISTANT_LINK`). The v1 stats paths answer `cpu_percent` over a short window (a share of the whole machine); Supervisor's v2 API drops it, so a missing `cpu_percent` is `not_checked: cpu`, never 0.

### Result

```
{section: "checkup", status: ok|attention|limited, checked_at,
 findings: [{code, level: problem|notice, area, …facts}],
 not_checked: [{check, reason}],
 facts: {supervisor: running|not_applicable, disk, resources, integrations, backup, updates}}
```

`status`: `attention` when any `problem`; else `limited` when anything is `not_checked`; else `ok` (notices may still be listed). Same enum as the health skill's overall. `ok` is "nothing found in what HA NOVA checked".

### Checks

| Area | Code | Level | Rule |
|---|---|---|---|
| system | `SUPERVISOR_UNHEALTHY` | problem | `/resolution/info` `unhealthy` not empty (reasons as enum words). |
| system | `SYSTEM_ISSUES` | problem | Supervisor resolution issues `{type, context, app?, fix}`. `fix: repair` when Home Assistant mirrors the issue as a fixable repair (domain `hassio`, `issue_id` = the issue's uuid hex, `is_fixable`; Core 2026.11 `hassio/coordinator.py` `_create_or_update_issue_repair`), else `fix: ui`. At most 20 rows. |
| disk | `DISK_LOW` | problem / notice | `disk_free` < 2 GB (Supervisor refuses updates and backups): problem; < 10 % of `disk_total` and < 5 GB: notice. |
| resources | `HIGH_MEMORY` | notice | Core, Supervisor and running Apps together use ≥ 90 % of the memory limit they all report; computed only when every container reports the same limit (none has its own, so it is the machine's memory); otherwise `not_checked: memory_share`. |
| integrations | `INTEGRATIONS_FAILING` | problem | enabled config entries in `setup_error`, `setup_retry`, `migration_error`, `failed_unload`: `{domain, name, state, count}`. |
| repairs | `REPAIRS_OPEN` | problem | not-ignored repairs minus those under `SYSTEM_ISSUES`: `{count, alerts, domains}`. |
| backup | `NO_BACKUP` | problem | no backup that includes Home Assistant. |
| backup | `NO_RECENT_BACKUP` | problem | the newest such backup is older than 8 days (a weekly schedule, a day late). |
| backup | `BACKUP_LAST_FAILED` | problem | `last_attempted_automatic_backup` newer than `last_completed_automatic_backup` (or none completed) while no backup runs. |
| backup | `BACKUP_PROBLEM` | problem | reasons: `failed_copy` (the newest backup's `failed_agent_ids`), `location_unreachable` (`agent_errors`), `apps_missing` (`failed_addons`), `folders_missing` (`failed_folders`: `share`, `addons/local`, `ssl`, `media`, else `other`). |
| backup | `BACKUP_ONLY_ON_DEVICE` | notice | the newest lives only on `backup.local`/`hassio.local`. |
| backup | `NO_AUTOMATIC_BACKUP` | notice | `next_automatic_backup` is null. |

Facts: `disk` {free_gb, total_gb, used_percent}; `resources` {memory_mb, memory_percent?, cpu_percent?, top_memory (3), top_cpu (3)}: a reading of this moment, of these containers only, never the whole machine's load; `integrations` {failing, logging_errors: top 5 domains with ERROR/CRITICAL log facts, count, since, last}; `backup` {restore_tested: false, newest {date, age_days, database_included, protected, locations, off_device_copies, size_mb}, automatic {last_completed, next}}; `updates` {pending, by_kind, core?, running}.

`not_checked` checks: `apps`, `system`, `disk`, `resources`, `cpu`, `memory_share`, `backup_state`, `backup_runs`, `backup_copies`, `log_facts`, `repairs`, `backups`, `backup_schedule`. Without Supervisor its areas are not applicable, not gaps.

### Resolution suggestions: never applied here

The check-up applies nothing. A `SYSTEM_ISSUES` row with `fix: repair` is fixed through the health skill's existing **Fix a Repair** flow (`/api/repairs/issues/fix` via `plan raw` + `apply`): Home Assistant's Supervisor repair flow lists the suggestions and applies the chosen one. Every repair-flow POST is already typed (`cli/plan_raw_tier.go`), so nothing runs without the person's confirmation. `fix: ui` is a hand-off to Home Assistant's Repairs or System pages; an issue Home Assistant doesn't mirror may not show in Repairs. No new write route, no Supervisor POST.

## Skill

`skills/health/checkup.md` (a reference; health's Flow step 0 sends check-up questions there; the router's health example names "why so slow" and "would my backup work"): run the section, lead with `status` in plain words, problems with their next step, notices, then what was not checked and why (an older App: update it). Backup answers always say HA NOVA did not test a restore, that a protected backup needs its emergency kit kept outside Home Assistant, and that a real proof is a test restore on a spare device, never on this home. A skill contract test checks that `checkup.md` words every code the Go code can return.

## Known limits

- Backup locations are integration domains: a network share and the device's own disk can share one (`hassio`); `off_device_copies` counts the copies not on `backup.local`/`hassio.local`.

- Database size is not read (`system_health/info` is a subscription; the files deny list covers the DB).
- Decryptability and restorability of a backup are not checked; age, completeness and copies are metadata.
- CPU is one short window and covers Home Assistant, the Supervisor and the running Apps only; it is a fact, not a finding.
- Container/Core installs have no Supervisor: system, disk and resources are not applicable.
- The bed has no Supervisor: the Supervisor paths are proven by Go tests with replies shaped like Supervisor 2026.10 and Core 2026.11, not on the bed.
- No scheduled check-up and no phone push (0.32, the App worker).

## Tests

Go (`cli/verb_map_checkup_test.go`): a healthy HAOS (status `ok`, resources and backup facts, no stats read for a stopped App); every finding at once with poisoned references, entry titles, repair titles, agent ids and errors, backup names, App slugs, folders, unhealthy reasons and log messages, none of which reaches the result; an older App (stats refused) and a v2 reply without `cpu_percent`; no Supervisor (no Supervisor read sent, the log refused like a 0.30 gate); the read allowlist and `--limit`. TS: Relay ↔ CLI parity with the new routes; `tests/skills/checkup-contract.test.ts`. Bed: `scripts/e2e/testbed/checkup.mjs` (the section on throwaway HA, a poisoned demo error counted only) and `scripts/e2e/testbed/agent-checkup.mjs` (a headless agent on the shell and the chat-app path runs the check-up and changes nothing).

## Review record

- KISS (claude-sonnet-5-5): taken: `HIGH_CPU` dropped (CPU stays a fact), backup codes merged into `BACKUP_PROBLEM` with reasons, `DISK_TIGHT` merged into `DISK_LOW`'s notice level, `SUPERVISOR_UNSUPPORTED` (HA mirrors it as a repair), `UPDATE_RUNNING` and `UPDATES_PENDING` (facts), `BACKUP_WONT_FIT` dropped, log facts a fact not a finding, no "older App" special case in Go. Not taken: dropping per-App stats (an App is the usual reason a home is slow; the requirement names resources), dropping `BACKUP_ONLY_ON_DEVICE` and `NO_AUTOMATIC_BACKUP` (the two most common reasons a backup won't save a home; computing them in the skill would put the model in the verdict), dropping the repair de-duplication (one line, avoids counting one problem twice).
- Adversarial reviews (claude-sonnet-5-5; privacy/fail-open and UX lenses): no P1. Fixed: a memory share that can't be computed is `not_checked` (it passed quietly); the skill now words every unread part, `facts.supervisor: not_applicable`, `DISK_LOW` by level, CPU and memory as numbers without a judgment, and "never say your backup works"; `off_device_copies`; the router line names "why is everything slow" (a whole-home question; a named automation stays `diagnose`); 8 days instead of 7; the disk notice also needs < 5 GB. Not taken (P3): the update advisor's error text in its gaps and its unsanitized App names (every read verb shows Relay error messages today; the check-up is stricter because the 0.32 worker will push it), undated backups counted as none.
- Local Codex review (gpt-6-luna, read-only): five P2s, all fixed: Supervisor words are now an enum allowlist (a pattern let any lowercase identifier through), Core versions must parse, and an unparsable schedule time, an unknown backup state, an unparsable run time, or a newest backup with no locations are `not_checked` instead of passing quietly.
- Astra (gpt-6-astra, medium): D1 agrees (a `map` section, a health reference). D2 partly: exact GET-only routes are right; an older App must give partial coverage (done), don't diagnose the cause from every refusal (the reason is the Relay's own code; the skill words only `SUPERVISOR_REQUEST_REFUSED` as an older App). D3 partly: the repair flow is right; all repair POSTs are already typed (spec now says so); the uuid join is confirmed against Core 2026.11's coordinator (`issue.uuid.hex`, `is_fixable=bool(presentable)`) and `is_fixable` was added; `fix: ui` promises no Repairs entry. D4 disagreed with "no free text": taken: the spec names the display names that pass, failures carry fixed reasons, backup locations are domains only, unknown App slugs are a count, the alert titles the advisor shows are not used. D5 partly: taken: gaps stay listed beside problems, resources are "these containers at this moment", the memory share only with one shared limit, `BACKUP_WONT_FIT` dropped, the skill always says no restore was tested.
