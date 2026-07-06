# Home Status And Calendar Skills Spec

Status: active

## Problem

System health/repairs and calendar reads are currently fallback-owned even though both are common, read-only Home Assistant tasks with stable API surfaces. Fallback keeps them experimental and mixes them with unsupported admin work.

## Scope

- Add `ha-nova:health` as a read-only Home Status skill.
- Add `ha-nova:calendar` as a REST-only calendar read skill.
- Update context dispatch, fallback ownership, skill architecture, API matrix, docs inventory, and safe-core tests.

## Home Status

Read-only only:
- REST `/api/config`
- REST `/api/components`
- REST `/api/states`
- WS `config_entries/get`
- WS `repairs/list_issues`
- WS `system_health/info` through generic bounded `collect_events` when Relay App 0.2.3+ is deployed; older relays degrade to unavailable details

Summaries:
- repairs/deprecation issues
- integration entries that are not loaded
- config/components summary
- unavailable/unknown entity summary
- low-battery summary

No repair/fix/ignore actions. No restart/reload/service calls.

Home Status output stays compact:
- include `Overall: ok | attention | limited`
- include checked timestamp and source coverage
- normalize relay shapes before summary: REST core uses `.data.body`, repairs use `.data.issues`, integrations use `.data`, system health uses `.data.events`
- require defensive type checks before indexing arrays or objects
- cap examples: top 3 repairs, top 5 integration issues, top 5 entity examples, max 3 system-health highlights
- show raw unavailable/unknown counts, but deprioritize noisy/stateless domains (`button`, `event`, `scene`, `stt`) in examples
- sanitize integration error reasons; do not print IPs, hostnames, tokens, URLs, or long raw errors by default
- treat config entries with `disabled_by` set as intentionally disabled context, not attention items
- label low values as battery/SOC unless the entity is clearly a replaceable device battery

## Calendar

REST-only:
- `GET /api/calendars`
- `GET /api/calendars/{entity_id}?start=<timestamp>&end=<timestamp>`

Rules:
- default event window: next 7 days
- always bounded
- list calendars before querying by ambiguous name
- summarize events, do not dump raw JSON
- no event create/update/delete in this skill

## Acceptance

- New skills are English-only markdown and under the line budget.
- Context skill dispatch routes home-status and calendar intents to the new skills.
- Fallback no longer owns System Health/Repairs or Calendar Queries as relay-ready features.
- `docs/reference/skill-architecture.md`, `docs/reference/ha-api-matrix.md`, and docs checks reflect 14 skill directories.
- Safe-core includes focused contract coverage for both skills.
