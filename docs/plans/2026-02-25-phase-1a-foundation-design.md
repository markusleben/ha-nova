# Phase 1a Foundation (Bridge MVP + Bootstrap Skills) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a balanced Phase 1a that delivers working Bridge MVP (`GET /health`, `POST /ws`) plus the three core skills (`ha-nova`, `ha-onboarding`, `ha-safety`) with clear acceptance checks.

**Architecture:** Keep Bridge deliberately thin: auth, routing, WS proxy, normalized error handling. Keep all decision logic in skills. Build vertical slices with tests first so each capability is usable end-to-end before expanding scope.

**Tech Stack:** TypeScript (Node.js >=20), `home-assistant-js-websocket`, `ws`, `yaml`, Vitest.

---

## Auth Decision (Locked)

- Onboarding requires a **user-generated Long-Lived Token (LLT)**.
- `SUPERVISOR_TOKEN` is treated as internal runtime detail, not onboarding credential.
- Phase 1a does not depend on supervisor-only auth paths.
- Rationale: LLT gives stable, user-controlled access for direct REST + bridge workflows.

## Approach Options (Brainstorm Result)

### Option A: Infra-first (all bridge internals, then skills)
- Pros: solid backend baseline early
- Cons: delayed user-visible value, higher integration risk late

### Option B (Recommended): Vertical slices
- Slice 1: server skeleton + auth + normalized errors
- Slice 2: `/health` + tests
- Slice 3: `/ws` allowlisted proxy + tests
- Slice 4: skill trio + onboarding flow
- Pros: fastest validation, balanced DoD, easier rollback
- Cons: requires stricter sequencing discipline

### Option C: Skill-first (docs complete before bridge)
- Pros: quick documentation output
- Cons: high risk of drift from real bridge behavior

Decision: **Option B (Vertical slices)**.

## Scope and Guardrails (Phase 1a)

- In scope:
  - Bridge endpoints: `GET /health`, `POST /ws`
  - Bearer token auth
  - LLT-first upstream auth model (`HA_TOKEN` mandatory)
  - Dev-first WS allowlist (configurable, deny-by-default for unknown types)
  - Consistent JSON error envelope (`ok: false`, `error.code`, `error.message`)
  - Skills: `ha-nova.md`, `ha-onboarding.md`, `ha-safety.md`
  - Light automation tests (Vitest) for auth, health, ws proxy behavior
- Out of scope:
  - `POST /ws/subscribe`, `POST /files`, `POST /backups`
  - Full best-practice rule catalog
  - Deep safety/consent framework beyond core-only rules

## Definition of Done (Balanced, Reduced Scope)

- Demo-DoD:
  - Fresh user can complete onboarding flow and run 2 successful sample prompts.
- API-DoD:
  - `/health` and `/ws` pass contract tests and failure-path tests.
  - Missing/invalid upstream LLT produces explicit startup or request-time error with remediation text.
- Skill-DoD:
  - Bootstrap routes correctly for Phase 1a/1b domains only.
  - Onboarding remains ultra-lean.
  - Safety remains core-only.

---

### Task 1: Project Skeleton and Tooling Baseline

**Files:**
- Create: `package.json`
- Create: `tsconfig.json`
- Create: `vitest.config.ts`
- Create: `.gitignore`
- Create: `src/index.ts`
- Create: `src/types/api.ts`

**Step 1: Write failing test entrypoint smoke**
- Create: `tests/bootstrap/startup.test.ts`
- Assert process can import server bootstrap factory.

**Step 2: Run test to verify it fails**
- Run: `npm test -- tests/bootstrap/startup.test.ts`
- Expected: FAIL with missing module errors.

**Step 3: Add minimal runtime/tooling**
- Add npm scripts: `build`, `dev`, `test`, `test:watch`, `typecheck`.
- Add TS config for NodeNext + strict mode.
- Add minimal `src/index.ts` export for app factory.

**Step 4: Run tests and typecheck**
- Run: `npm test -- tests/bootstrap/startup.test.ts`
- Run: `npm run typecheck`
- Expected: PASS for startup test, no TS errors.

**Step 5: Commit**
- `git add package.json tsconfig.json vitest.config.ts .gitignore src/index.ts src/types/api.ts tests/bootstrap/startup.test.ts`
- `git commit -m "build: initialize bridge skeleton and test tooling"`

### Task 2: Config and Auth Core

**Files:**
- Create: `src/config/env.ts`
- Create: `src/security/auth.ts`
- Test: `tests/security/auth.test.ts`

**Step 1: Write failing auth tests**
- Missing header -> 401
- Invalid bearer token -> 401
- Valid bearer token -> request continues

