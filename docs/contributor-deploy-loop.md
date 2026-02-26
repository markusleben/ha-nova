# Contributor Deploy Loop (HA App)

Date: 2026-02-26

## Goal

Fast, repeatable deploys to Home Assistant during development without relying on UI cache behavior.

## Prerequisites

- SSH access to HA host
- Home Assistant CLI (`ha`) available on host
- Local app repository already configured in HA
- App slug installed (default: `local_ha_nova_bridge`)

Required env:

```bash
export HA_HOST='<ha-host-or-ip>'
export HA_SSH_KEY='<path-to-private-key>'
```

Optional env:

```bash
export SSH_USER='root'
export SSH_PORT='22'
export APP_SLUG='ha_nova_bridge'
export SUPERVISOR_SLUG='local_ha_nova_bridge'
```

## Fast Iteration (Default)

Use for normal code changes:

```bash
npm run deploy:app:fast
```

What it does:
- `ha apps reload`
- ensure app installed
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

After deploy, run live checks:

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='ha_nova_bridge' \
BRIDGE_BASE_URL='http://<ha-host>:8791' \
BRIDGE_AUTH_TOKEN='<bridge-auth-token>' \
HA_LLAT='<user-generated-llat>' \
npm run smoke:app:e2e -- --apply
```

For quick regression (without option rewrite/restart side effects):

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='ha_nova_bridge' \
BRIDGE_BASE_URL='http://<ha-host>:8791' \
BRIDGE_AUTH_TOKEN='<bridge-auth-token>' \
npm run smoke:app:e2e
```

## Cache Notes

- Frontend app-store cache can hide metadata changes; script avoids UI dependency by using `ha apps ...` directly.
- If `config.yaml` changed and UI still looks stale, run `deploy:app:clean` once.
- If rebuild is rejected due Supervisor state, script falls back to `ha apps update` where required.
