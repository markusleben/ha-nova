---
name: ha-automation-best-practices
description: Enforce current Home Assistant automation best practices with a cached refresh snapshot gate before automation writes.
---

# HA Automation Best Practices

## Purpose

Keep automation write decisions aligned with current Home Assistant guidance.
For `create` and `update`, a best-practice refresh snapshot is mandatory.

## Refresh Snapshot Gate (Mandatory)

Apply this gate before any automation `create`/`update` write plan:

1. Check whether a refresh snapshot is already available.
2. Snapshot is valid only if all conditions hold:
   - `automation_bp_refreshed=true`
   - `automation_bp_refreshed_at` is not older than 30 days
   - Home Assistant major/minor version is unchanged since snapshot (if version info is available)
3. If snapshot is missing or stale, run a fresh best-practice research step now.
4. Record snapshot metadata:
   - `automation_bp_refreshed=true`
   - `automation_bp_refreshed_at=<ISO-8601 UTC>`
   - `automation_bp_sources=[...]`
   - `automation_bp_ha_version=<major.minor|unknown>`
5. Persist snapshot for cross-session reuse:
   - `${HOME}/.cache/ha-nova/automation-bp-snapshot.json`
6. Continue only when a valid snapshot is present.

No valid snapshot -> no automation `create`/`update`.

## Research Source Policy

Use current primary sources only:
- Home Assistant docs (`home-assistant.io/docs/...`)
- Home Assistant integration docs (`home-assistant.io/integrations/...`)
- Home Assistant release notes (`home-assistant.io/blog/...`)

Do not base write-critical rules on forum/community posts unless clearly marked as non-authoritative.

## Refresh Scope (Minimum)

Each session refresh must re-check at least:

1. Automation run modes and overlap behavior.
2. Trigger and condition modeling guidance.
3. Automation services (`reload`, `trigger`, runtime control).
4. Troubleshooting/traces guidance.
5. Any release-note changes that affect automation semantics.

## Enforcement Checklist for Create/Update

Validate proposed config against these rules before preview/confirm:

1. `mode` is explicit and justified (`single`/`restart`/`queued`/`parallel`).
2. Trigger model is deterministic; use trigger IDs when multiple triggers exist.
3. Prefer `entity_id` references over `device_id` where feasible.
4. Avoid Jinja templates when native trigger/condition/action primitives exist.
5. Add guard conditions for noisy/flapping sensors.
6. Avoid long blocking `delay` chains when a timer/helper pattern is more robust.
7. Reusable sequences should be promoted to script/helper instead of duplicated actions.
8. Post-write config read-back is required.
9. Verification must include `expected`, `observed`, `match`, and `passed=true` before reporting `Changes applied`.
10. `automation/reload` is recovery-only when stale-state symptoms occur.

## Failure Policy

- If snapshot is stale/missing and live refresh cannot be completed, block automation `create`/`update`.
- Return a structured failure:
  - `what_failed`: refresh step
  - `why`: source fetch/search failure
  - `next_step`: retry refresh with reachable docs sources
- `delete` operations are exempt from the refresh gate but still require preview + confirmation.