**Step 2: Run test to verify fail**
- Run: `npm test -- tests/security/auth.test.ts`
- Expected: FAIL (auth module missing).

**Step 3: Implement minimal auth/config**
- `env.ts`: parse `HA_TOKEN` (LLT, required), `BRIDGE_PORT`, `LOG_LEVEL`.
- `auth.ts`: parse `Authorization: Bearer <token>` and constant-time compare.

**Step 4: Re-run tests**
- Run: `npm test -- tests/security/auth.test.ts`
- Expected: PASS.

**Step 5: Commit**
- `git add src/config/env.ts src/security/auth.ts tests/security/auth.test.ts`
- `git commit -m "feat: add env parsing and bearer auth middleware"`

### Task 3: HTTP Server and Error Envelope

**Files:**
- Create: `src/http/server.ts`
- Create: `src/http/router.ts`
- Create: `src/http/errors.ts`
- Test: `tests/http/error-envelope.test.ts`

**Step 1: Write failing error contract tests**
- Unknown route -> `404` + `{ok:false,error:{code:"NOT_FOUND",message}}`
- Malformed JSON -> `400` + `INVALID_JSON`
- Unhandled internal -> `500` + `INTERNAL_ERROR`

**Step 2: Run tests to verify fail**
- Run: `npm test -- tests/http/error-envelope.test.ts`
- Expected: FAIL.

**Step 3: Implement minimal server/router**
- Create Node `http.createServer`.
- Route only known endpoints.
- Centralize response helper for success/error envelopes.

**Step 4: Re-run tests**
- Run: `npm test -- tests/http/error-envelope.test.ts`
- Expected: PASS.

**Step 5: Commit**
- `git add src/http/server.ts src/http/router.ts src/http/errors.ts tests/http/error-envelope.test.ts`
- `git commit -m "feat: add http server router and normalized error envelopes"`

### Task 4: HA WS Client Wrapper

**Files:**
- Create: `src/ha/ws-client.ts`
- Test: `tests/ha/ws-client.test.ts`

**Step 1: Write failing ws client tests**
- Connect success path
- Connection failure maps to typed bridge error
- Request timeout maps to typed bridge error

**Step 2: Run tests to verify fail**
- Run: `npm test -- tests/ha/ws-client.test.ts`
- Expected: FAIL.

**Step 3: Implement minimal ws wrapper**
- Single shared HA WS connection.
- `sendMessage(type,payload)` helper.
- Timeout guard for request/response.

**Step 4: Re-run tests**
- Run: `npm test -- tests/ha/ws-client.test.ts`
- Expected: PASS.

**Step 5: Commit**
- `git add src/ha/ws-client.ts tests/ha/ws-client.test.ts`
- `git commit -m "feat: add home assistant websocket client wrapper"`

### Task 5: `GET /health` Endpoint

**Files:**
- Create: `src/http/handlers/health.ts`
- Modify: `src/http/router.ts`
- Test: `tests/http/health.test.ts`

**Step 1: Write failing `/health` tests**
- Returns `200` with fields: `status`, `ha_ws_connected`, `version`, `uptime_s`
- No token -> 401

**Step 2: Run tests to verify fail**
- Run: `npm test -- tests/http/health.test.ts`
- Expected: FAIL.

**Step 3: Implement handler**
- Wire auth before handler.
- Use ws client state to populate `ha_ws_connected`.

**Step 4: Re-run tests**
- Run: `npm test -- tests/http/health.test.ts`
- Expected: PASS.

**Step 5: Commit**
- `git add src/http/handlers/health.ts src/http/router.ts tests/http/health.test.ts`
- `git commit -m "feat: implement authenticated health endpoint"`

### Task 6: WS Allowlist Engine (Dev-First)

**Files:**
- Create: `src/security/ws-allowlist.ts`
- Modify: `src/config/env.ts`
- Test: `tests/security/ws-allowlist.test.ts`

**Step 1: Write failing allowlist tests**
- Allowed type passes
- Unknown type blocked with `WS_TYPE_NOT_ALLOWED`
- Wildcard entries (e.g. `config/area_registry/*`) supported

**Step 2: Run tests to verify fail**
- Run: `npm test -- tests/security/ws-allowlist.test.ts`
- Expected: FAIL.

**Step 3: Implement allowlist module**
- Built-in defaults for Phase 1a.
- Env-based extension list for quick iteration.

**Step 4: Re-run tests**
- Run: `npm test -- tests/security/ws-allowlist.test.ts`
- Expected: PASS.

**Step 5: Commit**
- `git add src/security/ws-allowlist.ts src/config/env.ts tests/security/ws-allowlist.test.ts`
- `git commit -m "feat: add configurable websocket type allowlist"`

