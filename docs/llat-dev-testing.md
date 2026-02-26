# LLAT Dev/Test Workflow

Date: 2026-02-26

## Objective

Avoid manual LLAT re-entry after each dev deployment while keeping full-scope API/WS test coverage.

## Token Resolution Order

1. `HA_LLAT` (local env; preferred for local dev and CI)
2. app option `ha_llat` (persistent in `/data/options.json`)
3. legacy `HA_TOKEN` (fallback only when `BRIDGE_AUTH_TOKEN` is set separately)
4. `SUPERVISOR_TOKEN` (limited mode)

## Bridge Auth vs Upstream Auth

- Bridge endpoint auth:
  - `BRIDGE_AUTH_TOKEN` preferred
  - fallback: `HA_TOKEN`
- Upstream legacy LLAT fallback:
  - only active when `BRIDGE_AUTH_TOKEN` is set and `HA_TOKEN` is also set
  - this avoids accidental coupling where one token silently drives both concerns

## Seed LLAT Once Into App Options

Prerequisites:
- `SUPERVISOR_TOKEN` available in runtime
- LLAT string available from user profile in Home Assistant

Command:

```bash
npm run seed:llat -- '<YOUR_LLAT>'
```

Optional:

```bash
ADDON_SLUG=self SUPERVISOR_URL=http://supervisor npm run seed:llat -- '<YOUR_LLAT>' --dry-run
```

Behavior:
- Reads current options
- Merges `ha_llat`
- Validates merged options via Supervisor API
- Writes only when value changed

## Runtime Start

```bash
npm run dev
```

Runtime bootstrap flow:
1. load env
2. read `/data/options.json` (or `ADDON_OPTIONS_PATH`)
3. resolve upstream token source
4. create app with allowlist and auth
5. start HTTP server

## Test Matrix

1. Unit tests (`resolveUpstreamToken`): source precedence, warnings, empty input handling
2. Unit tests (`loadEnv`): optional `HA_LLAT` and `SUPERVISOR_TOKEN`
3. Integration tests (`/ws`, `/health`): unchanged behavior
4. Optional live smoke test: set LLAT once with seed script, then deploy/restart and verify token remains active
