# Home Assistant Doc Gate (Mandatory Before Planning)

Date: 2026-02-26

## Rule

Before any new implementation plan, review current official Home Assistant docs in three buckets:

1. Developer APIs (REST, WS, Auth, Supervisor)
2. Apps (formerly add-ons): configuration, communication, testing, security
3. User docs: automations/scripts/dashboard/common tasks

No plan starts before this gate is checked.

## Reviewed Sources (Official)

### 1) Developer + API
- https://developers.home-assistant.io/docs/api/rest/
- https://developers.home-assistant.io/docs/api/websocket/
- https://developers.home-assistant.io/docs/auth_api/
- https://developers.home-assistant.io/docs/api/supervisor/endpoints/

### 2) Apps / Add-ons
- https://developers.home-assistant.io/docs/apps/configuration/
- https://developers.home-assistant.io/docs/apps/communication/
- https://developers.home-assistant.io/docs/apps/testing/
- https://developers.home-assistant.io/docs/apps/security/
- https://developers.home-assistant.io/docs/apps/tutorial/

### 3) User Docs
- https://www.home-assistant.io/docs/
- https://www.home-assistant.io/common-tasks/general/
- https://www.home-assistant.io/docs/automation/editor/
- https://www.home-assistant.io/docs/automation/modes/
- https://www.home-assistant.io/docs/tools/dev-tools/
- https://www.home-assistant.io/docs/configuration/secrets/

## Hard Findings For ha-nova

1. App options persist in `/data/options.json`; this is the right place for persistent LLAT storage in app config.
2. Supervisor proxy supports `http://supervisor/core/api` and `ws://supervisor/core/websocket` via `SUPERVISOR_TOKEN`.
3. Supervisor endpoint supports `/addons/<addon>/options` and `/addons/<addon>/options/validate`; `self` slug is supported for app-scoped calls.
4. LLAT remains valid for long-lived integration scenarios and is created in user profile.
5. Official local app testing baseline is devcontainer-based with Supervisor + Home Assistant.
6. Security guidance favors least-privilege roles, no host-network unless required, and ingress/auth best practices.

## Planning Checklist (Must Pass)

- [ ] API transport choice mapped (REST vs WS vs Supervisor)
- [ ] Auth model mapped (`SUPERVISOR_TOKEN` vs LLAT) with fallback behavior
- [ ] App option schema impact checked (`config.yaml` options/schema)
- [ ] Local test path defined (unit + integration + local app testing)
- [ ] User-facing impact checked against user docs workflow
