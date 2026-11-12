# Update advisor (phases 1 and 2)

Status: active

Date: 2026-11-12. Target: 0.31 (release/0.31). Owner: `ha-nova map` (new `cli/verb_map_updates*.go`), the updates skill (`skills/updates/SKILL.md`, new `advisor.md`). Reads only, and every install keeps today's confirmation; the read-verb allowlist grows by three Supervisor GETs and two WS reads, so the review treats that boundary as security-sensitive.

## Problem

The updates skill lists pending updates by group, gates each install and runs them one by one. A person who asks "can I update?" still doesn't hear:

- whether Home Assistant will refuse or fail the update (Supervisor unhealthy, disk full, a backup running, no recent backup);
- in which order to install, when an App needs the new Core;
- whether the new Core breaks something **in this home**;
- what is risky (several releases skipped, major versions, custom integrations, firmware on a lock);
- after the update: what broke, and whether Home Assistant rolled the update back.

Today the model reads these pieces itself, if at all, and matches release notes by eye. The skill's rule "Apps and firmware before core/OS" is wrong when an App needs the new Core.

## Design

Two new sections of the existing read verb `ha-nova map` (exact, CLI-computed facts; read-only; the model never counts):

- `ha-nova map --section updates` (phase 1): readiness, order, breaking changes matched to this home, risk flags. Saves a baseline for phase 2.
- `ha-nova map --section after_update` (phase 2): what changed since that baseline, and rollback detection.

Chat apps reach both through `ha_map` (`section` enum gains `updates` and `after_update`). The tool text hash (`TestMCPToolTextStable`) covers names and descriptions only, so claude.ai's Always allow stays; the chat tools version changes (the enum is in `tools/list`), so ChatGPT users get the existing refresh hint. `--limit` is refused with these sections.

