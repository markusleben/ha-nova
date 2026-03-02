# HA NOVA Core Contracts (MVP)

## Response Contract (Domain-First)

Default write preview/result output uses domain fields, not orchestration internals.

Automation fields:
- `Automation Name`
- `Automation Goal`
- `Entities Used`
- `Behavior Summary`
- `Next Step`

Script fields:
- `Script Name`
- `Script Goal`
- `Inputs/Variables`
- `Actions/Entities Used`
- `Next Step`

Failure/debug format:
- `Error`
- `Cause`
- `Fix Next Step`

## Safety Contract

- Always run `preview -> confirm:<token> -> apply -> verify` for writes.
- Never accept free-text confirmation.
- Keep orchestration internals hidden unless failure/explicit request.
- Ask at most one blocking question when ambiguity remains unresolved.

## Confirmation Token Contract

- Confirmation token format remains `confirm:<token>`.
- Token TTL: default 10 minutes from preview render time.
- Token is one-time-use only; replay must hard-fail.
- Token must be bound to:
  - write method/path/target
  - preview digest (content hash)
- On stale/replay/mismatch token:
  - hard-fail write
  - regenerate preview
  - issue a fresh token

## Verification Contract

- `Changes applied` only when write verification succeeded.
- `create`/`update`: read-back verification required.
- `delete`: `200`/`204` accepted by default.
