# Phase 1a.3 Packaging + HA Runtime Smoke Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the Relay installable/runnable as a Home Assistant App and verify end-to-end startup + smoke tests with stable LLAT onboarding.

**Architecture:** Keep runtime logic in Node (`src/runtime/*`), keep container glue thin (`addon/run` only env wiring + process start). Avoid duplicated token logic in shell; token precedence remains centralized in `resolveUpstreamToken()`. Testing split: contract tests for App metadata + runtime tests + local HA smoke path.

**Tech Stack:** TypeScript (Node 20), Vitest, YAML parser, Home Assistant App packaging (`config.yaml`, `Dockerfile`, `run`).

---

## KISS + DRY Guardrails

- One source of truth for upstream token precedence: `src/security/token-resolver.ts`.
- One source of truth for runtime bootstrap: `src/runtime/start.ts`.
- `addon/run` must not implement business logic; only env mapping + process exec.
- No host network, no privileged flags unless strictly required.
- Keep App options minimal: `bridge_auth_token`, `ha_llat`.

## Official Best-Practice Inputs (checked 2026-02-26)

- App config and `/data/options.json`: https://developers.home-assistant.io/docs/apps/configuration/
- App communication + supervisor/core proxies: https://developers.home-assistant.io/docs/apps/communication/
- Local app testing (devcontainer/local build/run): https://developers.home-assistant.io/docs/apps/testing/
- App security hardening: https://developers.home-assistant.io/docs/apps/security/
- Supervisor options endpoints: https://developers.home-assistant.io/docs/api/supervisor/endpoints/
- User-side LLAT management: https://www.home-assistant.io/docs/authentication/

## Definition of Done

- App package can be built and started locally.
- HA can discover/install app from local repository.
- `/health` and `/ws` smoke pass on running app.
- LLAT persists via options (`/data/options.json`) and survives restart/update.
- Full verification passes: tests + typecheck + build.

### Task 1: Add App Contract Tests (metadata first)

**Files:**
- Create: `tests/addon/config-contract.test.ts`
- Create: `tests/addon/run-contract.test.ts`

**Step 1: Write failing `config.yaml` contract test**
- Assert required keys in `addon/config.yaml`.
- Assert security-related keys: `homeassistant_api: true`, `hassio_api: true`, `hassio_role: default`, `ingress: true`.
- Assert options/schema keys include `bridge_auth_token`, `ha_llat`.

**Step 2: Run test to verify it fails**
- Run: `npm test -- tests/addon/config-contract.test.ts`
- Expected: FAIL (missing keys/schema mismatch).

**Step 3: Write failing `run` contract test**
- Assert `addon/run` exists and is executable script.
- Assert `addon/run` starts `node dist/runtime/main.js`.
- Assert no token precedence logic duplicated in shell.

**Step 4: Run test to verify it fails**
- Run: `npm test -- tests/addon/run-contract.test.ts`
- Expected: FAIL (`addon/run` missing or contract mismatch).

**Step 5: Commit test scaffolding**
- `git add tests/addon/config-contract.test.ts tests/addon/run-contract.test.ts`
- `git commit -m "test: add addon packaging contract tests"`

### Task 2: Implement App Packaging Files (minimal runtime glue)

**Files:**
- Create: `addon/Dockerfile`
- Create: `addon/run`
- Modify: `addon/config.yaml`

**Step 1: Implement minimal Dockerfile**
- `ARG BUILD_FROM` / `FROM $BUILD_FROM`
- install Node runtime dependency only if needed
- copy `dist/`, `package.json`, minimal runtime assets
- set `CMD ["/run"]`

**Step 2: Implement thin `addon/run`**
- read options from `/data/options.json` via bashio (or pass through defaults)
- export env vars only (`BRIDGE_AUTH_TOKEN`, `HA_LLAT`, `WS_ALLOWLIST_APPEND`, `ADDON_OPTIONS_PATH`)
- `exec node dist/runtime/main.js`

**Step 3: Update `addon/config.yaml` options/schema**
- add `bridge_auth_token` (`password`, mandatory)
- keep `ha_llat` (`password?` optional)
- no ws type filter option
- set `hassio_role: default` explicitly

