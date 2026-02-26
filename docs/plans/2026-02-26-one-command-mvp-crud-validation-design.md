# One-Command MVP CRUD Validation Design

## Goal
Provide a single contributor command to validate MVP readiness end-to-end:
- onboarding health (`doctor`)
- App + Relay reachability
- automation CRUD (create/read/update/delete) with deterministic cleanup

## Scope
- add executable script: `scripts/smoke/ha-app-mvp-crud-smoke.sh`
- add npm command: `smoke:app:mvp`
- add contract test for script and npm command wiring
- document the command in contributor deploy loop docs

## Behavior
1. Run `bash scripts/onboarding/macos-onboarding.sh doctor`.
2. Resolve HA SSH target (`HA_HOST`) and key (`HA_SSH_KEY`, default `~/.ssh/ha_mcp`).
3. SSH to HA host and execute CRUD in running App container (`addon_<SUPERVISOR_SLUG>`):
   - create config
   - read config
   - update config
   - read updated config
   - delete config
   - verify config missing (`404`)
4. Always cleanup test automation via shell trap.

## Constraints
- contributor-only path (uses SSH + App container context)
- no token output in logs
- no backward-compatibility launcher behavior

## Verification
- `shellcheck scripts/smoke/ha-app-mvp-crud-smoke.sh`
- `npm test -- tests/app/mvp-automation-crud-smoke-contract.test.ts`
- `npm run smoke:app:mvp`
