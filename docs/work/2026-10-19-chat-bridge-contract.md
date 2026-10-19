# Chat-app bridge contract: `ha-nova serve` (2026-10-19)

Status: active

Parent spec: `docs/work/2026-10-18-onboarding-0.27-spec.md` §11. Scope: the contract between `ha-nova serve` (the bridge, PR train B1), the NOVA App that runs it and writes its files (B2), and the Home Assistant integration that forwards to it (B3). Behaviour beyond this contract is the code in `cli/mcp_serve.go` and `cli/mcp_serve_phone.go`.

## Path

Chat app → Home Assistant OAuth → integration view `/api/ha_nova/mcp` (pure pass-through) → `ha-nova serve` on an internal port of the App container → Relay → Home Assistant.

## Command

`ha-nova serve --listen <host:port> --data-dir <dir> --secret-file <file> [--confirm-wait 25s] [--server <name>]`

- `--listen`: an explicit host is required (the App passes `0.0.0.0:<port>`, never mapped to the host).
- `--secret-file`: exactly 64 lowercase hex characters (256 bits), mode `0600`. Read once at start.
- `--confirm-wait`: 1 s to 2 min; how long one `ha_apply` waits for a phone tap.

## HTTP

- `POST /mcp`, one JSON-RPC message per request, one JSON answer (`application/json`). Notifications and responses get `202` with no body. `GET` and `DELETE` get `405`. No SSE, no sessions (`Mcp-Session-Id` is accepted and ignored; none is issued), no elicitation.
- `Mcp-Protocol-Version`: a legacy revision (`2025-11-25`, `2025-06-18`, `2024-11-05`) selects that era; absent means `2025-11-25`; `2026-07-28` requests carry `_meta` per request; any other value gets `400`.
- Body limit 1 MiB.

## Authentication (every request)

1. Exactly one `Authorization: Bearer <secret>`, compared in constant time.
2. Exactly one `X-HA-User-Id`: the Home Assistant user the integration authenticated (`^[0-9a-f]{32}$`).
3. That user is allowed (below).

Refusals: 1–2 → `401`, 3 → `403`, unreadable settings or a failed owner read → `503`. The integration must set both headers itself, from its own secret and `request["hass_user"]`, and must drop any client-sent copies. The user header is trusted only together with the secret.

## Files in `--data-dir` (written by the App)

- `chat-apps.json`, re-read on every request. Unknown fields or trailing data refuse every request.
  - `allowed_users`: list of HA user ids. Absent means the HA owner only; an empty list allows nobody.
  - `read_only` (default `false`): refuses `ha_call`, `ha_plan`, and `ha_apply`.
  - `chat_codes` (default `true`): with `false`, a typed plan needs the user's linked phone.
- `phone-links.json`: `{"<HA user id>": {"service", "device_id", "user_id", "name"}}`, the bridge's only link store (never a computer's `config.json`). A link's `user_id` must equal its key; `null` or a malformed entry is an unusable link (refused, never "no phone"). An unreadable file refuses typed plans.
- Write both files atomically (temporary file plus rename): a missing `chat-apps.json` means the defaults.
- `users/<hash>/`: owned by the bridge (per-user plans).

## Tools and confirmations

- The same 10 tools, server instructions, and verbs as `ha-nova mcp`. Plans and pending phone asks are bound to the HA user that made them; another user's plan id (in `ha_apply` and in a `waiting_for_phone` re-call) answers exactly like an unknown plan.
- Typed plans: the linked phone of the requesting HA user (an active administrator, as in the CLI), else a chat code with the fixed low-protection note, unless `chat_codes` is `false`. Never a window or a form.
- `ha_apply` on a phone plan waits up to `--confirm-wait` (default 25 s pending the Home Assistant Link timeout test; spec §11.4 names 45 s), then returns `status: waiting_for_phone`. Calling again joins the same ask (no new notification). A tap applies at once; the owner's switches are re-checked before it applies. One open phone ask per user (`CONFIRM_PENDING` otherwise). An ask stays open 3 min or until the plan expires.
- The integration's own timeout must exceed `--confirm-wait` plus about 10 s, and must map its timeout to a readable error.

## App side (B2)

Code: `nova/src/runtime/chat-apps.ts`, `nova/src/http/handlers/chat-apps.ts`, `cli/mcp_serve_relay.go`. Built only when the image sets `HA_NOVA_CHAT_APPS=1` (`nova/Dockerfile` `ARG`, default `0`); until then nothing below runs, and no route or page section exists.