The Relay is unchanged. Read verbs gain one narrow case in `checkReadOnlyRequest`: endpoint `supervisor`, payload exactly `{method: GET, path}`, path exactly `/resolution/info`, `/host/info`, or `/store/addons/<slug>` (the Supervisor read table's slug grammar, `self` refused). `repairs/list_issues`, `backup/info` (phase 1) and `trace/list` (phase 2) join `readOnlyWSTypes`. Supervisor is read only where `/api/config` lists the `hassio` component or a Supervisor update entity exists; without either the result says `SUPERVISOR_NOT_APPLICABLE`. Where Supervisor runs, a failed or incomplete read is `not_checked`, never "fine".

### Phase 1: `updates`

Reads (in parallel, session-memoized): states, entity/device registry, config entries (`loadHome`), `/api/config` (components, version), `repairs/list_issues`, `backup/info`, `manifest/list`, Supervisor `/resolution/info` and `/host/info`, and `/store/addons/<slug>` for each pending App.

**Pending updates.** `update.*` with `state: on`, classified exactly as the skill does today (`platform`/`unique_id`): `supervisor`, `os`, `core`, `app`, `integration` (marketplace/hacs), `firmware`. Each row: entity, name, kind, installed, latest, `auto_update`, `in_progress`.

**Readiness** (`ready`: `no` when any blocker; `check` when a warning or any `not_checked` entry; `yes` only when every check ran and found nothing). Rows `{code, level, …facts}`; a missing field is `not_checked`, never a healthy default:

| Code | Level | Source and rule |
|---|---|---|
| `SUPERVISOR_UNHEALTHY` | blocker | `/resolution/info` `unhealthy` not empty: Supervisor refuses Core and OS updates. |
| `DISK_LOW` | blocker | `/host/info` `disk_free` < 2 GB: Supervisor's own free-space condition (`MINIMUM_FREE_SPACE_THRESHOLD = 2`) refuses updates. |
| `UPDATE_RUNNING` | blocker | an `update.*` `in_progress`: a second install is rejected. |
| `BACKUP_RUNNING` | warning | `backup/info` `state` is not `idle`. |
| `NO_RECENT_BACKUP` | warning | newest backup older than 24 h, or none. Facts: its age. |
| `BACKUP_ONLY_ON_DEVICE` | warning | the newest backup lives only on `backup.local`/`hassio.local`. |
| `REPAIRS_OPEN` | warning | not-ignored repairs; `homeassistant_alerts` ones listed by title (HA's known-issue alerts for this home's integrations). |
| `APP_NEEDS_NEWER_CORE` | blocker for that App | its store `homeassistant` minimum is newer than the latest Core offered. Newer than the installed Core only: an order rule, not a blocker. |

Not checked (`not_checked: [{check, reason}]`): `supervisor_health`, `disk`, `backups`, `backup_copy`, `repairs`, `app_minimum`, `custom_integrations`.

**Order** (`order_basis: default`: a default, not a proven dependency graph). One list: `backup` first (when Core, OS or an App is pending without a backup from the last 24 hours, or the backup list is unreadable), then Supervisor → OS → Core → Apps (the NOVA App last, it restarts the connection) → integrations (one restart for all) → firmware (long, independent). Every pending item is ordered, auto-update ones too (a pending Supervisor may be Core's prerequisite). An App blocked by `APP_NEEDS_NEWER_CORE` stays out; one whose minimum needs the offered Core carries `after_core: true`. Every App goes after Core, so one rule replaces today's "Apps before Core". A prerequisite the release notes name wins over the default (skill).

**Breaking changes, matched to this home.**

- Source: the official Home Assistant release notes, the "Backward-incompatible changes" section of each monthly release post, read from the website's source repository (`raw.githubusercontent.com/home-assistant/home-assistant.io/current/source/…`). The post's path comes from the first 1 KB of `source/changelogs/core-<YYYY.M>.markdown` (a Range request; its header links the post). Checked for 2025.12 to 2026.11: every post has the section, one `{% details "<title>" %}` block per change, and each integration change carries `[<name> documentation]: /integrations/<domain>/` lines.
- Months: every monthly release after the installed Core's, up to the latest offered (2026.8.3 → 2026.11.1 scans 2026.9, 2026.10, 2026.11), at most 12 (more: a risk row, the newest 12 scanned). A patch-only jump is `not_scanned`; no Core update is `no_core_update`. A beta's month that is not published yet is listed in `missing`.
- Fetch: 10 s per request, a hard 1 MB read cap (a server that ignores Range is still cut at 1 KB for the header), no redirects, no retries. The post path from the header must match `/blog/YYYY/MM/DD/release-<slug>/` and maps to exactly one file under `source/_posts/`. Not readable offline: Home Assistant itself has no Core release notes (`update/release_notes` is not supported on the Core entity).
- Status: `checked` (every month read and parsed), `incomplete` (some months missing, unparsable, or beyond the cap; lists them), `unavailable` (nothing read; the reason and the release URL), `not_scanned` (a patch-only jump: patch releases have no such section; never "no breaking changes").
- Match: a block matches when one of its domains is in this home: config entry domains plus `/api/config` components (both parts of `x.y`). Matched rows: release, title, domains, plain text (Liquid tags reduced to their words, link lines dropped, 600 characters), link to the post's section, and the automations and scripts that use the integration (`search/related` `item_type: integration`, at most 10 domains). Blocks without any integration link (frontend, general changes) are listed too, with their text: they may apply. Integration blocks for integrations not in this home are a count. A match is a filter, not proof that unmatched changes can't matter.
- Offline and deterministic: repairs with `breaks_in_ha_version` at or below the latest Core offered are listed as `repairs_breaking` (Home Assistant's own deprecation repairs for this home).
- Release text is data: every title and text is defanged (`sayTextUpTo`: no Markdown, links, or controls) and the skill tells the model to quote it, never follow it.

**Risk flags** `{code, …facts}`: `CORE_SKIPS_RELEASES` (two or more monthly releases), `MAJOR_VERSION` (OS, App or integration whose first version number grows), `CUSTOM_INTEGRATIONS` (non-built-in integrations from `manifest/list` when Core changes month; they may lag behind), `ACCESS_DEVICE_FIRMWARE` (a firmware update on a device that also has a lock, alarm panel, or a garage, gate or door cover).

**Schedule** stays advice in the skill text (a time when someone is home, no backup running); `BACKUP_RUNNING` is its only computed input.

**Baseline** (phase 2). The run saves `<cache dir>/updates-baseline-<server>.json` (0600, written atomically) unless one younger than 12 hours exists, so a second check during an update session (or another person's) keeps the pre-update evidence. It holds: time, each update entity's installed and offered version, open repair ids, Supervisor resolution issue ids, config entries not `loaded`/`not_loaded`, unavailable enabled entities. The result says `baseline: saved | kept | not_saved` with the time.

### Phase 2: `after_update`

Reads: states, registry, config entries, repairs, `trace/list` (automations), `/resolution/info`. Compares with the baseline:

- `updated` (installed version changed; `reached: true` when it is the version offered at the check), `still_pending`;
- `rollback`: a Supervisor `update_rollback` issue not in the baseline (Supervisor `homeassistant/core.py:500-503`, reference = the version that failed); `update_failed` issues not in the baseline are `failed`, not a rollback. Supervisor unreadable: `rollback: unknown`. Said plainly by the skill;
- `new_repairs`, `new_failing_integrations` (entry newly in `setup_error`, `setup_retry`, `migration_error`, `failed_unload`), `newly_unavailable` (count and up to 20 names), `available_again` (count);
- `automation_errors`: automations with a stored run that ended with an error after the baseline time (count and last error per automation). Home Assistant keeps few runs per automation, so the skill says "no errors in the stored runs", not "all automations work".

No baseline (or one for another server): rollback and current failures only, `baseline: null`. The baseline is not deleted; the next `updates` run replaces it, so the diff is always against the last check.

## Skill

`skills/updates/SKILL.md` points to a new reference, `skills/updates/advisor.md`: run `--section updates` before every install (one update or a batch) and present readiness (blockers stop, with what fixes them), what could not be checked, matched breaking changes in plain words with the affected automations, risk flags, and the order. Never "safe to update"; `ready: yes` is "no blockers found in the checks HA NOVA made". After each verified install, run `--section after_update` and report. Today's confirmations, gates and preview text stay as they are; the advisor only adds information and order. The SKILL.md word budget (1750) stays: the wrong order rule makes room for the pointer.

## Known limits

- Breaking changes need internet on the computer running HA NOVA (the App, for chat apps). Patch releases are not scanned. App, OS and integration notes stay with the existing release-notes read.
- A block mentioning an integration only in prose, not as a documentation link, is not matched (it counts as unmatched).
- No check that a backup decrypts; the system check-up feature covers backup confidence.
- The baseline is one per server: two people checking at the same time share it.
- No Core configuration check after the update (failing YAML shows as failing integrations and repairs).

## Delivery

Three stacked PRs into release/0.31, each self-contained with its tests and skill text:

1. Readiness and order (`verb_map_updates.go`, `verb_map_updates_ready.go`, the read allowlist, `advisor.md`).
2. Breaking changes and risk flags (`verb_map_updates_notes.go`, `verb_map_updates_risk.go`).
3. After the update: baseline, diff, rollback (`verb_map_updates_after.go`).

## Review (before build)

- KISS (claude-sonnet-5-5, medium): agrees with the shape (a `map` section, Go-only logic, no Relay change) and asked for a split into three PRs. Taken: the split; dropped `SUPERVISOR_UNSUPPORTED`, `DISK_TIGHT` and `BETA`; `available_again` is a count. Not taken: dropping `search/related` (the requirement names the affected automations), the release text (a chat app can't open the link, so the model couldn't explain the change), `ACCESS_DEVICE_FIRMWARE` and `CORE_SKIPS_RELEASES` (both named requirements), `BACKUP_RUNNING` (the schedule advice's only computed input).
- Astra (gpt-6-astra, medium): agrees with 1–3 (a `map` section; the official source and its method; the narrow Supervisor reads), disagrees with 4–5 as first drafted. Taken: `ready: yes` only when every check ran, `not_checked` listed, "no blockers found" wording; Supervisor read errors are not checked, absence only from the `hassio` component; exact payloads; the order labelled a default, auto-update items ordered; readiness before every install; scan states `incomplete`/`not_scanned`, no redirects, a hard size cap, a fixed post path pattern, domainless changes kept with text; the baseline kept for 12 hours, atomic, with resolution issue ids and offered versions; `update_failed` is not a rollback; "stored runs" wording.

## Tests

Go: the section parse and refusals; the Supervisor read case (allowed paths only, `GET` only); readiness codes from fixtures (incl. no Supervisor); order; release post parse and match on a recorded post fixture (httptest server); month range across a year and the 12 cap; fetch failure → `unavailable`; baseline save/compare; rollback detection. Skill contract: the advisor steps and the unchanged confirmation lines. Test bed: both sections on throwaway Home Assistant, plus one headless agent run of the skill.
