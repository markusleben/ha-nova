# App Bootstrap Deploy Flow (First Install + Option Set)

Date: 2026-02-26

## Goal

Provide one developer-only command that bootstraps a local Home Assistant App deployment from this repo, including first install and dynamic option provisioning.

## Requirements

1. Sync source to `/addons/local/<app_slug>` on HA host.
2. Prepare build context used by Supervisor local app builds.
3. Ensure app is installable on first run.
4. Set required app options dynamically (`relay_auth_token`; optional `ha_llat`) via Supervisor API.
5. Start/restart app and print status/log tail.

## Inputs

Required:
- `HA_HOST`
- `HA_SSH_KEY`
- `RELAY_AUTH_TOKEN`

Optional:
- `HA_LLAT`
- `SSH_USER`, `SSH_PORT`
- `APP_SLUG`, `SUPERVISOR_SLUG`
- `WS_ALLOWLIST_APPEND`

## Behavior

- Load `.env.local`/`.env` as convenience (explicit shell env wins).
- Use `ha store reload` + `ha apps install` when missing.
- Use `SUPERVISOR_TOKEN` from remote shell env for `/addons/<slug>/options/validate` and `/options`.
- Keep `ha_llat` optional by omitting it when empty.

## Out of Scope

- No changes to runtime token precedence.
- No fallback behavior for missing required relay auth token.
