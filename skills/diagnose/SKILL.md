---
name: diagnose
description: Use when diagnosing why a Home Assistant automation, script, device, or integration failed or misbehaved — root-cause analysis from error logs, system logs, traces, and bounded history through HA NOVA Relay. For current home status use ha-nova:health; for plain timelines use ha-nova:history.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay App in Home Assistant.
---

# HA NOVA Diagnose

## Scope

Root-cause a concrete failure: "why did X not run", "why did X misbehave", "what broke last night".

- Evidence sources: automation/script traces, HA error log, system log, bounded logbook/history windows, template probes, integration diagnostics.
- Boundary: `ha-nova:health` reports CURRENT home status (repairs, unavailable entities); `ha-nova:history` answers plain timeline questions; `ha-nova:review` audits config quality without a concrete incident. This skill starts from a concrete symptom.
- The ONLY mutation in this skill is a temporary `logger.set_level` debug escalation (see Flow step 5) — every other fix hands off to the owning skill (`ha-nova:write`, `ha-nova:helper`, `ha-nova:service-call`).

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based relay requests only:
- `ha-nova relay core --method GET --path <PATH>` for REST reads (`--out <result-file>` for large output)
- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova trace latest <entity_id> --json` / `ha-nova trace list <entity_id> --json` / `ha-nova trace get <entity_id> <run_id> --json` for run traces (positional entity, `automation.<id>` or `script.<id>`)
- `--jq-file <filter-file>` for non-trivial filters

## Evidence Sources

| Source | Call | Notes |
|--------|------|-------|
| Run traces | `ha-nova trace latest <entity_id> --json` (also `trace list <entity_id>`, `trace get <entity_id> <run_id>`) | First stop for automation/script failures: shows the exact step, condition results, and variables |
| Error log | `GET /api/error_log` | Plain text, NOT JSON — always use `--out <result-file>` and search it natively; never dump it whole |
| System log | WS `{"type":"system_log/list"}` | Structured warnings/errors with counts and sources |
| Logbook window | `GET /api/logbook/<ISO-start>?end_time=<ISO-end>&entity=<entity_id>` | Human-readable events around the incident time |
| State history | `GET /api/history/period/<ISO-start>?end_time=<ISO-end>&filter_entity_id=<ids>` | What states the involved entities actually had |
| Template probe | `POST /api/template` with `{"template":"..."}` | Evaluate the exact condition/template against live state |
| Integration diagnostics | WS `{"type":"diagnostics/list"}`, then `GET /api/diagnostics/config_entry/<entry_id>` | Deep integration state when an integration itself is the suspect |

Recency honesty: `error_log` and `system_log/list` only cover the time since the last Core restart. For older incidents use logbook/history windows, and say explicitly that live logs no longer reach back that far.

## Flow

1. Pin the symptom: which entity/automation/script, what expected vs observed behavior, and WHEN (ask one question if the incident time is unknown — every later window depends on it).
2. For automation/script symptoms, read the trace first (`ha-nova trace latest <entity_id> --json`; older runs via `trace list <entity_id>` + `trace get <entity_id> <run_id>`). The trace usually answers it: triggered or not, which condition stopped it, which step errored.
3. Correlate logs: search the error log and `system_log/list` output for the involved entities, integrations, and the incident window. Quote only the relevant lines.
4. Reconstruct context: bounded logbook + history windows (default ±30 minutes around the incident) for the involved entities; probe suspect conditions/templates with `POST /api/template` against live state.
5. Debug escalation (optional, the only mutation): if evidence is insufficient and the user agrees, raise one integration's log level via service `logger.set_level` (payload like `{"homeassistant.components.<domain>":"debug"}`). Preview the exact payload, get confirmation, and ALWAYS pair it with the reset step: after reproducing, restore the default level the same way (`"warning"`) — a forgotten debug level floods the log. State plainly that the setting lasts until the next restart.
6. Conclude with Claim-Evidence Binding: name the root cause only when the evidence shows it (trace step, log line, state sequence). Otherwise present the ranked hypotheses, each with its evidence and the one probe that would decide it.
7. Hand off the fix: config changes -> `ha-nova:write` / `ha-nova:helper`; a one-off corrective action -> `ha-nova:service-call`; systemic issues (repairs, unavailable entities) -> `ha-nova:health`.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Diagnose specifics:
- `/api/error_log` returns plain text: a JSON parse failure on it is expected, not an error — read it as text.
- Missing traces (empty `trace list`): traces are kept per entity with a small cap and reset on reload/restart — say so instead of guessing.
- `system_log/list` empty after a restart is normal; fall back to logbook/history windows.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Diagnosis-first: lead with the root cause (or ranked hypotheses), then the evidence chain (trace step / log lines / state sequence, each tied to its source and timestamp), then the recommended fix with the owning skill. Quote log lines sparingly — never dump raw logs.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- The debug escalation (`logger.set_level`) is this skill's single declared mutation: preview + natural confirmation, and always schedule the reset back to the default level in the same interaction.
- Never "fix" anything from here — fixes hand off to the owning skill after the diagnosis.

## Guardrails

- Bounded reads only: logbook/history windows default to ±30 minutes; widen stepwise on request, never unbounded.
- Search the error log natively from the saved file; never print more than the relevant lines.
- Do not run diagnostics proactively — this skill starts from a user-reported symptom.
- Never leave a raised log level behind: no reset offer, no escalation.
