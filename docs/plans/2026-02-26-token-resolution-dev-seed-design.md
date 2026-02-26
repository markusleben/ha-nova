# Token Resolution + Dev Seeding (Phase 1a.1)

Date: 2026-02-26  
Status: approved-for-implementation

## Goal

Enable full-scope Home Assistant access with LLAT while keeping dev/test ergonomics high.

## Scope

1. Add a deterministic upstream token resolver with explicit priority.
2. Extend environment parsing for LLAT/supervisor token inputs.
3. Add an idempotent helper script that persists LLAT in app options through Supervisor API.
4. Add tests for resolver behavior and env parsing.

## Non-Goals

- No new runtime endpoint.
- No bridge transport refactor.
- No secrets manager integration.

## Token Priority

1. `HA_LLAT` (local env; best for dev and CI).
2. App option `ha_llat` (persistent across restarts/updates in `/data/options.json`).
3. Legacy fallback `HA_TOKEN` (compat only; deprecation warning).
4. `SUPERVISOR_TOKEN` fallback (limited capability mode).
5. No token -> restricted mode with explicit warning.

## Expected Behavior

- Resolver returns: selected token, source, capability (`full | limited | none`), warnings.
- Empty/whitespace values are ignored.
- Env parser keeps existing required `HA_TOKEN` behavior for bridge auth compatibility.

## Testing

- Unit tests for every priority branch and warning branch.
- Unit tests for env parser reading optional `HA_LLAT` and `SUPERVISOR_TOKEN`.
- Full suite verification: `npm test && npm run typecheck && npm run build`.

## Dev Seeding Strategy

- Script validates input and `SUPERVISOR_TOKEN` presence.
- Script validates options via `/addons/<slug>/options/validate` before write.
- Script applies options via `/addons/<slug>/options`.
- Script defaults to `self` slug; override via `ADDON_SLUG`.
- Idempotent intent: repeated run writes same value safely.
