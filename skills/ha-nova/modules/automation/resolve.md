# Automation Resolve Module

Use blocks:
- `B1_STATE_SNAPSHOT`
- `B2_ENTITY_RESOLVE`
- `B3_ID_RESOLVE`

## Purpose

Resolve automation targets before write/read operations.

## Rules

- Resolve entities from one shared snapshot.
- Resolve automation config ID and existence branch (`200`/`404`).
- If ambiguous, ask exactly one blocking question.
- Do not proceed to write blocks until resolve is complete.
