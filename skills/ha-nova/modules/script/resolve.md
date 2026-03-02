# Script Resolve Module

Use blocks:
- `B1_STATE_SNAPSHOT`
- `B2_ENTITY_RESOLVE`
- `B3_ID_RESOLVE`

## Purpose

Resolve script target and related entities before script operations.

## Rules

- Use one shared state snapshot.
- Resolve config/runtime identifiers before write/read.
- Ask one blocking question only if ambiguity remains.
