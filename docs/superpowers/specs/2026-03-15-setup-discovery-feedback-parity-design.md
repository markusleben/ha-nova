# Setup Discovery Feedback Parity

## Goal

Bring the Go setup discovery and host-validation feedback up to the old release UX bar without adding a heavier network-scanning system.

## Scope

- Keep the current candidate-based discovery model.
- Add clearly visible progress feedback during discovery.
- Add clearly visible progress feedback during Home Assistant host validation.
- Keep the chosen host canonical for the rest of setup.

## Non-Goals

- No subnet scanner.
- No SSDP/UPnP/Bonjour expansion beyond the current discovery inputs.
- No new background services or platform-specific helpers.

## Design

### 1. Discovery feedback

- `ha-nova setup` should show an actual spinner in interactive terminals while discovery runs.
- Even when discovery finishes very quickly, the user should still get a short, visible progress phase before the result line.
- The result stays explicit:
  - `Found Home Assistant candidate: ...`
  - or `No confirmed Home Assistant found automatically; defaulting to homeassistant.local`

### 2. Host validation feedback

- After the user enters a Home Assistant address, setup should visibly check that address before moving on.
- The old release used a spinner for `Checking connection to Home Assistant...`; the Go flow should do the same.
- Non-interactive output should stay plain and stable.

### 3. Keep KISS

- Reuse one small spinner helper for both discovery and host validation.
- Do not add active subnet probing.
- Do not second-guess an explicit user-provided host or URL.

## Verification

- Unit test discovery result output.
- Unit test host-validation output contains the explicit connection-check label.
- Unit test fast discovery still spends a visible minimum duration in TTY mode.
- Full `go test ./...`
- Full `npm run verify`
