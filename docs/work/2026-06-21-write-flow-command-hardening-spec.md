# HA NOVA Write Flow Command Hardening Spec

Status: active

## Goal

Remove avoidable command/path ambiguity found during the live consent-test write flow.

## Changes

- Make `ha-nova relay ws --data-file` the only valid WS request-body pattern in skill docs.
- Clarify that inline `--body` applies only to tiny `relay core` diagnostics, never to WebSocket relay calls.
- Prefer `config/entity_registry/get` for known automation/script entity IDs when resolving `unique_id`.
- Keep `config/entity_registry/list_for_display` only for search/disambiguation.
- Compare expected vs observed configs structurally after normalization, not as raw JSON strings whose key order can differ.

## Verification

- Update skill contract tests for the CLI flag, unique-id path, and order-insensitive compare guidance.
- Run focused skill contracts, docs verification, safe-core, and dev sync.