### Task 7: `POST /ws` Endpoint (Single Message Mode)

**Files:**
- Create: `src/http/handlers/ws-proxy.ts`
- Modify: `src/http/router.ts`
- Test: `tests/http/ws-proxy.test.ts`

**Step 1: Write failing `/ws` tests**
- Valid allowlisted type -> `200` + `{ok:true,data}`
- Unknown type -> `403` + `WS_TYPE_NOT_ALLOWED`
- Missing `type` -> `400` + `VALIDATION_ERROR`
- HA WS failure -> mapped `502` + `UPSTREAM_WS_ERROR`

**Step 2: Run tests to verify fail**
- Run: `npm test -- tests/http/ws-proxy.test.ts`
- Expected: FAIL.

**Step 3: Implement handler**
- Parse JSON body.
- Validate `type` presence and shape.
- Enforce allowlist.
- Forward to HA WS client and return normalized result.

**Step 4: Re-run tests**
- Run: `npm test -- tests/http/ws-proxy.test.ts`
- Expected: PASS.

**Step 5: Commit**
- `git add src/http/handlers/ws-proxy.ts src/http/router.ts tests/http/ws-proxy.test.ts`
- `git commit -m "feat: implement websocket proxy endpoint with allowlist and error mapping"`

### Task 8: Skills (Phase 1a only, client-agnostic)

**Files:**
- Create: `skills/ha-nova.md`
- Create: `skills/ha-onboarding.md`
- Create: `skills/ha-safety.md`

**Step 1: Write `ha-nova.md` (bootstrap)**
- Include only active 1a/1b skills.
- Include API routing table (REST vs Bridge).
- Include explicit “load `ha-safety` for write ops” rule.

**Step 2: Write `ha-onboarding.md` (ultra-lean)**
- Mandatory first step: user creates LLT in HA profile/security and sets `HA_TOKEN`.
- Connection check sequence:
  - `GET {HA_URL}/api/`
  - `GET {BRIDGE_URL}/health`
- Three quick wins only.
- Minimal failure hints for URL/LLT/bridge down.

**Step 3: Write `ha-safety.md` (core-only)**
- Preview-before-execute.
- No guessing entity IDs/services.
- Clear user confirmation before any write.

**Step 4: Manual prompt validation**
- Run 5 scripted prompts and verify expected route/behavior in responses.
- Record pass/fail in plan appendix.

**Step 5: Commit**
- `git add skills/ha-nova.md skills/ha-onboarding.md skills/ha-safety.md`
- `git commit -m "docs: add phase 1a bootstrap onboarding and safety skills"`

### Task 9: Phase 1a Acceptance Matrix Execution

**Files:**
- Create: `docs/phase-1a-acceptance.md`

**Step 1: Define acceptance checklist**
- Demo-DoD checks
- API-DoD checks
- Skill-DoD checks

**Step 2: Execute API checks**
- `curl` tests for auth, health, ws allowlist block/pass.

**Step 3: Execute skill checks**
- Run scripted onboarding + 2 sample tasks.
- Verify routing and safety behavior.

**Step 4: Record evidence**
- For each check: command/prompt, expected, actual, status.

**Step 5: Commit**
- `git add docs/phase-1a-acceptance.md`
- `git commit -m "test: add and execute phase 1a acceptance matrix"`

---

## Phase 1a Acceptance Criteria (Condensed)

1. Bridge returns normalized errors on all tested failure paths (`400/401/403/404/500/502`).
2. `GET /health` returns valid envelope and auth-gated behavior.
3. `POST /ws` supports allowlisted message forwarding and blocks unknown types.
4. `ha-nova.md` routes only 1a/1b scope skills.
5. `ha-onboarding.md` supports first success path in <5 minutes.
6. Onboarding clearly requires LLT and includes one explicit LLT validation step.
7. `ha-safety.md` enforces preview + no-guessing.
8. Vitest suite covers auth, health, ws-proxy, allowlist, and envelope contracts.

## Risks and Mitigations

- Risk: WS proxy behavior differs from HA expectations.
  - Mitigation: maintain typed upstream error mapping + explicit timeout.
- Risk: Skill docs drift from actual endpoint behavior.
  - Mitigation: acceptance checklist includes prompt-to-endpoint evidence.
- Risk: Over-scope in 1a.
  - Mitigation: strict out-of-scope list and defer to 1b/1c.

## Execution Order

1. Task 1
2. Task 2
3. Task 3
4. Task 4
5. Task 5
6. Task 6
7. Task 7
8. Task 8
9. Task 9
