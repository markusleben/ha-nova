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

## Automation Mode Selection

Pick the right `mode` based on the automation's behavior:

| Mode | When to use | Example |
|------|-------------|---------|
| `single` | Only one instance should ever run. Re-trigger while active is ignored. | Garage door open/close sequence |
| `restart` | Re-trigger should cancel the current run and start fresh. Best for motion lights with timeout. | Motion → light on → wait 2 min → light off. New motion restarts the timer. |
| `queued` | Each trigger should run in order, one after another. Use `max` to cap queue depth. | Sequential lock/unlock commands |
| `parallel` | All triggers run independently at the same time. Use `max` to cap concurrency. | Per-room climate adjustment via a single automation |

**Default is `single`** — always set mode explicitly so intent is clear.

**Common mistake:** Using `single` for motion lights. The light turns on, but re-triggering during the wait period is ignored — the light turns off too early. Use `restart` instead.

## Enforcement Checklist

1. `mode` is explicit.
2. Trigger model deterministic.
3. Prefer `entity_id` over `device_id`.
4. Avoid templates when native primitives exist (see `template-guidelines.md` → Decision Tree).
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
