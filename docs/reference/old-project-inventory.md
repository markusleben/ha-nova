# Old Project: ha-mcp-addon Inventory

Reference for the old MCP server at `/Users/markus/Daten/Development/Privat/ha-mcp-addon`.

## Project Statistics

- **88.160 lines of TypeScript** in 679 files
- 539 files in `src/server/fastmcp/` alone (64.582 lines)
- 18 manager tools with 200+ actions
- 70 best-practice rules
- Node.js >=20, TypeScript ES2022, ESM

## Directory Structure

```
/ha-mcp-addon/
├── src/
│   ├── index.ts                     # Entry Point
│   ├── config.ts                    # Config Loader
│   ├── config/
│   │   ├── constants.ts             # Limits, Timeouts
│   │   ├── schema.ts                # Zod Config Schema
│   │   ├── loader.ts                # Options.json → Env
│   │   └── feature-flags.ts
│   ├── ha/
│   │   ├── rest.ts                  # HA REST Client (~358 lines) ← REUSE
│   │   ├── ws.ts                    # HA WS Client (~366 lines) ← REUSE
│   │   ├── ws-state-machine.ts      # WS State Tracking ← REUSE
│   │   ├── ws-reconnection.ts       # WS Reconnect ← REUSE
│   │   ├── state-manager.ts         # Entity State Cache
│   │   ├── state-event-handler.ts   # HA Start/Stop Events
│   │   ├── entity-index.ts          # Entity index by domain/area/device
│   │   └── service-registry-cache.ts # Service cache with ETag
│   ├── server/
│   │   ├── fastmcp-server.ts        # MCP server setup (261 lines)
│   │   ├── resources.ts             # MCP resources (4 items)
│   │   └── fastmcp/                 # 539 files — THE HEART OF THE SERVER
│   │       ├── register-automation.ts
│   │       ├── register-script.ts
│   │       ├── register-helper.ts
│   │       ├── register-entity.ts
│   │       ├── register-control.ts
│   │       ├── register-event.ts
│   │       ├── register-dashboard.ts
│   │       ├── register-energy.ts
│   │       ├── register-integration.ts
│   │       ├── register-organizer.ts
│   │       ├── register-system.ts
│   │       ├── register-file.ts
│   │       ├── register-repairs.ts
│   │       ├── register-scene.ts
│   │       ├── register-sensor.ts
│   │       ├── register-template.ts
│   │       ├── register-backup.ts
│   │       ├── register-playbook.ts
│   │       ├── automation-*.ts      # ~30 files for automation alone
│   │       ├── script-*.ts          # ~15 files
│   │       ├── helper-*.ts          # ~10 files
│   │       ├── template-*.ts        # ~10 files
│   │       ├── sensor-*.ts          # ~8 files
│   │       ├── entity-*.ts          # ~10 files
│   │       ├── dashboard-*.ts       # ~5 files
│   │       ├── energy-*.ts          # ~5 files
│   │       └── ...
│   ├── backup/
│   │   └── manager.ts              # Secure backup manager (~697 lines) ← REUSE
│   ├── security/
│   │   ├── auth.ts                  # Token validation ← REUSE
│   │   ├── rate-limiter.ts          # Rate Limiting
│   │   ├── resource-guard.ts        # Memory/Event-Loop Pressure
│   │   ├── consent-proof.ts         # HMAC-signed proofs
│   │   ├── mutation-guard.ts        # Write gate
│   │   └── sensor-config-write-guard.ts
│   ├── best-practices/
│   │   ├── engine.ts                # Rule Engine
│   │   └── rules/                   # 70 rule files → BECOME SKILL KNOWLEDGE
│   └── utils/
│       ├── logger.ts                # Structured logging ← REUSE
│       ├── async.ts                 # Async Helpers
│       ├── circuit-breaker.ts       # Circuit Breaker
│       └── input-sanitizer.ts       # Input Sanitization
├── addon/
│   ├── config.yaml                  # Supervisor Config
│   ├── Dockerfile                   # Multi-Arch Build
│   └── run                          # Startup Script
├── docs/                            # 33+ documentation files
├── tests/                           # Vitest Tests
└── scripts/                         # Build, QC, Deploy Scripts
```

## Files that can be reused for the Relay

| File | Lines | Function |
|-------|--------|----------|
| `src/ha/rest.ts` | ~358 | HA REST Client (Axios, Auth, Error-Mapping) |
| `src/ha/ws.ts` | ~366 | HA WebSocket Client (home-assistant-js-websocket) |
| `src/ha/ws-state-machine.ts` | ~100 | WS State: CONNECTING→CONNECTED→DISCONNECTED→CIRCUIT_OPEN |
| `src/ha/ws-reconnection.ts` | ~80 | Exponential Backoff Reconnect |
| `src/backup/manager.ts` | ~697 | Backup CRUD with compression + retention |
| `src/security/auth.ts` | ~50 | Bearer Token Validation |
| `src/utils/logger.ts` | ~80 | Structured JSON Logger |
| `src/config.ts` | ~200 | Config loading (heavily simplify) |
| **Total reusable** | **~1.931** | |

## Dependencies of the old project

```json
{
  "fastmcp": "3.32.0",            // ← REMOVED
  "@modelcontextprotocol/sdk": "1.26.0",  // ← REMOVED
  "zod": "3.23.8",                 // Optionally keep
  "yaml": "2.8.2",                 // Keep
  "axios": "1.13.5",               // Keep (REST Client)
  "home-assistant-js-websocket": "9.6.0",  // Keep (WS Client)
  "ws": "8.19.0",                  // Keep (WS Polyfill)
  "dotenv": "16.4.5"               // Optional
}
```

## Best-Practice Rules (most important for Skills)

The top 10 that will be migrated as skill knowledge in phase 1c:

1. **binary-sensor-trigger-without-for** — Binary sensor trigger without `for:` (bouncing)
2. **wait-without-timeout** — `wait_for_trigger` without timeout
3. **service-action-without-target** — Service call without target
4. **template-numeric-without-availability-guard** — Template without availability
5. **actuator-oscillation-risk** — Parallel actions on the same actuator
6. **state-trigger-without-attribute** — State trigger without attribute filter
7. **missing-off-delay** — Missing off-delay for motion sensors
8. **single-mode-with-repeating-trigger** — `single` mode with repeat-capable triggers
9. **delay-instead-of-timer** — Delay instead of timer helper
10. **condition-for-restart-loss** — Missing condition in `restart` mode

Full list: `ha-mcp-addon/src/best-practices/rules/` (70 files)
