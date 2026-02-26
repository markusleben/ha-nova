# Phase 1b Acceptance Matrix

Date: 2026-02-25  
Branch: `feat/phase-1b-rest-skills`

## Skill Deliverables

| Check | Evidence | Status |
|---|---|---|
| `ha-entities` exists | `skills/ha-entities.md` | PASS |
| `ha-control` exists | `skills/ha-control.md` | PASS |
| `ha-automation-crud` exists | `skills/ha-automation-crud.md` | PASS |
| `ha-automation-control` exists | `skills/ha-automation-control.md` | PASS |

## Bootstrap Consistency

| Check | Evidence | Status |
|---|---|---|
| Bootstrap catalog references Phase 1b skills | `skills/ha-nova.md` active skill catalog | PASS |
| Routing rules include 1b automation flows | `skills/ha-nova.md` routing rules 4-5 | PASS |
| LLT remains mandatory | `skills/ha-nova.md` required inputs include `HA_TOKEN` as LLT | PASS |

## Safety and Flow Quality

| Check | Evidence | Status |
|---|---|---|
| Entity resolution before writes | `ha-control`, `ha-automation-control`, `ha-automation-crud` | PASS |
| Preview + confirmation before writes | all write-oriented skills | PASS |
| Verify-after-execute guidance | control and automation skill flows | PASS |

## Notes

- Phase 1b intentionally adds no Relay endpoints.
- Trace/diagnostics remain Phase 1c+ scope.
