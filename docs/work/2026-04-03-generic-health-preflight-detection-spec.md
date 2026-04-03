# Generic Health Preflight Detection Spec

Date: 2026-04-03

## Goal

Classify deliberate generic-scenario health preflights by the concrete policy violation even when the transcript uses repo-local CLI forms such as `go run . relay health`.

## Change

- Extend the generic scenario harness health-preflight regex to match both:
  - `ha-nova relay health` style commands
  - `go run ... relay health` style repo-local commands
- Keep `/health` detection intact.
- Add a contract test that proves the harness counts:
  - `go run . relay health`
  - `./cli/cli relay health`
  - `scripts/onboarding/bin/ha-nova relay health`

## Verification

- Run `npx vitest run tests/e2e/codex-skill-scenarios-contract.test.ts`
- Rerun `scripts/e2e/codex-ha-nova-scenarios-e2e.sh`
