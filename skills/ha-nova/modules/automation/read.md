# Automation Read Module

Use blocks:
- `B0_ENV` + `B1_STATE_SNAPSHOT` + `B2_ENTITY_RESOLVE`
- add `B3_ID_RESOLVE` only for single-item read flows (not for list)

## Purpose

List/read automations through relay paths without write intent, including single-item reads.

## Rules

- Prefer one-shot reads.
- No write gate, no confirm token.
- Keep response compact.
- `automation.list`: do not require `B3_ID_RESOLVE`.
- `automation.read` (single item): require `B3_ID_RESOLVE`.
