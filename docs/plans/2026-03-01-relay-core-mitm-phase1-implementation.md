# 2026-03-01 Relay `/core` MITM Phase 1 Implementation

## Scope
- Implement Relay `POST /core` transport for Home Assistant REST calls.
- Keep relay-side validation minimal (`method`, relative `/api/...` path, path hygiene).
- Remove automation CRUD allowlist gating from relay runtime and skill docs.
- Align live E2E harness/contract to end-user relay flow (no client-side `HA_LLAT` requirement).

## Constraints
- Relay remains dumb and transport-focused (no automation business logic).
- Skills remain smart and own workflow/best-practice guidance.
- Keep existing auth model: relay uses App-side LLAT (`ha_llat`) for upstream calls.

## Deliverables
- Runtime:
  - `/core` handler wired in app bootstrap.
  - HA REST client for upstream forwarding.
- Tests:
  - `/core` handler contract tests (success, validation, upstream error).
  - bootstrap wiring tests for route and injected client.
- Skills/E2E:
  - `ha-automation-crud` updated to relay passthrough semantics.
  - live Codex E2E harness + contract updated to relay-only CRUD evidence.

## Verification
- `npm run typecheck`
- `npm test`
