# NOVA Relay: Architecture Reference

> **Implementation status:** Phase 1a is implemented (`/health`, `/ws`, `/core`), and
> the Phase 3 filesystem endpoint (`/files`) shipped with relay 0.4.0 — opt-in and
> off by default. Phases 1c (streaming subscriptions) and 2 (backups) remain planned.
> Phase 1b was folded into 1a during development (the `/core` REST proxy was
> originally scoped as a separate phase but shipped together with `/ws` and `/health`).

## Overview

The Relay is a lean App that runs on the HA host and provides three capabilities
that a remote Skill cannot: WebSocket proxy (implemented), REST core proxy (implemented),
filesystem access (planned). Backup lifecycle (status/create/inspect/delete) needs no
dedicated endpoint — `ha-nova:backup` rides the existing `/ws` proxy (`backup/*`).

## Endpoints

### Phase 1a (MVP) — IMPLEMENTED

```
GET  /health
POST /ws
POST /core
```

### Phase 1c (+ Subscriptions) — PLANNED, NOT IMPLEMENTED

```
POST /ws/subscribe
```

### Phase 2 (config-snapshot store) — PLANNED, NOT IMPLEMENTED

HA SYSTEM backups (status/create/inspect/delete) are covered today via `/ws`
(`ha-nova:backup`) and need no relay endpoint. This planned endpoint is a
different thing: a relay-side store for CONFIG snapshots (see the
`POST /backups` — Backup Management spec below):

```
POST /backups
```

### Phase 3 (+ Filesystem) — IMPLEMENTED in relay 0.4.0 (opt-in, default off)

```
POST /files
```

---

## Endpoint Specifications — Implemented

### `GET /health`

Returns relay status. Wrapped in the standard envelope (`{ ok: true, data: ... }`).
Requires the relay bearer token like the other implemented endpoints.

```json
Response 200:
{
  "ok": true,
  "data": {
    "status": "ok",
    "ha_ws_connected": true,
    "version": "0.2.5",
    "uptime_s": 3600
  }
}
```

### `POST /ws` — Generic WS Proxy
```json
Request:
{
  "type": "config/area_registry/list"
}

Response 200:
{
  "ok": true,
  "data": [...]
}

Response 4xx/5xx:
{
  "ok": false,
  "error": { "code": "UPSTREAM_WS_ERROR", "message": "..." }
}
```

Validation: request body must contain a non-empty string field `type`.
On validation failure: `400 VALIDATION_ERROR`.
On upstream failure: `502 UPSTREAM_WS_ERROR` or `502 UPSTREAM_WS_TIMEOUT`.
For finite event-response WS commands, a Skill may opt in to bounded event
collection with an explicit envelope:

```json
{
  "message": { "type": "system_health/info" },
  "collect_events": {
    "until_type": "finish",
    "max_events": 100,
    "timeout_ms": 10000,
    "on_limit": "error"
  }
}
```

The Relay forwards `message`, collects events until `until_type`, and returns
them as `data.events`. It does not inspect command semantics or event payloads.

`on_limit` (relay 0.3.0) decides what happens when `max_events` or `timeout_ms`
is reached before the finish event:

- `"error"` (default): fail with `502 UPSTREAM_WS_ERROR` / `UPSTREAM_WS_TIMEOUT` — the original strict semantics.
- `"return"` (window mode): resolve with the events collected so far and set `data.truncated: true`. This is what makes bounded *sniffing* possible for streams that never emit a finish event (MQTT topics, event buses).

**Subscription commands are allowed INSIDE a `collect_events` envelope** (relay
0.3.0). The two reasons for the general ban do not apply there: the collection
unsubscribes in its `finally` block, and its lifetime is bounded by
`max_events`/`timeout_ms`, so no upstream subscription can leak or accumulate.
A *bare* subscription (no envelope) is still rejected with
`400 UNSUPPORTED_WS_TYPE`, because the relay could neither deliver its events
nor bound its lifetime.

Optional: Batch mode
```json
Request (Array):
[
  { "type": "config/area_registry/list" },
  { "type": "config/floor_registry/list" }
]

Response 200:
{
  "ok": true,
  "data": [
    { "ok": true, "data": [...] },
    { "ok": true, "data": [...] }
  ]
}
```

### `POST /core` — REST Core Proxy

Proxies arbitrary HA REST API calls through the Relay. Used by write, helper,
and review skills to call HA REST endpoints without needing the upstream token.

```json
Request:
{
  "method": "GET",        // "GET" | "POST" | "DELETE"
  "path": "/api/states",  // must start with /api/
  "body": { ... }         // optional, used with POST
}

Response 200:
{
  "ok": true,
  "data": {
    "status": 200,
    "body": [ ... ]
  }
}

Response 400 (validation):
{
  "ok": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request body must contain: method ('GET'|'POST'|'DELETE') and path ('/api/...')"
  }
}

Response 502 (upstream failure):
{
  "ok": false,
  "error": { "code": "UPSTREAM_HTTP_ERROR", "message": "..." }
}
```

