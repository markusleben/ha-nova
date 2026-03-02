# HA NOVA Automation Best Practices

Tiered policy for automation writes.

## Snapshot Policy

- Snapshot file: `${HOME}/.cache/ha-nova/automation-bp-snapshot.json`
- Snapshot fields:
  - `automation_bp_refreshed`
  - `automation_bp_refreshed_at`
  - `automation_bp_sources`
  - `automation_bp_ha_version`
- Stale threshold: 30 days.

## Tiered Gate

- Simple automation:
  - Complexity under 3 triggers and under 3 actions.
  - Stale/missing snapshot => warning only.
  - Continue with advisory note in preview.
- Complex automation:
  - 3+ triggers or 3+ actions.
  - Stale/missing snapshot => hard gate.
  - Main thread refresh required before apply.

## Refresh Scope (minimum)

Refresh step checks:
- run modes and overlap behavior
- trigger/condition modeling guidance
- automation services + tracing guidance
- release notes affecting automation semantics

## Enforcement Checklist

1. `mode` is explicit.
2. Trigger model deterministic.
3. Prefer `entity_id` over `device_id`.
4. Avoid templates when native primitives exist.
5. Add guard conditions for noisy sensors.
6. Avoid long blocking delays when helper/timer fits.
7. Promote reusable action chains to script/helper.
8. Require read-back after write.
9. Verify expected vs observed before success message.
10. Use reload only as recovery path.

## Failure Semantics

- Snapshot refresh failure on simple automation: continue with warning.
- Snapshot refresh failure on complex automation: block apply.
- Return structured failure with:
  - what failed
  - why
  - next concrete step
