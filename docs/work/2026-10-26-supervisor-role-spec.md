# Supervisor access for HA NOVA (security spec)

Status: active

Date: 2026-10-26. Parent: `docs/work/2026-10-26-ha-ui-parity-spec.md` (batch 3b, Decision 3: a role change for the App is a separate security spec). Nothing here is built; `nova/config.yaml` keeps `hassio_role: default`.

## Finding (throwaway HAOS VM, 2026-10-26)

HAOS 18.3, Supervisor 2026.10.3, Core 2026.10.2, NOVA Relay App 0.11.2. Probed from inside the App container with the App's `SUPERVISOR_TOKEN`, which is how the Relay talks upstream (`nova/src/config/env.ts`: `ws://supervisor/core/websocket`; `/ws` forwards the message unchanged, `nova/src/http/handlers/ws-proxy.ts`).

- **WS `supervisor/api` through the Relay: refused for every endpoint and method**, reads included: `{"code":"unauthorized"}` for `/info`, `/os/info`, `/host/info`, `/network/info`, `/core/info`, `/supervisor/info`, `/addons`, `/store`, `/store/addons`, `/store/repositories`, `/addons/<slug>/info`, `/hardware/info`, `/host/disks/default/usage`, `/os/datadisk/list`, `/resolution/info`, `/host/services`, `/backups`, every `/logs` endpoint, `/stats`, and the write `POST /addons/<slug>/options {"auto_update": false}`. `hassio/update/addon` and `supervisor/subscribe` are refused the same way. The Supervisor log names the cause: `[supervisor.api.proxy] [2368fcfa_ha_nova_relay] Blocked WebSocket message with disallowed or malformed command types: ['supervisor/api']`.
- Cause: Supervisor's WS proxy for Apps drops every command type starting with `supervisor/` or `hassio/` before Core sees it, because Core would run it with its own fully privileged Supervisor token ([proxy.py `DENIED_WS_TYPE_PREFIXES`, `filter_app_commands=True`](https://github.com/home-assistant/supervisor/blob/2026.10.3/supervisor/api/proxy.py)). It does not look at `hassio_role`: **no role change opens this path**. Core alone would allow it, since the Relay's upstream user is `Supervisor` with `is_admin: true` ([hassio/websocket_api.py](https://github.com/home-assistant/core/blob/2026.10.2/homeassistant/components/hassio/websocket_api.py)). The REST proxy `/core/api/hassio/*` answers 403 (the existing matrix pin; Supervisor `BLACKLIST`).
- The same message with the owner's own token, straight to Home Assistant, works. The VM runbook installed the App this way, and setup's owner sign-in uses the same path (`cli/ha_supervisor_app.go`).
- **Direct Supervisor REST with the App's token, today's `default` role:** 200 for `/info`, `/os/info`, `/host/info`, `/network/info`, `/core/info`, `/supervisor/info`, `/addons/<slug>/info`, `/hardware/info`, `/resolution/info`, `/backups/info`, and the App's own `/addons/self/{info,logs,stats,options}`. 403 for `/addons`, `/store`, `/store/addons`, `/host/disks/default/usage`, `/os/datadisk/list`, `/supervisor/logs`, `/host/logs`, `/core/logs`, another App's `/logs` and `/options`, `/supervisor/stats`, and `/backups`. This matches the role table in [middleware/security.py](https://github.com/home-assistant/supervisor/blob/2026.10.3/supervisor/api/middleware/security.py): `default` = `/.+/info`; `homeassistant` adds `/core/.+`; `backup` adds `/backups.*`; `manager` adds `/addons…` (not `/security`), `/store.*`, `/host/.+`, `/network/.+`, `/os/.+` (not `datadisk/wipe` or `ssh/authorized_keys`), `/hardware/.+`, `/supervisor/.+`, `/core/.+`, `/backups.*`, `/mounts.*`, `/docker/.+`, and more; `admin` = everything.

Already covered without any of this: App start/stop/restart and host reboot/shutdown are Core services (`hassio.app_*`, `hassio.host_*`), typed via `ha-nova:service-call`; App updates via `ha-nova:updates`.

## What each UI item needs

| HA UI item | Supervisor endpoint | Minimum role (App token, direct REST) |
|---|---|---|
| System info: OS, host, Core, Supervisor versions | `/{os,host,core,supervisor}/info` | default |
| Network (read) | `/network/info` | default |
| Hardware | `/hardware/info` | default |
| Supervisor repairs list | `/resolution/info` | default |
| One App's details | `/addons/<slug>/info` | default |
| Apps list, store, repositories | `/addons`, `/store…` | manager |
| App install, uninstall, options, auto-update toggle | `/store/addons/<slug>/install`, `/addons/<slug>/{uninstall,options}` | manager |
| Storage usage, data disk | `/host/disks/default/usage`, `/os/datadisk/list` | manager |
| Host, Core, Supervisor, App logs | `/host/logs`, `/core/logs`, `/supervisor/logs`, `/addons/<slug>/logs` | manager (`homeassistant` covers Core logs only) |
| Network changes | `/network/interface/<if>/update` | manager |

`admin` is never needed for these.

## Options

- **A. Relay route, today's role.** A GET-only Relay pass-through to the default-role reads above. The exact endpoint list lives in the Relay, not only in the CLI guard, because a device credential reaches the Relay without the CLI. Unlocks System → info, Network (read), Hardware, App details. No role change, but it is a Relay change and a new cross-component contract (effort L), so it needs a high-risk review. Open question for the review: what `/addons/<slug>/info` returns to an App caller for another App (its `options` can hold passwords). The response must be redacted, or that endpoint left out.
- **B. A + `hassio_role: manager`.** Unlocks every row above. Risks the review must weigh: `manager` can add any store repository and install any App, including one that asks for full host access, so a Relay bug or a stolen device credential becomes host-level control. It can also uninstall or reconfigure any App (this one included), cut the home off its network (`/network`), reboot or update the OS (`/os`, `/host`), stop or update Core (`/core`), download or restore backups (`/backups`, which hold every stored credential), and read network-share and registry credentials (`/mounts`, `/docker`). Existing installs would get the role through an App update; whether Supervisor asks the owner first is unverified. Mitigation: a fixed endpoint+method allowlist in the Relay plus the CLI tiers (reads ordinary; install, uninstall, and network changes typed; options previewed; HA NOVA's own App, `/backups`, `/core`, `/supervisor`, `/os`, `/host` writes, `/mounts`, `/docker`, and `/store/repositories` refused). It still widens what a Relay bug can reach.
- **C. Owner sign-in per task (desktop CLI only).** Reuse setup's owner sign-in: the owner's own token sends `supervisor/api` to Core, which works today. No role change and nothing new in the Relay. It needs a person at the computer for every task, and chat apps can't use it.

Recommendation: A first for the read pages (no new privilege). Decide B against C for writes in the security review. The review needs 2-3 adversarial lenses (security/fail-open over the Relay allowlist, the sibling-path sweep over every Supervisor route, UX) and a test-bed proof on this VM.

## Known limits

- Supervisor may change its proxy filter or role table; this spec is pinned to 2026.10.3.
- Standalone container installs have no Supervisor, so none of this applies there.