Security:
- `path` must start with `/api/`; absolute URLs are rejected.
- Path traversal (`..`, `\`, encoded variants) is blocked via iterative decode + check.
- Path length capped at 2048 characters.
- Control characters in path are rejected.
- Upstream request timeout: 10 s (default).

---

## Endpoint Specifications — Planned (NOT IMPLEMENTED)

> The following endpoints are designed but have no implementation yet.
> Specifications may change before implementation.

### `POST /ws/subscribe` — Event Subscription
```json
Request:
{
  "type": "subscribe_events",
  "event_type": "state_changed",
  "duration_sec": 30
}

Response: SSE-Stream
data: {"event_type":"state_changed","data":{...}}
data: {"event_type":"state_changed","data":{...}}
```

Limits: max 300s duration, max 5 concurrent subscriptions.

### `POST /files` — Filesystem Operations
```json
// list_dir
{ "action": "list_dir", "path": "/config/ha_mcp", "limit": 200 }

// read_file
{ "action": "read_file", "path": "/config/ha_mcp/templates/my_sensor.yaml", "max_bytes": 200000 }

// write_file
{ "action": "write_file", "path": "/config/ha_mcp/templates/my_sensor.yaml", "content": "..." }

// delete_file
{ "action": "delete_file", "path": "/config/ha_mcp/sensors/rest/old.yaml" }
```

Security:
- Paths must be within `/config` (HA config root)
- Blocked: `.storage`, `.cloud`, `.ssh`, `.git`, `deps`, `ssl`, `secrets.yaml`
- Symlink traversal check
- Writes only in whitelisted directories (`/config/ha_mcp/`)

### `POST /backups` — Backup Management (Phase 2, planned config-snapshot store)

Relay-side storage for CONFIG snapshots (automation JSON, etc.) — distinct from
HA system backups, which `ha-nova:backup` manages via `/ws` `backup/*` today.

```json
// list
{ "action": "list", "category": "automations" }

// save
{ "action": "save", "category": "automations", "name": "before-cleanup", "data": "..." }

// load
{ "action": "load", "file": "automations/before-cleanup-2026-02-25.json.gz" }

// prune
{ "action": "prune", "max_age_days": 30, "max_files": 100 }

// delete
{ "action": "delete", "file": "automations/old-backup.json.gz" }
```

---

## Auth — Dual-Token Model

The Relay uses two separate tokens for inbound and upstream authentication:

| Token | Env Var | Purpose |
|-------|---------|---------|
| Relay auth token | `RELAY_AUTH_TOKEN` | Authenticates inbound client requests to the Relay |
| HA Long-Lived Access Token | `HA_LLAT` | Authenticates Relay requests upstream to Home Assistant (WS + REST) |

**Inbound (client -> Relay):**
```
Authorization: Bearer {RELAY_AUTH_TOKEN}
```
Validated via timing-safe comparison (`node:crypto.timingSafeEqual`). On failure: `401 UNAUTHORIZED`.

**Upstream (Relay -> HA):**
The Relay uses `HA_LLAT` to authenticate with Home Assistant. For WebSocket it creates a
long-lived token auth via `home-assistant-js-websocket`. For REST calls (`/core` proxy)
it adds `Authorization: Bearer {HA_LLAT}` to upstream `fetch()` requests.

The two tokens are independent. `RELAY_AUTH_TOKEN` is chosen by the operator;
`HA_LLAT` is generated inside Home Assistant.

## WS Forwarding Policy

The Relay forwards authenticated WS messages as passthrough requests.
Message-type filtering is not applied locally.

## Configuration

All configuration is via environment variables. The `run` entrypoint script
resolves values from HA app options and sets them before starting Node.

```yaml
# Required (both must be non-empty)
RELAY_AUTH_TOKEN: "<operator-chosen-secret>"   # Inbound client auth
HA_LLAT: "<ha-long-lived-access-token>"        # Upstream HA auth

# Optional (with defaults)
HA_URL: "http://homeassistant:8123"            # Default: http://homeassistant:8123
RELAY_PORT: 8791                               # Default: 8791
LOG_LEVEL: "info"                              # trace|debug|info|warn|error, default: info
RELAY_VERSION: "dev"                           # Injected by run script from bashio
APP_OPTIONS_PATH: "/data/options.json"         # HA app options file path
```

## Tech Stack

- TypeScript / Node.js >=20
- No HTTP framework (Node.js `http.createServer`)
- REST client uses native `fetch()` (no axios at runtime)
- WS client uses `home-assistant-js-websocket` (the only directly imported production dependency)
- `ws`, `yaml` are listed in `package.json` but not directly imported by relay source code; they are transitive or reserved for future phases (`axios` was removed as dead weight — REST stays on native `fetch()`)
- Current scope: ~1,500 lines across ~20 `.ts` files (Phase 1a)

## Standard Envelope

All responses follow the same JSON envelope (defined in `types/api.ts`):

```typescript
// Success
{ ok: true, data: T }

// Error
{ ok: false, error: { code: string, message: string } }
```

HTTP status is 200 for success. Error status codes: 400 (validation), 401 (auth),
404 (route not found), 502 (upstream failure), 500 (internal).

## What the Relay does NOT do

- No business logic
- No validation rules (beyond request format and path safety)
- No state caching
- No consent gating
- No session management
- No metrics (structured JSON logging only)
- No MCP protocol
