# Phase 1a.4 Live E2E Status

Date: 2026-02-26

## Live Environment Gate Check

Data source: Home Assistant `system_manager.get_addon` (installed + available)

Findings:
- Installed apps include `local_ha_mcp_server`.
- `ha-nova` / slug `ha_nova_relay` is not present in installed list.
- `ha-nova` is not present in available list from configured repositories.

Conclusion:
- Live E2E against `ha-nova` is currently blocked by deployment/repository availability in Home Assistant.
- Code-side E2E harness is ready: `npm run smoke:app:e2e`.

## Next Unblock Action

1. Publish or mount `ha-nova` app into a configured HA app repository.
2. Install app in HA (`ha_nova_relay`).
3. Run:

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='ha_nova_relay' \
RELAY_BASE_URL='http://<ha-host>:8791' \
RELAY_AUTH_TOKEN='<relay-auth-token>' \
HA_LLAT='<user-generated-llat>' \
npm run smoke:app:e2e -- --apply
```