**Step 4: Run contract tests**
- Run: `npm test -- tests/addon/config-contract.test.ts tests/addon/run-contract.test.ts`
- Expected: PASS.

**Step 5: Commit packaging implementation**
- `git add addon/Dockerfile addon/run addon/config.yaml`
- `git commit -m "feat: add runnable addon packaging artifacts"`

### Task 3: Runtime Startup Hardening (no duplicated logic)

**Files:**
- Modify: `src/runtime/start.ts`
- Modify: `src/runtime/main.ts`
- Modify: `tests/bootstrap/runtime-start.test.ts`

**Step 1: Write failing tests for packaged startup assumptions**
- startup logs include auth source/capability
- restricted mode does not crash server startup
- explicit error message on `/ws` in limited mode

**Step 2: Run tests to verify fail**
- Run: `npm test -- tests/bootstrap/runtime-start.test.ts`
- Expected: FAIL for new assertions.

**Step 3: Implement minimal runtime adjustments**
- no behavior drift; only deterministic startup logging/error messaging
- keep token resolution only in `resolveUpstreamToken()` path

**Step 4: Re-run runtime tests**
- Run: `npm test -- tests/bootstrap/runtime-start.test.ts`
- Expected: PASS.

**Step 5: Commit runtime hardening**
- `git add src/runtime/start.ts src/runtime/main.ts tests/bootstrap/runtime-start.test.ts`
- `git commit -m "refactor: harden packaged runtime startup contracts"`

### Task 4: Local Build/Run App Smoke Scripts (fast feedback)

**Files:**
- Create: `scripts/smoke/addon-local-build.sh`
- Create: `scripts/smoke/addon-local-run.sh`
- Create: `scripts/smoke/addon-http-smoke.sh`
- Modify: `package.json`

**Step 1: Write failing script tests or command checks**
- verify scripts exist + executable
- verify expected commands are present (builder/docker build/run/curl)

**Step 2: Run check and verify fail**
- Run: `npm test -- tests/addon/run-contract.test.ts`
- Expected: FAIL until scripts are created.

**Step 3: Implement scripts**
- `addon-local-build.sh`: build with `BUILD_FROM` override for local arch.
- `addon-local-run.sh`: mount `/tmp/ha_nova_data:/data`, expose Relay port.
- `addon-http-smoke.sh`: call `/health` and `/ws` with bearer token.

**Step 4: Add npm aliases**
- `smoke:addon:build`, `smoke:addon:run`, `smoke:addon:http`.

**Step 5: Commit smoke tooling**
- `git add scripts/smoke package.json`
- `git commit -m "chore: add local addon smoke scripts"`

### Task 5: HA Runtime Smoke in Real Supervisor (manual acceptance)

**Files:**
- Create: `docs/phase-1a3-acceptance.md`
- Modify: `docs/llat-dev-testing.md`

**Step 1: Define manual acceptance checklist**
- install from local App repository
- set options once (`bridge_auth_token`, optionally `ha_llat`)
- start Relay App, inspect logs
- call `/health`, `/ws`
- restart Relay App and verify LLAT persistence

**Step 2: Add Supervisor API assisted checks**
- include `/addons/self/options/validate`
- include `/addons/self/options` and `/addons/self/restart` calls

**Step 3: Record expected outputs**
- exact response envelopes for healthy and limited modes

**Step 4: Commit docs**
- `git add docs/phase-1a3-acceptance.md docs/llat-dev-testing.md`
- `git commit -m "docs: add phase 1a3 addon runtime acceptance checklist"`

### Task 6: Final Verification + Integration

**Files:**
- Modify: `docs/breadcrumbs.md`
- Modify: `docs/choices.md`

**Step 1: Full verification**
- Run: `npm test`
- Run: `npm run typecheck`
- Run: `npm run build`
- Expected: all PASS.

**Step 2: Optional local container smoke**
- Run: `npm run smoke:addon:build`
- Run: `npm run smoke:addon:run`
- Run: `npm run smoke:addon:http`
- Expected: health/ws smoke PASS.

**Step 3: Update breadcrumbs/choices**
- note KISS/DRY decisions and security choices.

**Step 4: Commit final verification notes**
- `git add docs/breadcrumbs.md docs/choices.md`
- `git commit -m "docs: record phase 1a3 packaging decisions and verification"`
