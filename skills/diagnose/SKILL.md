---
name: diagnose
description: Use when diagnosing why a Home Assistant automation, script, device, or integration failed or misbehaved — root-cause analysis from error logs, system logs, traces, and bounded history through HA NOVA Relay. For current home status use ha-nova:health; for plain timelines use ha-nova:history.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Diagnose

## Scope

Root-cause a concrete failure: "why did X not run", "why did X misbehave", "what broke last night".

- Evidence sources: automation/script traces, HA error log, system log, bounded logbook/history windows, template probes, integration diagnostics.
- Boundary: `ha-nova:health` reports CURRENT home status (repairs, unavailable entities); `ha-nova:history` answers plain timeline questions; `ha-nova:review` audits config quality without a concrete incident. This skill starts from a concrete symptom.
- The ONLY mutation in this skill is a temporary `logger.set_level` debug escalation (see Flow step 5) — every other fix hands off to the owning skill (`ha-nova:write`, `ha-nova:helper`, `ha-nova:service-call`, `ha-nova:integration-setup` for credential recovery).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

`ha-nova call` returns the preview: confirm it once, then `ha-nova apply --plan <id>`.

File-based relay requests only:
- `ha-nova relay core --method GET --path <PATH>` for REST reads (`--out <result-file>` for large output)
- `ha-nova call <domain.service> --skill ha-nova:diagnose [--set k=v]…` for services (`logger.set_level`)
- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova trace latest <entity_id> --json` / `ha-nova trace list <entity_id> --json` / `ha-nova trace get <entity_id> <run_id> --json` for run traces (positional entity, `automation.<id>` or `script.<id>`)
- `--jq-file <filter-file>` for non-trivial filters

## Evidence Sources

| Source | Call | Notes |
|--------|------|-------|
| Run traces | `ha-nova trace latest <entity_id> --json` (also `trace list <entity_id>`, `trace get <entity_id> <run_id>`) | First stop for automation/script failures: shows the exact step, condition results, and variables |
| System log | WS `{"type":"system_log/list"}` | PRIMARY log source: structured warnings/errors with counts and sources, works on every install type |
| Error log | `GET /api/error_log` | **404 on HA OS/Supervised since 2025.11** (the log file moved to journald) — that is normal, not an error; stay with `system_log/list`. Where the file exists (Container/Core, or re-enabled), see Reading the error log |
| State history + logbook | `ha-nova history <entity_id> --since <incident minus 30 min, RFC3339> --until <incident plus 30 min, RFC3339> [--logbook]` | Summarized states and events (≤ 7 days) |
| Template probe | `ha-nova relay core --method POST --path /api/template --body-file <payload-file>` with `{"template":"{{ ... }}"}` | Evaluate the exact condition/template against live state; the rendered result is the string in `.data.body` |
| Integration diagnostics | WS `{"type":"diagnostics/list"}`, then `GET /api/diagnostics/config_entry/<entry_id>` with `--out <result-file>` | Deep integration state when an integration itself is the suspect |
| Device diagnostics | `GET /api/diagnostics/config_entry/<entry_id>/device/<device_id>` with `--out <result-file>` | One device's slice when a single device misbehaves; resolve the `(entry_id, device_id)` pair via WS `diagnostics/list` (only entries with a device-diagnostics handler) — a device can carry several entry ids |

Diagnostics redaction is integration-controlled, so downloads are potentially sensitive and NEVER hit bare stdout — save with `--out`, extract only relevant fields via `relay jq` before quoting.

Recency honesty: `error_log` and `system_log/list` only cover the time since the last Core restart. For older incidents use logbook/history windows, and say that live logs no longer reach back that far.

### Reading the error log

Only where the log file exists (Container/Core installs; on HA OS/Supervised the user can restore it with `ha core options --duplicate-log-file=true` followed by `ha core rebuild` and `ha core restart`, since 2026.1). The upstream body is plain text, but the relay wraps it in the `/core` envelope — the whole log is one escaped string in `.data.body`, so searching the saved file line-wise does not work.

1. `ha-nova relay core --method GET --path /api/error_log --out <envelope-file>`
2. Filter the lines you need with a raw jq read (never dump the whole log):
   ```text
   ha-nova relay jq -r --file <envelope-file> --jq-file <filter-file>
   ```
   with `<filter-file>`:
   ```jq
   .data.body | split("\n")[] | select(test("<entity_or_integration>"; "i"))
   ```
   `-r` is required for raw text output — `relay core --jq` does not support it, so the filtering happens in this second step. Substitute only an `entity_id`, domain, or integration slug — never a friendly name, which can contain `(`, `)` or `|` and would break the filter. Even a slug is not literal: the `.` in `sensor.kitchen` is a regex wildcard, so escape it (`sensor\\.kitchen`) or the filter also matches `sensor_kitchen` and reports unrelated lines as evidence.

   The identifier is the ONLY selector — never OR a severity into it. `test("sensor\\.kitchen|ERROR")` matches every error line in the log, from every unrelated integration, and each one then looks like evidence for this incident. To narrow by severity, AND it: `select(test("sensor\\.kitchen"; "i") and test("ERROR|WARNING"; "i"))`. And a log with no matching line is a finding in itself — say the log holds nothing about this entity, rather than widening the filter until something appears.

## Flow

1. Pin the symptom: which entity/automation/script, what expected vs observed behavior, and WHEN. A day word ("today", "this morning", "last night", "yesterday") already sets the window: that span in HA's time zone (`GET /api/config` → `time_zone`), ending now at the latest — read the evidence first, and ask only if it is still ambiguous. Ask one question for the time only when nothing bounds it. Resolve names with `ha-nova find "<phrase>" --domain automation` (no `--name` flag).
2. For automation/script symptoms, read the trace first (`ha-nova trace latest <entity_id> --json`; older runs via `trace list <entity_id>` + `trace get <entity_id> <run_id>`). The trace usually answers it: triggered or not, which condition stopped it, which step errored. No run in the window (no trace, `last_triggered` null or older): check its own state first (`off`, `unavailable`). Automation: take its triggers from `ha-nova get <entity_id> --config` (blueprint instance: expand once with `blueprint/substitute`, `../ha-nova/consumer-discovery-preflight.md`) and compare each trigger's full predicate (`from`/`not_from`, `to`/`not_to`, `attribute`, `for:`) against the trigger entity's history for the window — only a matching change means it fired, so check the conditions. No change while the entity sits in the `to:` state → a `state` trigger fires only on a transition, so that is the root cause: name the state and when it changed. Script: it runs only when called — check its callers (`ha-nova get <entity_id> --related`) and the logbook (`ha-nova history <entity_id> --logbook`).
3. Correlate logs: search `system_log/list` (and the error log where it exists) for the involved entities, integrations, and the incident window. Quote only the relevant lines. Align timestamps before claiming causation: the log line must sit within minutes of the trace's failing step — an error far outside that window is context, not the cause; say which it is.
4. Reconstruct context: bounded logbook + history windows (±30 minutes around the incident, or the day-word window) for the involved entities (`ha-nova history --since --until`, above; omit `--until` for a window ending now — a future one is rejected); probe suspect conditions/templates against live state (`relay core --method POST --path /api/template --body-file <payload-file>`, body `{"template":"{{ ... }}"}`; the rendered value comes back in `.data.body`).
5. Debug escalation (optional, the only mutation): if evidence is insufficient and the user agrees, raise one integration's log level.
   - FIRST read the current levels: WS `{"type":"logger/log_info"}` → `[{"domain":..., "level": <numeric>}]`. `logger/log_info` reports NUMBERS but `logger.set_level` only accepts NAMES — map the recorded number back before building any payload: `10` → `debug`, `20` → `info`, `30` → `warning`, `40` → `error`, `50` → `critical`. A numeric level in a `set_level` payload is rejected by Home Assistant, which would leave debug logging raised until the next restart. Record the target logger's mapped level — that is the state you must restore. If it has no entry, the effective level is the configured default (HA's own default is `warning`, but `configuration.yaml` may set another).
   - Escalate: `ha-nova call logger.set_level --skill ha-nova:diagnose --set homeassistant.components.<domain>=debug`.
   - ALWAYS pair it with the reset: after reproducing, rerun it with the recorded level NAME (mapped above), not `debug` — never the raw number, and never a hard-coded `warning`, which would silently overwrite a level the user set on purpose. Home Assistant has no "remove override" service: an override persists until the next restart, so restoring means setting the recorded level back explicitly (or restarting HA for a truly clean state). Say this plainly before escalating.
6. Conclude with Claim-Evidence Binding: name the root cause only when the evidence shows it (trace step, log line, state sequence). Otherwise present the ranked hypotheses, each with its evidence and the one probe that would decide it.
7. Hand off the fix: config changes -> `ha-nova:write` / `ha-nova:helper`; a one-off corrective action -> `ha-nova:service-call`; invalid credentials or a pending reauthentication -> `ha-nova:integration-setup`; systemic issues (repairs, unavailable entities) -> `ha-nova:health`; an ESPHome device's own YAML (pins, removed keys, Wi-Fi/API/OTA) -> `ha-nova:esphome`.

## Error Handling

Full relay/upstream error taxonomy: `../ha-nova/relay-api.md` -> Error Handling. Diagnose specifics:
- `/api/error_log` 404: normal on HA OS/Supervised since 2025.11 — continue with `system_log/list` and mention the `--duplicate-log-file` re-enable only if the user asks for the raw file.
- Missing traces (empty `trace list`): traces are kept per entity with a small cap and reset on reload/restart — say so instead of guessing.
- `system_log/list` empty after a restart is normal; fall back to logbook/history windows.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked, no ids, "entity", modes, Relay/HA URLs, snapshot files, raw JSON. Previews, deletes, and results use the Cards defined there (`../ha-nova/output-cards.md`).

Render the Report shape, Diagnosis specialization (output-cards.md → Report Shape): root cause first, then the evidence chain and the recommended fix (the handoff stays internal). Quote logs sparingly.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing speculative.

- The debug escalation (`logger.set_level`) is this skill's single declared mutation: read the current level via `logger/log_info` first, preview + natural confirmation, and always schedule the restore of THAT recorded level (never a hard-coded default) in the same interaction.
- Never "fix" anything from here — fixes hand off to the owning skill after the diagnosis.

## Guardrails

- Bounded reads only: logbook/history windows default to ±30 minutes (or the day the user named); widen stepwise on request, never unbounded.
- Search the error log natively from the saved file; never print more than the relevant lines.
- Do not run diagnostics proactively — this skill starts from a user-reported symptom.
- Never leave a raised log level behind: no recorded pre-level and no restore plan means no escalation.
