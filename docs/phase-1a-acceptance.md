# Phase 1a Acceptance Matrix

Date: 2026-02-25  
Branch: `feat/phase-1a-foundation`

## Demo-DoD

| Check | Command / Prompt | Expected | Actual | Status |
|---|---|---|---|---|
| Onboarding requires LLT | Review `skills/ha-onboarding.md` | LLT creation is mandatory before checks | File contains explicit LLT step + "Do not continue without LLT" | PASS |
| First quick-success flow is documented | Review `skills/ha-onboarding.md` | 3 quick wins are listed | Quick wins list present (lights, sensors, turn on light) | PASS |
| Two sample user intents have routing path | Review `skills/ha-nova.md` routing rules | Read query -> `ha-entities`; write automation -> `ha-automation-crud` + `ha-safety` | Routing rules include both cases | PASS |

## API-DoD

### Automated test evidence

| Check | Command | Expected | Actual | Status |
|---|---|---|---|---|
| Health contract | `npm test -- tests/http/health.test.ts` | 2 tests pass | 2/2 passed | PASS |
| WS proxy contract | `npm test -- tests/http/ws-proxy.test.ts` | 4 tests pass | 4/4 passed | PASS |
| Full regression set | `npm test` | all suites pass | 6 files, 16 tests passed | PASS |
| Type safety | `npm run typecheck` | no TS errors | passes | PASS |

### Curl smoke evidence (temporary local server)

| Check | Command | Expected | Actual | Status |
|---|---|---|---|---|
| Auth enforced on `/health` | `curl -i http://127.0.0.1:8799/health` | `401 UNAUTHORIZED` | `401` + `UNAUTHORIZED` | PASS |
| `/health` success payload | `curl -i -H 'Authorization: Bearer secret' .../health` | `200`, payload contains `status`, `ha_ws_connected`, `version`, `uptime_s` | `200` with all fields | PASS |
| `/ws` allowlisted type | `curl -i -X POST ... -d '{"type":"ping"}' .../ws` | `200` with data | `200`, `{"echoed":"ping"}` | PASS |
| `/ws` blocks unknown type | `curl -i -X POST ... -d '{"type":"evil/type"}' .../ws` | `403 WS_TYPE_NOT_ALLOWED` | `403` with expected code | PASS |
| `/ws` upstream error mapping | `curl -i -X POST ... -d '{"type":"ping_fail"}' .../ws` | `502 UPSTREAM_WS_ERROR` | `502` with expected code | PASS |

## Skill-DoD

| Check | Evidence | Status |
|---|---|---|
| Bootstrap scope is Phase 1a/1b only | `skills/ha-nova.md` has "Active Skill Catalog (Phase 1a/1b only)" | PASS |
| Onboarding is ultra-lean | `skills/ha-onboarding.md` has LLT + 2 connectivity checks + 3 quick wins + minimal troubleshooting | PASS |
| Safety is core-only | `skills/ha-safety.md` includes preview, no-guessing, clear errors | PASS |

## Conclusion

Phase 1a acceptance checks currently pass for implemented scope.
