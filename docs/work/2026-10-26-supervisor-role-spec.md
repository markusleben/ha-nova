# Supervisor access for HA NOVA (security spec)

Status: active

Date: 2026-10-26. Parent: `docs/work/2026-10-26-ha-ui-parity-spec.md` (batch 3b, Decision 3: a role change for the App is a separate security spec). Built 2026-10-27 as option B, narrow: see Decisions below.

## Finding (throwaway HAOS VM, 2026-10-26)

HAOS 18.3, Supervisor 2026.10.3, Core 2026.10.2, NOVA Relay App 0.11.2. Probed from inside the App container with the App's `SUPERVISOR_TOKEN`, which is how the Relay talks upstream (`nova/src/config/env.ts`: `ws://supervisor/core/websocket`; `/ws` forwards the message unchanged, `nova/src/http/handlers/ws-proxy.ts`).

- **WS `supervisor/api` through the Relay: refused for every endpoint and method**, reads included: `{"code":"unauthorized"}` for `/info`, `/os/info`, `/host/info`, `/network/info`, `/core/info`, `/supervisor/info`, `/addons`, `/store`, `/store/addons`, `/store/repositories`, `/addons/<slug>/info`, `/hardware/info`, `/host/disks/default/usage`, `/os/datadisk/list`, `/resolution/info`, `/host/services`, `/backups`, every `/logs` endpoint, `/stats`, and the write `POST /addons/<slug>/options {"auto_update": false}`. `hassio/update/addon` and `supervisor/subscribe` are refused the same way. The Supervisor log names the cause: `[supervisor.api.proxy] [2368fcfa_ha_nova_relay] Blocked WebSocket message with disallowed or malformed command types: ['supervisor/api']`.
- Cause: Supervisor's WS proxy for Apps drops every command type starting with `supervisor/` or `hassio/` before Core sees it, because Core would run it with its own fully privileged Supervisor token ([proxy.py `DENIED_WS_TYPE_PREFIXES`, `filter_app_commands=True`](https://github.com/home-assistant/supervisor/blob/2026.10.3/supervisor/api/proxy.py)). It does not look at `hassio_role`: **no role change opens this path**. Core alone would allow it, since the Relay's upstream user is `Supervisor` with `is_admin: true` ([hassio/websocket_api.py](https://github.com/home-assistant/core/blob/2026.10.2/homeassistant/components/hassio/websocket_api.py)). The REST proxy `/core/api/hassio/*` answers 403 (the existing matrix pin; Supervisor `BLACKLIST`).
- The same message with the owner's own token, straight to Home Assistant, works. The VM runbook installed the App this way, and setup's owner sign-in uses the same path (`cli/ha_supervisor_app.go`).
- **Direct Supervisor REST with the App's token, today's `default` role:** 200 for `/info`, `/os/info`, `/host/info`, `/network/info`, `/core/info`, `/supervisor/info`, `/addons/<slug>/info`, `/hardware/info`, `/resolution/info`, `/backups/info`, and the App's own `/addons/self/{info,logs,stats,options}`. 403 for `/addons`, `/store`, `/store/addons`, `/host/disks/default/usage`, `/os/datadisk/list`, `/supervisor/logs`, `/host/logs`, `/core/logs`, another App's `/logs` and `/options`, `/supervisor/stats`, and `/backups`. This matches the role table in [middleware/security.py](https://github.com/home-assistant/supervisor/blob/2026.10.3/supervisor/api/middleware/security.py): `default` = `/.+/info`; `homeassistant` adds `/core/.+`; `backup` adds `/backups.*`; `manager` adds `/addons…` (not `/security`), `/store.*`, `/host/.+`, `/network/.+`, `/os/.+` (not `datadisk/wipe` or `ssh/authorized_keys`), `/hardware/.+`, `/supervisor/.+`, `/core/.+`, `/backups.*`, `/mounts.*`, `/docker/.+`, and more; `admin` = everything.

Already covered without any of this, because Home Assistant calls the Supervisor with its own token:

- App start/stop/restart and host reboot/shutdown: Core services `hassio.addon_*` and `hassio.host_*`, typed via `ha-nova:service-call` (Core 2026.10.2 also has `hassio.app_*`; the skill names only `addon_*`). App updates: `ha-nova:updates`.
- Disk total and used, OS, Supervisor, and Docker versions, board, host connectivity: `system_health/info`, read by `ha-nova:health` ([hassio/system_health.py](https://github.com/home-assistant/core/blob/2026.10.2/homeassistant/components/hassio/system_health.py)).
- Home Assistant's own network adapters and URLs: Core WS `network` and `network/url` (not `supervisor/*` types; both answered through the Relay's upstream on the VM). Neither is allowlisted yet: a follow-up of one allowlist line plus an owner (S); the `admin` skill sits at its word budget. URLs already come from `get_config`.

## What each UI item needs

| HA UI item | Supervisor endpoint | Minimum role (App token, direct REST) |
|---|---|---|
| System info beyond `system_health/info` | `/{os,host,core,supervisor}/info` | default |
| Host network setup (read) | `/network/info` | default |
| Hardware | `/hardware/info` | default |
| Supervisor repairs list | `/resolution/info` | default |
| One App's details | `/addons/<slug>/info` | default |
| Apps list, store, repositories | `/addons`, `/store…` | manager |
| App install, uninstall, options, auto-update toggle | `/store/addons/<slug>/install`, `/addons/<slug>/{uninstall,options}` | manager |
| Storage breakdown, data disk (total and used are in `system_health/info`) | `/host/disks/default/usage`, `/os/datadisk/list` | manager |
| Host, Core, Supervisor, App logs | `/host/logs`, `/core/logs`, `/supervisor/logs`, `/addons/<slug>/logs` | manager (`homeassistant` covers Core logs only) |
| Host network changes | `/network/interface/<if>/update` | manager |

`admin` is never needed for these.

## Options

- **A. Relay route, today's role.** A GET-only Relay pass-through to the default-role reads above. The exact endpoint list lives in the Relay, not only in the CLI guard, because a device credential reaches the Relay without the CLI. Unlocks System → info, host network (read), Hardware, App details. No role change, but it is a Relay change and a new cross-component contract (effort L), so it needs a high-risk review. The list is exact method + path: the role's `/.+/info` suffix also reaches `/backups/<slug>/info` and `/store/addons/<slug>/info`. Another App's `/addons/<slug>/info` comes back with `options: {}` under `default` ([api/apps.py](https://github.com/home-assistant/supervisor/blob/2026.10.3/supervisor/api/apps.py) `expose_options`: only the App itself, `manager`, and `admin` see them).
- **B. A + `hassio_role: manager`.** Unlocks every row above. Risks the review must weigh: `manager` reads every App's options in clear through `/addons/<slug>/info` (MQTT, Z-Wave, and database passwords, API keys), so the Relay would have to strip `options` itself. It can add any store repository and install any App; it cannot turn off Protection mode (`/addons/<slug>/security` is excluded), but an App that maps the Home Assistant config read-write (`.storage/auth`, `secrets.yaml`) or uses the host network is enough, so a Relay bug or a stolen device credential becomes control of the home. It can set the host's DNS servers (`/dns`), relax content-trust checks (`/security/options`), switch the update channel (`/supervisor/options`), send input to other Apps (`/addons/<slug>/stdin`), uninstall or reconfigure any App (this one included), cut the home off its network (`/network`), reboot or update the OS (`/os`, `/host`), stop or update Core (`/core`), download or restore backups (`/backups`, which hold every stored credential), and read network-share and registry credentials (`/mounts`, `/docker`). Existing installs would get the role through an App update; whether Supervisor asks the owner first is unverified. Mitigation: a fixed endpoint+method allowlist in the Relay plus the CLI tiers (reads ordinary; install, uninstall, and network changes typed; options previewed; HA NOVA's own App, `/backups`, `/core`, `/supervisor`, `/os`, `/host` writes, `/dns`, `/security`, `stdin`, `/mounts`, `/docker`, and `/store/repositories` refused). It still widens what a Relay bug can reach.
- **C. Owner sign-in per task (desktop CLI only).** Reuse setup's owner sign-in: the owner's own token sends `supervisor/api` to Core, which works today. No role change and nothing new in the Relay. It needs a person at the computer for every task, and chat apps can't use it.

Recommendation: A first for the read pages (no new privilege). Decide B against C for writes in the security review. The review needs 2-3 adversarial lenses (security/fail-open over the Relay allowlist, the sibling-path sweep over every Supervisor route, UX) and a test-bed proof on this VM.

## Known limits

- Supervisor may change its proxy filter or role table; this spec is pinned to 2026.10.3.
- Closed by Decision 3 (the Relay's own `/ws` and `/core` deny): a standalone Relay container pointed at an HA OS or Supervised Home Assistant (`HA_URL` plus an admin `HA_LLAT`, `docs/reference/relay-container.md`) has no Supervisor App filter in its path: Core would run `supervisor/api`, `hassio/*`, and `/api/hassio/*` with its own Supervisor token, and today only the CLI guard refuses them (`cli/relay_raw_guard.go`, `rawWSAllowed`), while a device credential reaches the Relay without the CLI. Recommended as its own high-risk PR: a deny in the Relay itself that mirrors the Supervisor's (WS types starting `supervisor/` or `hassio/`, also inside `collect_events`; `/api/hassio` and `/api/hassio_` paths after decoding). Read from code, not run.

## Decisions (maintainer, 2026-10-27)

Option **B, narrow**: `hassio_role: manager`, reachable only through one Relay route with a fixed allowlist. Nothing else of the role is reachable through HA NOVA. Pinned to Supervisor 2026.10.3 (v1 API; the `/v2` sub-app is not on the list).

1. **Role.** `nova/config.yaml` sets `hassio_role: manager` with a comment. No App version bump here; the next release-prep does it.
2. **One Relay route.** `POST /supervisor` with `{method, path, data?, lines?}` on the bootstrap (legacy) and device (TLS) listeners, App mode only (a standalone Relay has no such route: `404 NOT_FOUND`). Not on the Home Assistant Link (Cloud) functional routes in this change: the Cloud checklist runs with the maintainer, so Cloud stays as it is (known limit). The route forwards to `http://supervisor` with the `SUPERVISOR_TOKEN` only when `(method, path)` matches the static table below exactly (raw string: no query, `%`, `.` segments, doubled slashes, or trailing slash). `{slug}` = `[A-Za-z0-9][A-Za-z0-9_-]{0,63}` and not `self`; `{identifier}` = `[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}` (no `..`). Every other request is refused before anything is sent.

   | Method | Path | Kind |
   |---|---|---|
   | GET | `/info`, `/os/info`, `/host/info`, `/network/info`, `/hardware/info`, `/resolution/info`, `/core/info`, `/supervisor/info` | read |
   | GET | `/addons`, `/addons/{slug}/info`, `/store`, `/store/addons`, `/store/addons/{slug}` | read |
   | GET | `/host/disks/default/usage`, `/os/datadisk/list`, `/host/logs/boots`, `/host/logs/identifiers` | read |
   | GET | `/supervisor/logs`, `/core/logs`, `/host/logs`, `/host/logs/identifiers/{identifier}`, `/addons/{slug}/logs` | logs (text) |
   | POST | `/store/addons/{slug}/install` | write, `data` empty |
   | POST | `/addons/{slug}/uninstall` | write, `data` empty or `{remove_config: bool}` |
   | POST | `/addons/{slug}/options` | write, `data` exactly `{options: object}` or `{auto_update: bool}` |

   - **Logs**: the Relay sets `?lines=N&no_colors` itself (`lines` 1-1000, default 100), asks for `text/plain`, and stops reading at 512 KiB (`truncated: true`). Follow, latest, and boot-offset log routes are not on the list.
   - **Redaction (every JSON reply)**: a value under a secret-like key (password, secret, token, key, psk, credential, cert, pin, code, llat, …) is replaced by `<hidden>`; booleans and null stay. In `/addons/{slug}/info`, every option the App's own schema marks `format: password` is hidden too. Options are kept otherwise: the agent needs them to describe and change an App.
   - **Options write is a merge**: the Relay reads the App's stored options with its own token and sends `{...stored, ...given}` (top-level keys), so a hidden credential never has to leave the Relay to keep its value. A given value of `<hidden>` is refused (CLI and Relay).
   - **Hard refusals**: any write to HA NOVA's own App (slug containing `ha_nova_relay`, or the slug `/addons/self/info` reports), `self`, `/dns*`, `/security*`, `/supervisor/options` and every other `/supervisor` write, `/auth*`, `/backups*` (restores and deletes stay with the typed `backup` skill paths), `/addons/{slug}/stdin|security|sys_options|rebuild|start|stop|restart|update` (start/stop/restart stay typed `hassio.addon_*` services; App updates stay with `updates`), `/host/reboot|shutdown|options` (typed `hassio.host_*` services), `/os/*` and `/network/*` writes, `/mounts*`, `/docker*`, `/jobs*`, `/store/repositories`, `/store/reload`, `/addons/reload`, `/refresh_updates`, `/discovery`, `/services`, `/core/*` writes and the `/core/api` proxy, and everything not listed.
3. **Gap closed in the same change**: the Relay's `/ws` refuses types starting `supervisor/` or `hassio/` (case-folded, also inside `collect_events`), and `/core` refuses decoded paths starting `/api/hassio` (`/api/hassio_*` included), mirroring the Supervisor's own App filter for a standalone Relay container.
4. **CLI**: endpoint `supervisor` through the shared guard (`cli/relay_raw_supervisor.go`) with the same table mirrored (a test checks both tables match). `ha-nova relay supervisor` and `ha_read` send reads only (64 KiB cap, `--jq`); writes go through `ha-nova plan raw` only. Tiers: install, uninstall, and any `options` change typed (App code, or options that can hold credentials and code); `auto_update` preview. Every reply passes the same key redaction in the CLI too.
5. **Skills**: `ha-nova:updates` owns Apps (list, store, install, uninstall, options, auto-update) in a short section; `ha-nova:diagnose` reads logs; the fallback EXTERNAL rows go.

Existing installs get the role with the next App update. Supervisor shows the App's role on its info page; whether it asks the owner first was not verified (known limit).

