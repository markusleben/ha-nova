# Generic WS Event Collection Spec

Status: active

Date: 2026-06-21

## Goal

Replace the Relay's `system_health/info` special case with a generic bounded WS event-collection envelope so Skills own domain-specific behavior and the Relay remains a transport bridge.

## Behavior

- Plain `/ws` remains unchanged for normal request/response messages:
  `{"type":"config_entries/get"}`.
- Event collection is opt-in through:
  `{"message":{"type":"system_health/info"},"collect_events":{"until_type":"finish","max_events":100,"timeout_ms":10000}}`.
- Relay only forwards `message`, collects events until `until_type`, and enforces bounds.
- Relay does not inspect health semantics or parse event payloads.
- Infinite subscription/live-update WS types remain rejected.

## Compatibility

- Bump Relay App to `0.2.3`.
- `ha-nova:health` requires Relay App `0.2.3` or newer for `system_health/info`.
- Older relays still provide repairs, integrations, config, components, and states; Health skips only System Health details.

## Verification

- Update WS proxy tests for plain passthrough, generic collection, bounds validation, and subscription blocking.
- Update Health skill contracts and architecture docs.
- Run focused tests, safe-core, docs verify, full verify, and dev sync.
