# Dashboard / Organize / History Parity Expansion

Date: 2026-04-05

## Summary

Expand the promoted skills to cover the next capability tier:

- `dashboard`
  - Lovelace resource inventory + CRUD
  - dashboard structure inventory
  - targeted card add/update/move/delete inside existing views
- `organize`
  - rich metadata for areas, floors, labels, categories
  - richer entity/device metadata updates, including aliases and label add/remove flows
- `history`
  - bounded long-term statistics for trend questions

## Decisions

- Keep the HA NOVA UX shape:
  - preview first
  - verify by readback
  - exact token only for top-level destructive deletes
- Stay storage-only for dashboard writes.
- Do not introduce view CRUD.
- Do not invent new custom-card schemas.
- Keep `history` read-only; add statistics, not logs.
- Keep all examples generic; avoid personal or niche entity naming in active skill/docs/harness surfaces.

## Verification

- Extend the promoted live harness instead of creating a second harness.
- Add live proofs for:
  - dashboard card flow
  - dashboard resource flow
  - organize rich metadata flow
  - history statistics flow
- Preserve mandatory cleanup of HA fixtures and local temp artifacts, even on failed runs.
