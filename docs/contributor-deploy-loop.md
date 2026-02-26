# Contributor Deploy Loop (HA App)

Date: 2026-02-26

## Goal

Fast, repeatable deploys to Home Assistant during development without relying on UI cache behavior.
This workflow is for contributors only, not normal app users.
For end-user onboarding on macOS, use `docs/user-onboarding-macos.md`.

## One-time Setup

```bash
cp .env.example .env.local
```

Fill `.env.local` with your local values/secrets.
Best practice:
- commit `.env.example` only
- never commit `.env` / `.env.local`
- keep only minimum keys in `.env.local`; add overrides only when needed

Both deploy and live E2E scripts auto-load `.env.local` and `.env` when present.
Shell-exported env vars always win over file values.

## Prerequisites

- SSH access to HA host
- Home Assistant CLI (`ha`) available on host
- Local app repository available (`/addons/local`)

Required env:

```bash
export HA_HOST='<ha-host-or-ip>'
export HA_SSH_KEY='<path-to-private-key>'
export RELAY_BASE_URL='http://<ha-host>:8791'
export RELAY_AUTH_TOKEN='<relay-auth-token>'
export HA_LLAT='<required-llat>'
```

Optional env:

```bash
export SSH_USER='root'
export SSH_PORT='22'
export APP_SLUG='ha_nova_relay'
export SUPERVISOR_SLUG='local_ha_nova_relay'
```

Note:
- `APP_SLUG`/`SUPERVISOR_SLUG` are advanced overrides.
- For standard setup, defaults are enough.

## Developer Bootstrap (Separate Helper)

Use this only for active App development on a local HA host:

```bash
npm run dev:app:bootstrap
```

What it does:
- syncs this repository to `/addons/local/<app_slug>` on HA host
- prepares local Supervisor build context for this project layout
- reloads store + installs app if missing
- validates/writes options (`relay_auth_token`, required `ha_llat`)
- starts/restarts app and prints status + logs

Requirements:
- `RELAY_AUTH_TOKEN` must be set
- remote shell must have `SUPERVISOR_TOKEN` env (standard in HA SSH App shell)

Note:
- This is a developer helper, not end-user onboarding.

## Fast Iteration (Default)

Use for normal code changes:

```bash
npm run deploy:app:fast
```

What it does:
- `ha store reload`
- ensure app installed
- detect static metadata drift (`ingress`, port mappings) and auto-reinstall app when Supervisor cache is stale
- `ha apps rebuild <slug>` (or update fallback when Supervisor requires it)
- `ha apps start <slug>`
- print app info + recent logs

## Cache-Break Iteration (Aggressive)

Use when changes are not visible:

```bash
npm run deploy:app:clean
```

Extra steps vs `fast`:
- stop app
- remove cached app images matching `addon-<app_slug>`
- rebuild + start again

## Verification

Fast daily readiness check (recommended before heavy tests):

```bash
npm run onboarding:macos:quick
```

What it validates:
- onboarding doctor checks (HA + Relay + Keychain token)
- local Codex skill installation marker
- fresh-session readiness prompt

Real user-like live E2E through Codex agent:

```bash
npm run e2e:skill:codex
```

What it validates:
- runs a real `codex exec --json` session
- enforces local `ha-nova` skill usage
- executes automation CRUD scenario with marker-based assertions
- stores machine-readable session log for audit/debug

Optional override:
- `AUTOMATION_ID` to set a custom test automation id
- `OUTPUT_DIR` to keep logs in a fixed path
- `E2E_SUBAGENT_POLICY=allow|deny` (`allow` default for realistic user-like agent behavior)

After deploy, run live checks:

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='ha_nova_relay' \
RELAY_BASE_URL='http://<ha-host>:8791' \
RELAY_AUTH_TOKEN='<relay-auth-token>' \
HA_LLAT='<user-generated-llat>' \
npm run smoke:app:e2e -- --apply
```

For quick regression (without option rewrite/restart side effects):

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='ha_nova_relay' \
RELAY_BASE_URL='http://<ha-host>:8791' \
RELAY_AUTH_TOKEN='<relay-auth-token>' \
HA_LLAT='<user-generated-llat>' \
npm run smoke:app:e2e
```

`SUPERVISOR_TOKEN` handling:
- Optional for runtime-only check (`health/ws`) when app options are already correct.
- Required for Supervisor preflight (`/addons/.../info`, `validate`) and `--apply`.

## One-Command MVP Validation (Automation CRUD)

Use this when you want an end-to-end contributor smoke for current MVP behavior:

```bash
npm run smoke:app:mvp
```

What it validates:
- onboarding health (`doctor`)
- SSH reachability to HA host
- automation CRUD in App container context (`create -> read -> update -> read -> delete -> verify 404`)
- deterministic cleanup via shell trap

Defaults:
- `HA_SSH_KEY` falls back to `${HOME}/.ssh/ha_mcp` if not set.
- test automation id defaults to `nova_mvp_crud_smoke` (override with `MVP_AUTOMATION_ID`).

Positioning:
- `onboarding:macos:quick`: fastest readiness gate (daily).
- `e2e:skill:codex`: user-like live skill path validation.
- `smoke:app:mvp`: deep App-container CRUD validation.

## Cache Notes

- Frontend app-store cache can hide metadata changes; script avoids UI dependency by using `ha apps ...` directly.
- If `config.yaml` changed and UI still looks stale, run `deploy:app:clean` once.
- If rebuild is rejected due Supervisor state, script falls back to `ha apps update` where required.
