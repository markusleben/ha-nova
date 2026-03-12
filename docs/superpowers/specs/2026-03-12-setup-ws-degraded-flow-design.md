# Setup WS-Degraded Flow Design

## Problem

`ha-nova setup` can currently end with `Setup complete!` even when the relay is reachable but upstream Home Assistant WebSocket connectivity is still degraded (`ha_ws_connected=false` and `/ws` probe fails).

This creates two UX problems:

- the final success banner overstates the real system state
- setup pushes the user to `ha-nova doctor` too early instead of helping inside setup

## Decision

- Keep setup permissive enough to save config and installed skills.
- Do not show `Setup complete!` when the relay-to-HA WS path is still degraded.
- Reuse the existing retry style from the relay reachability path:
  - `[Enter] retry`
  - bounded retries
  - save config if the user still exits
- End in `Setup incomplete` when relay health is reachable but HA WS is not healthy after retries.
- `ha-nova doctor` remains a later re-check command, not the primary setup recovery path.

## Messaging Rules

- Only claim a concrete `ha_llat` problem when the `/ws` probe response explicitly proves it.
- Otherwise describe the state generically:
  - relay reachable
  - Home Assistant WebSocket not connected yet
- Final incomplete banner must still tell the user what to do next and mention `ha-nova doctor` as a later follow-up command.

## Implementation Scope

- Update doctor degraded messaging to remove the unconditional `ha_llat is required` statement.
- Update setup verification flow to retry degraded WS connectivity before finishing.
- Track final setup state so the banner can show `Setup complete!` vs `Setup incomplete`.
- Add regression coverage for:
  - degraded WS during setup
  - doctor messaging for generic degraded WS
