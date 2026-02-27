# Single-Secret Client Model (Relay-Only User Auth)

Date: 2026-02-27

## Goal

Remove end-user LLAT duplication across client and Relay.

Target model:
- Client stores only relay auth token.
- LLAT is configured only in App option `ha_llat`.
- End-user skill flow is Relay-first, without client-side LLAT handling.

## Scope

1. Onboarding:
   - remove LLAT prompt/storage from macOS setup.
   - keep strict `doctor` failure on Relay WS degradation.
   - keep guidance focused on App option `ha_llat` + App restart.
2. Runtime env export:
   - export `HA_HOST`, `HA_URL`, `RELAY_BASE_URL`, `RELAY_AUTH_TOKEN` only.
3. Skills/docs:
   - remove active end-user guidance that requires client-side `HA_LLAT`.
   - keep Supervisor and direct REST paths contributor-internal only.
4. Safety:
   - delete legacy `ha-nova.ha-llat` keychain entry during setup migration.

## Notes

- This change intentionally optimizes end-user onboarding UX first.
- Contributor tooling can still use explicit LLAT for development checks when needed.
