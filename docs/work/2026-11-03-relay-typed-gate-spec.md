# Relay gate for typed-tier actions

Status: active

Date: 2026-11-03. Owner: Relay (`nova/src/http/`, `nova/src/runtime/`, `nova/src/security/`), the bundled CLI (`cli/`), the NOVA page. Builds on `docs/work/2026-11-03-relay-refuses-code-writes-spec.md` (branch `fix/relay-refuses-code-writes-cli`: `relayTyped`, `rawItemTier`, `personFollows`), which must merge first. High-risk: security, new cross-component contract, release-bound.

## Today

Typed-tier actions (unlock, open a door, gate, or garage, disarm, deletes, and every other action a plan rates `typed`) ask a person only inside the CLI: the window, the linked phone, or a one-time code (`docs/reference/safety.md`, row "Unlocking or opening a lock …"). The Relay forwards whatever a valid credential sends and returns Home Assistant's answer unchanged. It refuses only `supervisor/`/`hassio/` WS types, subscriptions outside `collect_events`, and `/api/hassio` paths (`nova/src/http/handlers/ws-proxy.ts:60-100`, `core-proxy.ts`). The CLI's raw guard (`cli/relay_raw_guard.go`, `cli/relay_raw_allow.go`), its response redaction (`cli/relay_raw_redact.go`: phone webhook ids, lock codes, the backup password, camera tokens), and `relayTyped` all run in the caller's own process. On Linux (Secret Service) and Windows (Credential Manager) any process of the same user can read the device credential and call the Relay directly, so none of this stops it (audit finding A3; `docs/work/2026-10-17-confirmation-hardening-spec.md` §4 "T3").

Maintainer decision (2026-11-03, option A): the Relay enforces it. It holds a typed-tier action until a person approves it on their linked phone or in the confirmation window. This breaks "Relay stays dumb" on purpose, for this one gate. The gate is only worth its cost if the Relay also stops handing out what would let a principal skip it, so it covers the Relay's answers too (§2).

## Threat model

Invariant: no typed-tier effect reaches Home Assistant from a device credential without a person's approval that the Relay verified, and no Relay answer gives a device credential what it needs to act without the Relay.

Protects against a principal that holds a paired device credential for a **local** pairing (LAN pinned TLS) or the legacy shared token, but neither an approving person's Home Assistant session nor their unlocked phone:

- a coding agent or script with a shell that reads the credential from the keyring, a `--credential-store=file` copy, or a synced dotfile, and calls the Relay with `curl`;
- a model that reorders flags, edits `config.json`, or otherwise steers `ha-nova` past its own confirmation;
- that credential copied to another machine on the LAN.

Does not protect against (stated in `safety.md`):

