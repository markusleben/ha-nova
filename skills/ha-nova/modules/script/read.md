# Script Read Module

Use blocks:
- `B0_ENV` + `B1_STATE_SNAPSHOT` + `B2_ENTITY_RESOLVE`
- add `B3_ID_RESOLVE` only for single-item read flows (not for list)

## Purpose

Read/list script data without write intent, including single-item reads.

## Rules

- Keep request path one-shot where possible.
- Keep response compact and domain-first.
- `script.list`: do not require `B3_ID_RESOLVE`.
- `script.read` (single item): require `B3_ID_RESOLVE`.
