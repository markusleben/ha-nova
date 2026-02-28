# HA Nova Bridge: Architecture Reference

## Overview

The Bridge is a lean App that runs on the HA host and provides three capabilities
that a remote Skill cannot: WebSocket proxy, filesystem access, backups.

## Endpoints

### Phase 1a (MVP)

```
GET  /health
POST /ws
```

### Phase 1c (+ Subscriptions)

```
POST /ws/subscribe
```

### Phase 2 (+ Backups)

```
POST /backups
```

### Phase 3 (+ Filesystem)

```
POST /files
```

## Endpoint Specifications

### `GET /health`
```json
Response 200:
{
  "status": "ok",
  "ha_ws_connected": true,
  "version": "1.0.0",
  "uptime_s": 3600
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
  "error": "message"
}
```

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

### `POST /backups` — Backup Management
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

## Auth

Same long-lived access token as for HA:
```
Authorization: Bearer {HA_TOKEN}
```

The Bridge validates the token by comparing it with the configured token (single-tenant).

## WS Forwarding Policy

The Bridge forwards authenticated WS messages as passthrough requests.
Message-type filtering is not applied locally in the Relay.

## Configuration

```yaml
# Required
HA_URL: "http://homeassistant:8123"
HA_TOKEN: "<long-lived-access-token>"

# Optional
BRIDGE_PORT: 8791              # Default: 8791
LOG_LEVEL: "info"              # trace|debug|info|warn|error
CONFIG_ROOT: "/homeassistant"  # HA config directory
BACKUP_DIR: "/data/backups"    # Backup storage
BACKUP_COMPRESS: true
BACKUP_RETENTION_DAYS: 30
WRITABLE_ROOTS: "/homeassistant/ha_mcp"
```

## Tech Stack

- TypeScript / Node.js >=20
- No HTTP framework (Node.js `http.createServer`)
- Dependencies: `ws`, `yaml`, optional `zod`
- Estimated scope: ~700 core lines (Phase 1a), ~2,000-3,000 lines final

## What the Bridge does NOT do

- No business logic
- No validation rules
- No state caching
- No consent gating
- No session management
- No metrics (logging only)
- No MCP protocol (optional as a facade in Phase 4)
