# Health empty results spec

Status: active

Date: 2026-06-21

## Problem

Live testing showed two empty/unusable health surfaces:

- `GET /api/error_log` returned upstream status 404 with body `404: Not Found`.
- WS `system_health/info` returned successful relay output with `data: null`.

`repairs/list_issues` worked through the same WS relay path, so the empty system-health result is not a general Relay/WebSocket failure.

## Findings

Home Assistant's `system_health/info` command confirms the request with an empty result, then sends the actual payload as event messages and stores a subscription for follow-up updates. HA NOVA Relay is intentionally request/response-only and does not deliver event streams from WS calls.

Therefore `system_health/info` cannot produce useful data through the old relay contract. Treating its `null` ack as a best-effort read is misleading.

The official REST documentation still lists `/api/error_log`, and current Home Assistant Core still defines `APIErrorLog`, but the route is file-backed. On the live HA OS instance it returned 404. This makes it unreliable as a modern Home Status signal.

## Change

- Collect finite `system_health/info` event responses through the generic bounded WS `collect_events` relay envelope and return them as normal JSON.
- Update `ha-nova:health` to parse `data.events` for `initial`, `update`, and `finish` events.
- Gate System Health event reads on Relay App version 0.2.3 or newer.
- Remove `/api/error_log` from `ha-nova:health`.
- Update docs and tests so empty system-health results are not treated as usable data.

## Verification

- Targeted WS-proxy test for generic event collection.
- Targeted health/calendar skill contract tests.