- **Files** (`/data/chat-apps/`, written atomically, `0600`): `state.json` (`enabled`, `discovery_uuid`), `secret`, `relay.json`, `bridge/` (the `--data-dir`), `home/` (the bridge's `HOME`).
- **Secret:** 32 random bytes as 64 hex, made on the first turn-on, kept across App restarts and updates, deleted only by turning off.
- **Internal device credential:** the App mints an ordinary device record in its own registry (`client_install_id` `nova-app-chat-bridge`: a re-mint replaces the old record, and pairing refuses that id) and writes `relay.json` `{"relay_url": "https://127.0.0.1:8792", "spki_pin", "credential"}`. `ha-nova serve --relay-file` uses only that: the Relay's pinned TLS port over loopback, device mode, no `config.json`, keyring, Cloud route, or pairing code. Reused while it resolves (a new TLS pin is written into the same file); re-minted before each bridge start otherwise (registry reset, revocation). The NOVA page never lists it under Devices, and `GET /auth/device/peers` leaves it to `chat_app_files`.
- **Process:** `ha-nova serve --listen 0.0.0.0:8794 --data-dir … --secret-file … --relay-file …` with only `PATH`, `HOME`, `TZ`, `HA_NOVA_NO_BROWSER` in its environment (never the Supervisor or HA token). 8794 is not in `config.yaml` `ports:`, so only the Supervisor network reaches it. While on, an exit restarts it after 5 s, backing off to 5 min while it keeps failing; turning off sends SIGTERM, then SIGKILL after 7 s.
- **Discovery:** `POST /discovery` `{"service": "ha_nova", "config": {"host": <App hostname from /addons/self/info>, "port": 8794, "secret": <64 hex>}}`; the returned uuid is kept. Supervisor keeps one message per App and service, so the App re-posts on every start while on (an unchanged message is a no-op). Turning off sends `DELETE /discovery/<uuid>`; a 404 counts as gone.
- **Integration files:** only `/config/custom_components/ha_nova`, copied from the image's `/app/integration/ha_nova` (B3 adds it; without it the feature reports unavailable). `custom_components` must be a real folder, the old `ha_nova` is removed (a symlink is unlinked, never followed), files are created exclusively (`0644`). Written on turn-on only; refreshing it on an App update is B3's call.
- **Owner action** (NOVA page form, or `GET`/`POST /chat-apps/v1/state` over ingress: owner only, machine only, JSON only). `POST` fields, all optional: `enabled`, `allow_restart`, `read_only`, `chat_codes`, `owner_phone` (`{service, device_id, name}` or `null`; stored for the owner's own user id). On/off runs first, so a refusal changes nothing else. On and off (and the start-up resume) never interleave.
  - Turning on refuses, before any change: `409 HA_TOO_OLD` (Home Assistant before 2026.10); `409 RESTART_CONSENT_REQUIRED` when Home Assistant does not run `ha_nova` yet and `allow_restart` is not `true`; `409 HA_CONFIG_INVALID` when its configuration check fails. Then secret, files, credential, serve, discovery, and only then `enabled: true` (a failure leaves it off, so [Turn on] is the retry). The answer comes first; `homeassistant.restart` follows a second later (Home Assistant carries the ingress connection). The page's [Turn on chat apps] sits under "Home Assistant restarts once (about a minute)." and is that consent.
  - Turning off: serve stops and its credential is revoked first, then the discovery message, the secret, and the folder; a failed step leaves `unfinished`, and the same action finishes it. An App version that can't run chat apps still offers [Turn off].

### Apply in the App release bump (not before)

Supervisor stores read `nova/config.yaml` from `main`, so these land only with the version bump that publishes the image containing B2 and B3:
- `nova/config.yaml`: `discovery: [ha_nova]` (without it Supervisor refuses `POST /discovery` with 403), and the `map:` comment ("only USES it when file_access is set") now also names chat apps. No new option, port, or map entry.
- `nova/Dockerfile`: `ARG HA_NOVA_CHAT_APPS=1`, and the B3 integration copied to `/app/integration/ha_nova`.
- The bump comes with or after the final `skill_version`: `app-image.yml` stages the bridge (CLI + skills) from that same tree (`scripts/release/stage-app-bridge.sh`).
- Docs: `nova/DOCS.md` and `nova/CHANGELOG.md` (chat apps, turn them off before removing the App), `docs/reference/safety.md` (the token-scope limit, spec §15 R4), `docs/releasing.md` lanes (on, off, App update, rollback, registry reset).

### Known limits (B2)

- Removing the App, or going back to an App without chat apps, while they are on leaves the discovery message (until Supervisor drops it) and the folder; an App version with the code but without the bridge offers only [Turn off].
- A request already running when chat apps turn off may finish within serve's 5 s shutdown, but its Relay credential is revoked at once.
- The page names the connector path, not the full Link address, and shows "On" before Home Assistant has restarted; both are B4.
- Whether deleting the discovery message removes the config entry depends on the entry's unique ID: B3 verifies on the test HAOS VM (spec §11.6 says the App slug; Home Assistant's discovery removal may match the uuid).
