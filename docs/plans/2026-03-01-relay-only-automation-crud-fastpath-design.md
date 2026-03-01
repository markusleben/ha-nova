# Relay-Only Automation CRUD Fast Path (Design)

Date: 2026-03-01  
Status: Proposed/Implemented in skills + contracts

## Goal

Optimize automation `create`/`update`/`delete` flow for relay-only user sessions with minimal runtime overhead.

## Constraints

- Keep App + Relay model (no client-side LLAT path for end users).
- Keep one best-practice refresh gate per session for `create`/`update`.
- Keep write safety: preview + explicit confirmation.
- Minimize API/tool round-trips and avoid proactive preflight checks.

## Fast-Path Flow

### Session rule

- No proactive `ready`, `doctor`, `/health`, or broad list/get preflight before write planning.
- Only run diagnostics after an actual write-path failure.

### Create / Update (relay-only)

1. Ensure best-practice session gate:
   - if `automation_bp_refreshed=true` in session context -> reuse, no refresh call.
   - else perform one refresh and record timestamp/sources.
2. Build automation payload once.
3. Call `validate_config` (`operation=create|update`) to get `validated_hash`.
4. Show preview from validated payload.
5. Ask explicit confirmation once.
6. Call `flow_execute` (`operation=create|update`, `validated_hash`, `consent:true`, `verify:false`).
7. Return write result (`id`, optional `entity_id`) without extra read-back calls by default.

Nominal runtime: **2 write-path calls + 1 user confirmation**.

### Delete (relay-only)

1. Resolve exact automation id (only if ambiguous).
2. Call `validate_config` (`operation=delete`) to get `validated_hash`.
3. Show delete preview.
4. Ask explicit confirmation once.
5. Call `flow_execute` (`operation=delete`, `validated_hash`, `consent:true`, `verify:false`).
6. Return delete result without extra verification calls by default.

Nominal runtime: **2 write-path calls + 1 user confirmation**.

## Error Policy

- Refresh gate failure on `create`/`update` blocks write.
- Return structured failure (`what_failed`, `why`, `next_step`).
- Do not add fallback write path that bypasses refresh gate.

## Files To Update

- `skills/ha-nova.md`
  - add relay-only automation CRUD fast-path routing/runtime policy.
- `.agents/skills/ha-nova/SKILL.md`
  - mirror orchestrator routing for installed skill users.
- `skills/ha-automation-crud.md`
  - replace contributor-first CRUD path with relay-only minimal-call flow.
- `skills/ha-automation-best-practices.md`
  - tighten once-per-session refresh reuse semantics.
- `tests/skills/ha-nova-skill-contract.test.ts`
  - assert relay-only fast-path language + once-per-session gate text.

## Verification

- `npm test -- tests/skills/ha-nova-skill-contract.test.ts`
