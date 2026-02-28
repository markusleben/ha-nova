# LLAT Dev/Test Workflow

Date: 2026-02-26

## Objective

Avoid manual LLAT re-entry after each dev deployment while keeping full-scope API/WS test coverage.

## Token Resolution Order

1. `HA_LLAT` (local env; preferred for local dev and CI)
2. app option `ha_llat` (persistent in `/data/options.json`)

## Relay Auth vs Upstream Auth

- Relay endpoint auth:
  - `RELAY_AUTH_TOKEN` required
- Upstream auth:
  - full scope requires LLAT (`HA_LLAT` or app option `ha_llat`)
  - no Supervisor-token fallback mode

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
APP_SLUG=self SUPERVISOR_URL=http://supervisor npm run seed:llat -- '<YOUR_LLAT>' --dry-run
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
2. read `/data/options.json` (or `APP_OPTIONS_PATH`)
3. resolve upstream token source
4. create app with ws proxy and auth
5. start HTTP server

## Supervisor-Assisted Restart Flow

Use `SUPERVISOR_TOKEN` and internal URL `http://supervisor`.

1. Validate current/new options payload:
```bash
curl -sS -X POST \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  -H "Content-Type: application/json" \
  http://supervisor/addons/self/options/validate
```

2. Persist options (`ha_llat`, `relay_auth_token`, ...):
```bash
curl -sS -X POST \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"options":{"ha_llat":"<TOKEN>"}}' \
  http://supervisor/addons/self/options
```

3. Restart app:
```bash
curl -sS -X POST \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  http://supervisor/addons/self/restart
```

4. Re-run smoke calls:
- `GET /health`
- `POST /ws` with `{"type":"ping"}`

Preferred terminology note (2026+):
- Use "App" in docs/UI communication.
- Keep technical endpoint path `/addons/...` because Supervisor API currently uses that path.

## Test Matrix

1. Unit tests (`resolveUpstreamToken`): source precedence, warnings, empty input handling
2. Unit tests (`loadEnv`): required `HA_LLAT`
3. Integration tests (`/ws`, `/health`): unchanged behavior
4. Optional live smoke test: set LLAT once with seed script, then deploy/restart and verify token remains active
