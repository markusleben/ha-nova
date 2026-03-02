# 2026-03-02 HA NOVA Subskills + Lazy Discovery Implementation

## Objective
Refactor HA NOVA skill layout to a router + subskills model that scales with new features while keeping runtime overhead low.

## Scope (MVP)
- Router skill contract in `skills/ha-nova.md`
- Core references in `skills/ha-nova/core/*`
- Automation + script modules in `skills/ha-nova/modules/*`
- Contract tests updated for modular structure and lazy discovery rules
- Keep compatibility via existing top-level skills during migration

## Non-Goals
- Relay runtime code changes
- Home Assistant API surface changes
- Full e2e harness redesign

## Acceptance
1. Router documents lazy discovery map and does not embed full domain logic.
2. Core block catalog exists and is referenced by router and modules.
3. Automation/script modules exist and define fast-pass composition.
4. Contract tests enforce modular structure and domain-first response policy.
5. Skill mirror remains identical and installable.
