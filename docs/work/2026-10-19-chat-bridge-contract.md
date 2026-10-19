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
- **Integration files:** only `/config/custom_components/ha_nova`, copied from the image's `/app/integration/ha_nova` (B3 adds it; without it the feature reports unavailable). `custom_components` must be a real folder, the old `ha_nova` is removed (a symlink is unlinked, never followed), files are created exclusively (`0644`). Written on turn-on only, never refreshed on an App update (B3's decision, below).
- **Owner action** (NOVA page form, or `GET`/`POST /chat-apps/v1/state` over ingress: owner only, machine only, JSON only). `POST` fields, all optional: `enabled`, `allow_restart`, `read_only`, `chat_codes`, `owner_phone` (`{service, device_id, name}` or `null`; stored for the owner's own user id). On/off runs first, so a refusal changes nothing else. On and off (and the start-up resume) never interleave.
  - Turning on refuses, before any change: `409 HA_TOO_OLD` (Home Assistant before 2026.10); `409 RESTART_CONSENT_REQUIRED` when Home Assistant does not run `ha_nova` yet and `allow_restart` is not `true`; `409 HA_CONFIG_INVALID` when its configuration check fails. Then secret, files, credential, serve, discovery, and only then `enabled: true`; a failure on a first turn-on undoes every step (it stays off, and [Turn on] is the retry). The answer comes first; `homeassistant.restart` follows a second later (Home Assistant carries the ingress connection). The page's [Turn on chat apps] sits under "Home Assistant restarts once (about a minute)." and is that consent.
  - Turning off: serve stops, a restart not yet sent is cancelled, and every registry record with the reserved install id is revoked before any chat-app file is read; then the discovery message, the secret, and the folder; a failed step leaves `unfinished`, and the same action finishes it. An App version that can't run chat apps still offers [Turn off].

### Apply in the App release bump (not before)

Supervisor stores read `nova/config.yaml` from `main`, so these land only with the version bump that publishes the image containing B2 and B3:
- `nova/config.yaml`: `discovery: [ha_nova]` (without it Supervisor refuses `POST /discovery` with 403), and the `map:` comment ("only USES it when file_access is set") now also names chat apps. No new option, port, or map entry.
- `nova/Dockerfile`: `ARG HA_NOVA_CHAT_APPS=1` (the B3 integration is already copied to `/app/integration/ha_nova`).
- The bump comes with or after the final `skill_version`: `app-image.yml` stages the bridge (CLI + skills) from that same tree (`scripts/release/stage-app-bridge.sh`).
- Gates before the bump: R9 (below) answered by the maintainer's claude.ai test, and the real-Supervisor discovery lane (spec §12) green.
- Docs: `nova/DOCS.md` and `nova/CHANGELOG.md` (chat apps, turn them off before removing the App; link `docs/reference/chat-apps.md`), `docs/reference/safety.md` (the token-scope limit, spec §15 R4), `docs/releasing.md` lanes (on, off, App update, rollback, registry reset); drop the "Status" line of `docs/reference/chat-apps.md`.
- CLI (B4): add `ha-nova chat-apps [on|off|status]` to `printUsage` and drop it from the "unadvertised" check in `cli/help_test.go`.
- README (release prep, maintainer's call): the B4 PR body lists the proposed sentences.

### Known limits (B2)

- Removing the App, or going back to an App without chat apps, while they are on leaves the discovery message (until Supervisor drops it) and the folder; an App version with the code but without the bridge offers only [Turn off].
- A request already running when chat apps turn off may finish within serve's 5 s shutdown, but its Relay credential is revoked at once.
- The NOVA page names the connector path, not the full Link address, and shows "On" before Home Assistant has restarted (setup's step 3 and the CLI show the full address and wait for the restart, B4).
- A discovery message Supervisor accepted while its answer was lost (timeout) has no stored uuid, so turning off can't delete it; it names a stopped bridge and a deleted secret. A discovery re-post that fails at start-up is retried only at the next App start.
- A device that paired with the install id `nova-app-chat-bridge` before pairing refused it would be treated as the bridge (the CLI never sends that id).

## Setup and CLI (B4)

Code: `cli/chat_apps.go`, `cli/command_chat_apps.go`, the step 3 row in `cli/setup_page_control.go` and `cli/setupweb/steps.js`. User doc: `docs/reference/chat-apps.md`.

- **Owner session:** the ingress session of this run's owner sign-in (memory only, `setupOwnerIngress`), the same one that armed the pairing; cleared when a pairing attempt starts, so only a sign-in pairing in that attempt sets it. The row also needs a pairing in this run (as the phone row). It calls `GET`/`POST /chat-apps/v1/state` (`CloudEndpointChatApps*`), never with a device credential. A six-digit-code pairing has no owner session: no row, no question (the NOVA page or `ha-nova chat-apps on` instead).
- **When the row shows (feature detection, no version floor):** the `GET` answers `available: true` (a dormant App answers 404: nothing shows), Home Assistant is 2026.10 or newer (`/api/config` through this computer's Relay), and the connector has an origin: Link's address while its remote access answers, else Home Assistant's `external_url` when it is public HTTPS (`https://`, a hostname with a dot, no IP literal, no local-only suffix such as `.local`, no path, query, or user info; a `*.nabu.casa` address counts only through Link). Neither: the row is one plain line, no switch (`noaddress`); the text path prints that line. Already on: the row shows the address. The NOVA page picks the same origin from `cloud/status` and `get_config` (`chatConnectorUrl`).
- **Add to Claude:** wherever the address shows (step 3 when on, step 4, the NOVA page, `ha-nova chat-apps on|status`), a link to Claude's documented install URL `https://claude.ai/customize/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=<percent-encoded address>` (`claudeConnectorLink`, Go and TS). It carries the address only, never a token; Claude shows the values and asks before adding. The page renders it only when it starts with `https://claude.ai/`. ChatGPT has no documented prefill link: the copy bar and the steps stay.
- **Consent:** the switch only opens the consent ("… Home Assistant restarts once (about a minute)."); "Turn on and restart Home Assistant" posts the page action `chat` with value `on` (A6 action rules), and only that sends `enabled: true, allow_restart: true`. The text path asks the same in one arrow menu, default "Not now"; without the menu only a typed `yes` consents. `ha-nova chat-apps on` checks Home Assistant and Link, signs the owner in, then asks (the consent names the phone that will confirm, or says a code shows in the chat). Finish is disabled while the page's consent is open.
- **Owner phone:** a phone linked on this computer by the owner's own tap goes to the App as `owner_phone` in its own request after the turn-on (also when step 3 links it after chat apps are on), unless the App has one already (spec §11.4).
- **Ready:** step 3 waits until the `ha_nova` config entry is `loaded` (`config_entries/get` through the Relay), showing the restart (Home Assistant stops answering, then answers again); with a restart to come, only an entry loaded after Home Assistant was down counts. 5 min at most, then one line (restart Home Assistant yourself) and the address anyway. Ctrl+C while waiting: the page ends setup as on step 3's wait; the terminal says on, not started yet. Step 4's notes repeat the address.
- **Failures:** a lost answer (network, timeout, a 5xx other than `OWNER_CHECK_UNAVAILABLE`) is re-read with `GET`: when the App says on, the wait continues; when they were on before, or the re-read fails, the line says HA NOVA couldn't tell. One plain line per App refusal (`HA_TOO_OLD`, `HA_CONFIG_INVALID`, `CONFIG_*`, other), the switch stays and [Turn on] runs the whole action again; an expired ingress session (Supervisor ends it 15 min after its last use) points to the NOVA page.
- **Off:** the NOVA page, or `ha-nova chat-apps off` (owner sign-in, `enabled: false`). Step 3 offers no off.
- **`ha-nova chat-apps status`:** no sign-in; "on" means the `ha_nova` entry is loaded.
- **Refused user:** the bridge answers a signed-in user who may not use chat apps with `403` and a JSON-RPC error (the request's id when it is a string or number, else `null`; code `-32001`): "Only the Home Assistant owner can use HA NOVA in chat apps. Sign in as the owner." (with an explicit `allowed_users` list: "… Sign in with an account the owner allowed.").

## Integration (B3)

Code: `nova/integration/ha_nova/` (copied into the image at `/app/integration/ha_nova`, then by the App into `/config/custom_components/ha_nova`). Tests: `tests/ha_integration/` (pytest against the pinned Home Assistant), CI job `ha-integration` (`ci-gate` needs it). `scripts/check-docs.sh` [5b] fails it on any logic beyond discovery, the proxy, and the user hand-off.

- **Entry:** only from the `hassio` discovery flow, no user step. Data: `host`, `port`, `secret`, and the App `slug`; a malformed message (host not a hostname, port not 1–65535, secret not 64 hex) creates nothing. Unique ID: the message's uuid, because Home Assistant's discovery removal deletes only the `hassio` entry with that uuid (`components/hassio/discovery.py`). A changed message (same uuid) updates the entry and reloads it. A new message from the same App slug (Home Assistant missed the old one's removal) replaces the entry's uuid and data. A message from another App slug is refused while an entry exists. Deleting only the entry is undone by Home Assistant's re-discovery; the App deletes the message instead (B2).
- **View** `/api/ha_nova/mcp`, `requires_auth = True`: Home Assistant's own check (access token, OAuth through Home Assistant Link). Unauthenticated requests get Home Assistant's `401` with `resource_metadata` and never reach the bridge. A signed path (`authSig`, a shareable link) gets `401`; a user Home Assistant made for itself (Supervisor, content user) gets `403`. Registered once; while no entry is loaded it answers `404`.
- **Forwarded:** `POST`, `GET`, and `DELETE` to `http://<host>:<port>/mcp` (the 0.27 bridge answers `405` with `Allow: POST` for the last two); other methods get Home Assistant's `405`. The query string is never forwarded. The body streams both ways, never read, parsed, or logged.
- **Request headers:** only `Content-Type`, `Accept`, `Last-Event-ID`, and `Mcp-*` pass; everything else, including the client's `Authorization`, `Cookie`, and any `X-HA-User-Id`, is dropped. Then exactly one `Authorization: Bearer <secret>` and one `X-HA-User-Id: <the authenticated Home Assistant user's id>`.
- **Response:** the bridge's status, `Content-Type`, `Allow`, and `Mcp-*` headers; nothing else (no `Set-Cookie`). The integration's own client session keeps no cookies. Exceptions, each with a JSON `{"message"}`: the bridge's `401` (wrong secret) becomes `502` "chat-app connection is out of date" (a `401` would send the chat app back to Home Assistant's sign-in); bridge unreachable (incl. 10 s connect timeout) `502`; no answer for 150 s (`sock_read`, above `--confirm-wait`'s 2 min maximum) `504`. A stream cut after the status was sent aborts the connection, so the client sees a broken transfer, never a short answer that looks complete.
- **Never logged:** the secret and bodies (debug lines name only the error class).
- **Version-stable:** the App writes the integration on turn-on only and never refreshes it on an App update, because new integration code needs a Home Assistant restart. A later change to it needs an App update that re-copies it and asks for that restart (not built in 0.27); the bridge must keep working with this version.

### Known limits (B3)

- **R9 (open):** Home Assistant's protected-resource metadata names only the origin. If the maintainer's claude.ai test (spec §13 B0) shows that claude.ai needs the full path, the fallback (an own `401` with an own metadata document whose `resource` is the full `/api/ha_nova/mcp` URL) means a view with `requires_auth = False` that checks Home Assistant's authentication itself: a separate reviewed PR before the release, not built here.
- A timeout or a refused secret answers with plain HTTP errors, not JSON-RPC errors (the integration never reads the request id).
- Any App whose `config.yaml` lists `discovery: [ha_nova]` can announce the bridge while no entry exists, including after turn-off until Home Assistant's next restart (the code stays loaded); App code is trusted, as for every Supervisor discovery.
- Turning chat apps off while Home Assistant is down loses Supervisor's removal push: the entry stays (and fails to load once the folder is gone) until the owner deletes it, or the next turn-on replaces it.
- A changed message reloads the entry; during that moment the view answers `404` (harmless: the bridge is stateless).
- A tool call that waits behind other bridge work for more than 150 s gets `504` while the bridge may still finish it.
- Discovery against a real Supervisor (HAOS VM) is not yet run. The pytest drives Home Assistant's own `hassio` discovery code (add, re-discover after an entry delete, remove by uuid) with Supervisor mocked; the live test bed check replayed the same calls. The real run is a required release lane (spec §12).