- **Computers paired through Home Assistant Link.** They keep the Home Assistant user's own OAuth refresh token in the same OS store (`cli/cloud_oauth_secret_store.go`). Whoever reads it acts in Home Assistant as that user without the Relay: calls services, opens an ingress session, fires a phone-action event as that user. The gate still checks what such a computer sends through the Relay, but it is not a boundary there (decision 2).
- **A Home Assistant administrator session or token**, and code that already runs in one (installed dashboard card code runs in the administrator's browser). A process that reads the browser profile or the Companion app's storage acts as that person in Home Assistant itself. Approval in the page is exactly as strong as that session.
- **Whoever holds the linked phone** during an open ask (no device unlock needed for a notification button, as today).
- **Writes below typed.** Previewed and ordinary writes pass without a person, as today.
- **Indirect effects the tier engine does not see** (hardening spec §4: zone or calendar triggers, flows of entries the CLI did not start, a stored scene used later). The gate is exactly as wide as the CLI's tiers.
- **The in-App chat bridge** (`ha-nova serve`): it runs inside the App with its own confirmation rules (§5).
- **Camera and image access tokens computed by a template.** `/api/template` renders arbitrary output, so a template can read a camera's `access_token` attribute and reshape it past any redactor. The worker refuses a template answer that contains a live token value (§2), but a transformed one passes. Such a token opens that camera's stream or image for minutes; it never lets a device act.
- **A phone webhook id that leaks outside the Relay** (Home Assistant's own files, the phone). Through the Relay it is hidden (§2).
- **Denial of service.** A credential holder can still send asks (capped, §3), previewed writes, and revoke-worthy noise.
- **Check-to-send race.** The verdict is computed right before the Relay forwards; a concurrent change by another principal in that window is not re-checked.

## Design

### 1. Where the gate sits

One gate in front of the device-facing handlers, on every listener that serves them (device TLS, bootstrap, and the Home Assistant Link functional routes on the ingress listener) and for every principal kind (`device`, `legacy`): every `POST /ws` message (the `message` of a `collect_events` envelope and, when the envelope also has a `type`, the body itself, as `relayTyped` does), every `POST /core` request whatever its method (`GET`, `POST`, `DELETE`), every `POST /files`, `POST /backups`, and `POST /supervisor` request, reads included. Every answer to these principals passes the gate too (§2, redaction).

Verdicts: `pass` (forward unchanged), `typed` (forward only with a matching grant, §4), `refuse` (never forward). Anything else, a timeout, or an unreachable worker is `refuse` with `503 GATE_UNAVAILABLE`; nothing is sent, and no answer is returned unredacted.

Exempt: the chat bridge's internal principal (reserved install id `nova-app-chat-bridge`), only when the request's socket peer is `127.0.0.1` or `::1` on the device listener. That listener binds `0.0.0.0` (`nova/src/runtime/app-mode.ts`) and `Principal` carries no peer today (`nova/src/security/principal.ts`), so layer 3 adds the peer to the principal. The same credential from any other peer is refused. Pairing v1, pairing v2, and Link activation refuse both reserved install ids (`nova-app-chat-bridge`, `nova-app-gate`). A standalone container on host networking makes loopback mean the whole host; `nova/DOCS.md` says so and setup's container path warns.

### 2. Classification and redaction without business logic in the Relay

The App image already ships the CLI (`/opt/ha-nova`, staged for the chat bridge). The Relay runs it as a second child, `ha-nova gate --listen 127.0.0.1:<port> --secret-file /data/gate/secret --relay-file /data/gate/relay.json`, started and restarted like the bridge (`nova/src/runtime/chat-apps.ts`), with the same minimal environment (never the Supervisor or Home Assistant token). Loopback, `Authorization: Bearer <gate secret>`, constant-time compare:

- `POST /check` (5 s) `{principal, endpoint, body}` → `{verdict, kind, why, say}` (`kind`: `read` or `write`). `body` is exactly the bytes the Relay will forward (after its own parse and serialize), so what is classified is what is sent. The worker refuses a body with duplicate keys, lone surrogates, or integers a double can't hold.
- `POST /redact` (5 s) `{endpoint, request, response}` → the response with `redactSecrets` applied (`cli/relay_raw_redact.go`, nonces on), or a refusal. A response the worker can't parse or redact is not returned: `503`.

The worker decides with the CLI's own functions, never a second list: `rawItemTier` (raw config writes, files, backups, Supervisor; `cli/plan_raw_tier.go`), `checkRawRelayRequest` (the deny-by-default guard and its read rules, `cli/relay_raw_guard.go`), the call tier engine (`cli/tier_engine.go`, `cli/tier_rules.go`) for `call_service` and `POST /api/services/<domain>/<service>`, and the config-plan scan that makes an automation or script write typed when it reaches a typed action (`cli/plan_config.go`, `reasonConfirmWatch`). Layer 1 exposes these as one function, `gateVerdict(endpoint, request, reader)`, that the CLI's plans, verbs, and `relayTyped` also call. Any tier change the gate needs lands in that shared function, so the plan and the gate always agree. It keeps `relayTyped`'s pattern: tier without reads first; read Home Assistant only where the tier depends on live state; a failed read is `typed`.

Refused for device and legacy principals (fail closed on unknown):

- a WS type or core request shape the CLI never sends (the parity test enumerates every shape its verbs, plans, setup, doctor, and session reads send, not only raw ones);
- event firing (`fire_event`, `POST /api/events/*`), `execute_script`, `POST /api/states/*`, every `/api/webhook/*` request whatever its method (an automation's webhook trigger may accept `GET`);
- `GET` of a flow or repair step (`/api/config/config_entries/…/flow/<id>`, `/api/repairs/issues/fix/<id>`: a GET re-runs the step);
- secret exports: `/api/backup/download/*`, log reads (`GET /api/error_log`, `system_log/list`, `/supervisor` Core, Supervisor, and App logs), as `/files` already refuses `*.log`;
- subscriptions the CLI's guard refuses: `call_service` events, all events, `mobile_app_notification_action` and the other phone event types, templated `subscribe_trigger` (a device must not read live action ids and copy them).

Typed under the gate, by changing the shared tier (so the plan asks too):

- `files` `write_file` and `delete_file`: a YAML write plus a reload acts unasked. The ask shows the path and the typed reasons the config scan finds in it.
- any change to backup locations (`backup/config/update` touching `create_backup.agent_ids` or `agents`), and a config flow for an integration that provides a backup agent: otherwise a new remote location plus `backup/generate_with_automatic_settings` carries a backup, with Home Assistant's refresh tokens and phone webhook ids, out of the house. The download refusal alone is not the export guard.

Redaction: every answer to a device or legacy principal goes through `/redact`, including `collect_events` results, `/supervisor` JSON (on top of the Relay's own App-option hiding), and `/core` responses. This moves the phone webhook id (registry `unique_id` prefix, `data.webhook_id` in traces), lock and alarm codes, the backup password, the Thread dataset, MQTT alarm codes, flow secrets, and camera tokens out of a device credential's reach; the CLI keeps redacting too. Binary answers (camera frames, `/files` `read_file` of a non-text file) pass unchanged, since `redactSecrets` reads only JSON and text.

Key-based redaction is a boundary only for structured answers, whose shape the request can't change: registry lists, traces, settings, flow forms, Supervisor JSON. Every secret that lets a device act (phone webhook ids, lock and alarm codes, the backup password, flow secrets) lives only there; none is reachable from a template. Template output (`POST /api/template`) is computed, so no redactor bounds it: the worker reads the live camera and image tokens and refuses a template answer that contains one, and a reshaped token stays a stated limit (Threat model).

The Relay keeps only transport: the gate middleware, the worker client, the ask and grant store, and the page. `scripts/check-docs.sh` checks [3]–[5] get one named exemption for these files and keep failing on domain handlers anywhere else.

The worker reads Home Assistant through its own internal principal (reserved install id `nova-app-gate`, minted like the bridge's, loopback peer only). The Relay forwards its request only when (a) the worker's own check says `kind: read`, (b) it is a `call_service` with domain `notify` and a service starting `mobile_app_` (the ask itself), or (c) it is a `collect_events` subscription to `mobile_app_notification_action` (the answer). (b) and (c) are allowed for this principal only. A check of a read never reads Home Assistant, so the worker's reads don't loop. Its answers are not redacted: it needs the action ids it sent.

### 3. Asks and approval channels

A typed request never starts an ask by itself. A plain request the worker rates typed, without a matching grant, is refused with `428 GATE_APPROVAL_REQUIRED` ("Only a person may confirm this, on their phone or in Home Assistant. Update HA NOVA on this computer: ha-nova update"). Only an explicit ask starts one:

`POST /gate/v1/ask` `{requests: [{endpoint, body_b64}] (1–20, in order), lang}`: the worker checks each; none typed → `200 {needed: false}`; any refused → `403`; else `202 {ask_id, expires_at, channels}`. `GET /gate/v1/ask/<ask_id>` long-polls (≤ 25 s per call) → `waiting` | `approved {grant_id, until}` | `cancelled` | `expired`. `DELETE /gate/v1/ask/<ask_id>` cancels. Ids are 256-bit random, compared in constant time, bound to the principal that asked.

What the person sees is the worker's `say`: built from `cli/confirm_strings.*.json`, names only through `sayText`, plus the asking device's name from the Relay's registry ("from Markus' MacBook"). Each typed item names its entity id, domain, and class wording next to the friendly name ("Open Living room blinds (cover.garage_door, garage door)"), because a rename is previewed and a device could rename the garage first. The ask lists every item; Approve means exactly these request bytes.

Channels the Relay accepts as approval:

1. **Linked phone.** The Relay keeps each device's linked phone in its own registry (`confirm_phone: {notify_service, device_id, user_id}`), not only in the computer's `phone-links.json`. The worker sends the notification with fresh random action ids and judges the answer with the CLI's `judgePhoneEvent`: only a `mobile_app_notification_action` event carrying one of those ids, whose `context.user_id` is the linked user, an active, non-system administrator who is not the user the Relay acts as. The worker returns `{answer, user_id, context_id}` on the loopback channel and the Relay records it; no endpoint a device can reach accepts an approval. Proof of origin is Home Assistant's own: the event comes from that phone's authenticated Companion-app webhook, whose id a device can no longer read through the Relay (§2); an event the Relay's user fires never counts. When the Relay acts as the owner (`OWNER_TOKEN`), a tap can't be told apart from the Relay, so the phone channel is off.
2. **Confirmation window = the approval page.** The NOVA page's approval view, reached only through Supervisor ingress (`panel_admin: true`; identity from `x-remote-user-id` only from the Supervisor peer, `nova/src/security/ingress-identity.ts`). It answers with `frame-ancestors 'none'` (the rest of the NOVA page keeps `'self'`), so no dashboard card can frame it, and it opens top-level from the panel. Approve needs a fresh read that the user is an active, non-system administrator and not the Relay's user, and a single-use CSRF token bound to `approve:<ask_id>`. On a gated Relay the CLI's native window carries no Confirm button: on Windows and X11 any same-user process can click it, so the Relay can't trust it. It shows the ask text, "Confirm on your phone or in Home Assistant", [Open in Home Assistant], and [Cancel]. The button's address comes only from the paired server profile, never from Home Assistant's `internal_url` or `external_url` (a device can change those at preview tier and point the button at a lookalike sign-in). The standalone container has no Supervisor ingress, so no page: it approves on the phone only.

Cancel from any channel, the CLI and the native window included, ends the ask; a cancel never grants anything. No approval falls back to a weaker channel on an error: phone unavailable → page only; neither → `409 GATE_NO_CHANNEL`, nothing sent. The one-time code is not a gate channel: the CLI derives it from the device credential, so the gate can't tell who typed it.

Limits: one open ask per principal, expiring after 3 minutes, and at most 10 asks per principal per hour. Three asks in a row that end cancelled or unanswered pause phone asks for that device for 15 minutes (`429 GATE_PAUSED` on the phone channel; the page still works, so a process that floods asks can't lock the real person out). The NOVA page lists paused devices with [Revoke this device]. A phone link is set or changed only by `POST /gate/v1/phone` followed by a tap on the new phone; changing an existing link also needs the phone linked now, or the page.

### 4. Grants

An approved ask becomes a grant: the ordered SHA-256 digests of `endpoint ‖ 0x00 ‖ body bytes as received` for each item, the principal, and an expiry 2 minutes after approval (Relay memory only; an App restart drops it and nothing runs). The CLI then sends each request as before, byte-identical, with `X-HA-Nova-Grant: <grant_id>`. The Relay forwards a typed request only when the grant is the caller's, unexpired, and holds that digest unused; it marks the digest used before forwarding (at most once, never retried). A grant never covers another principal, another body, or a second send; requests the worker rates `pass` ignore the header. This keeps `ha-nova apply`'s order (re-check the live config, send, verify): a plan whose re-check changes a body no longer matches and is refused.

### 5. Chat apps and the Home Assistant MCP endpoint

Both run through `ha-nova serve` inside the App (`/api/ha_nova/mcp` → integration → bridge → Relay over loopback). The bridge stays exempt (§1): the model in a chat app never holds a credential, the bridge runs the same tier code, and it keeps its own asks (linked phone, chat code where the owner allows it), its own redaction, and its levels. `cli/mcp_serve_levels.go` is unchanged: below Manage everything the allowlist still fails closed on the user id the integration vouched for. The exemption is why it is bound to the loopback peer and why pairing refuses the reserved ids (§1).

### 6. Compatibility and rollback

- **Older CLI, newer App:** it sends typed writes plainly and gets `428`; nothing is sent; the message says to update. Everything below typed works; redaction changes nothing for it, since it already hides the same values.
- **Newer CLI, newer App:** the Relay's ask replaces the CLI's own; a typed action is asked about once, never twice.
- **Newer CLI, older App:** `GET /health` carries `gate: 1` and `/gate/v1/ask` exists only on a gated App. Without both, the CLI asks on its own as today (window, phone, code) and sends. Once a server profile has seen `gate: 1`, the CLI stores `gate_seen` and refuses typed actions there when the gate is gone: "HA NOVA's App was rolled back to a version without its safety check; update it in Home Assistant". `gate_seen` lives in user-writable config, so it guards against an accidental rollback, not an attacker (who can't downgrade the App: the Supervisor allowlist refuses writes to HA NOVA's own App).
- **App rolled back after a grant:** the person already approved those bytes; an old App forwards them.
- **`min_relay_version`:** stays a nudge (`cli/version.go`) and is not raised in the gate release, so older Apps keep today's client-side asks. Raising it and dropping the client-side path is a later release (decision 5).
- **Worker missing or crashed:** every gated request is refused (`503 GATE_UNAVAILABLE`). The standalone image (`nova/Dockerfile.standalone`) must ship the CLI too, or it refuses everything (layer 7).
- **New request shapes:** a request the App's worker doesn't know is refused until the App updates; a CLI that adds one needs an App release and a `min_relay_version` raise.

### 7. Migration and UX

- With a linked phone, nothing to do after the App update: the first typed action asks on the same phone, now sent by the Relay. If the Relay doesn't know the link yet, the phone first asks once "Use this phone to confirm HA NOVA changes from <computer>?"; the tap links it.
- No phone: the window opens the approval page; signed in to Home Assistant, it is one click. The page sits behind the HA NOVA sidebar entry, so the Companion app on any phone reaches it too (headless Linux, SSH).
- Typed four-digit codes end for computers on a gated App; the release notes say where to confirm instead.
- The NOVA page gets "Waiting for your OK" (open asks, from which device, Approve and Cancel) and paused devices with [Revoke this device].
- `ha-nova doctor` and setup's summary say "Safety check in Home Assistant: on", or why not (App too old, worker down, the Relay signs in as the owner so only the page can approve, a Link computer where the check is not a boundary).
- Texts follow the confirmation-localization spec; Relay-side copy (`428`, `503`, page chrome) is English like other Relay errors.

## Test plan (test bed only, never a live Home Assistant)

- **Go:** a parity oracle: every case in `cli/tier_golden_test.go`, `cli/tier_failopen*_test.go`, `cli/plan_raw_tier_test.go`, and `cli/relay_typed_test.go` gets the same tier from `gateVerdict` as from the plan; every request shape the CLI sends (verbs, plans, setup, doctor, session reads) is recognized; the §2 refusal list → `refuse` (GET webhook, GET flow step, backup download, logs, phone-event subscriptions, duplicate keys); backup-location changes and backup-agent flows → `typed`; a failed read → `typed`. `/redact` vectors from `cli/relay_raw_redact_test.go`, plus a template answer carrying a live camera token → refused. `judgePhoneEvent` vectors: the Relay's user, owner token, wrong id, non-admin, system user.
- **TypeScript:** the gate in front of every handler and listener (device TLS, bootstrap, Link routes on ingress), every method on `/core`; worker answers `pass`/`typed`/`refuse`, garbage, timeout, refused connection, crash mid-ask → nothing forwarded; `/redact` failure → `503` and no body; bridge credential exempt from `127.0.0.1` only, refused from a LAN peer; pairing refuses both reserved ids; gate principal limited to reads, `notify.mobile_app_*`, and phone-event collection; grants: wrong principal, wrong digest, reused digest, expired, after restart; ask cap, pause on the phone channel only, page still approves; page: non-Supervisor peer, non-admin, the Relay's user, stale and replayed CSRF, `frame-ancestors 'none'`; cancel from any channel, approve only from the two.
- **End to end (`npm run testbed:up`, `TESTBED_PHONE=1` for fake phones):** with `.state/relay-token` and `curl`: `lock.unlock` on a demo lock → `428`, lock unchanged; the same inside a `collect_events` envelope; `config/entity_registry/list` shows no phone webhook id; ask → fake phone tap (a test administrator's `mobile_app` registration posting the action through its own webhook) → grant → unlock once; replay → `428`; `fire_event` of `mobile_app_notification_action` through the Relay → refused; a tap from the Relay's user → ignored; worker killed during an ask → lock unchanged; an automation write that unlocks on a time trigger → `428`; a `files` YAML write with `readwrite` access → `428`; `GET /api/webhook/<id>` → refused.
- **Compatibility (test bed):** the last released CLI (`TESTBED_CLI_BIN`) against the new App: typed fails closed, ordinary and previewed work; the new CLI against the last released App image: client-side asks as today; the new CLI after a rollback with `gate_seen` → refused with the update line.
- **Chat apps:** with the bridge on the test bed, a Use-the-home user's typed call still asks on the bridge's phone and applies; the HA MCP endpoint the same.
- **Maintainer lanes (handed over, not run by agents):** real phones (iOS, Android, Watch), Home Assistant Link via the Cloud checklist, the ingress page on the HAOS VMs.

## PR split (each at most about 400 production lines, tests excluded)

1. **CLI `gateVerdict`:** one entry point over `rawItemTier`, the guard, the call tier engine, and the config-plan scan; `relayTyped`, plans, and verbs call it; the two tier changes from §2 (files, backup locations), which change CLI behavior too; parity oracle. After `fix/relay-refuses-code-writes-cli` merges.
2. **CLI `ha-nova gate` worker:** `/check`, `/redact`, `/ask` (reuses `askPhone`, `judgePhoneEvent`), loopback bearer, the refusal list in §2, entity ids in `say`.
3. **Relay gate middleware:** worker child and client, peer on the principal, internal gate principal and its three allowances, loopback-only bridge exemption, reserved ids refused at pairing, `refuse`/`428`/`503` and redaction on every gated handler and listener.
4. **Relay asks and grants:** `/gate/v1/ask` (POST, GET, DELETE), the grant header and digests, cap and pause, `confirm_phone` in the registry, `/gate/v1/phone`.
5. **NOVA page:** approval view (`frame-ancestors 'none'`), "Waiting for your OK", paused devices and revoke; `check-docs.sh` exemption; `/health` `gate: 1`.
6. **CLI client:** asks for typed plans on a gated Relay, the waiting window with [Open in Home Assistant] from the server profile, `gate_seen`, the one-time phone-link move, `428` handling, the doctor line.
7. **Images and docs:** CLI in the standalone image, App bump, `safety.md` rows (the gate and its limits, Link computers included, replace "A model that can read the Relay credential is not stopped by any of this"), `chat-apps.md`, `nova/DOCS.md` (host networking), release notes.

Layers 3–5 add roughly 1,000–1,200 Relay lines; each raises the Relay LOC ceiling in `scripts/check-docs.sh` check [1] (11850 today) with its reason, as earlier waves did.

Release order: 1 and 6 may ship first (6 probes and falls back; 1 makes files writes and backup-location changes ask on every App). 3–5 and 7 ship in one App release; between their merges `main` refuses typed writes outright (closed) and is not released.

## Decisions (maintainer, 2026-11-03)

Option A approved as specified: the CLI gate worker plus Relay redaction. All open questions took the recommended default:

1. Unknown request shapes are refused; writes are never held with a generic "unrecognized change" ask.
2. Computers paired through Home Assistant Link are documented as a limit (Threat model), not bound to an approving user.
3. Page approval: any active, non-system administrator except the Relay's user.
4. Four-digit codes on computers end with a gated App.
5. Older Apps keep today's client-side asks until a later release raises `min_relay_version`.
6. A standalone container whose Relay token is the owner's refuses typed actions until a dedicated Home Assistant user exists; no Relay sign-in page for now.
7. Chat apps keep the bridge exemption and chat codes.
8. `files` writes are typed everywhere.
9. 10 asks per hour per device; phone asks paused 15 minutes after three unanswered or cancelled asks; the page always stays open.
