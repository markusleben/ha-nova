# Sign in with Home Assistant: owner-armed ingress pairing — Spec (2026-10-16)

Status: active

## Goal

During `ha-nova setup` the user signs in with Home Assistant in the browser
that opens, and the computer is paired. Nobody reads, copies, or types a
six-digit code on the happy path. The NOVA page code stays as the fallback.

The existing security model stays: OPAQUE (RFC 9807) device pairing, one
revocable credential per computer, SPKI-pinned TLS 1.3 for every later call,
owner-only device management, a dumb relay.

## Non-goals

- No change to the typed six-digit code path, `ha-nova pair`, or the Home
  Assistant Cloud flows.
- No persisted Home Assistant token anywhere (CLI, relay, config, keyring).
- No App install automation (onboarding PR4) and no demo entry (see
  "Demo-first" below).
- No first-look logic in this change: `ha-nova map` (feat/home-map) owns it.

## Happy path, step by step

An extra computer on a home whose NOVA Relay App already runs (detection from
the installed-App spec skips the install step). What the user sees:

```
$ curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
  ...installs, then starts the wizard...

  Which AI client do you use?           > Claude Code (detected)      [Enter]
  How should this computer connect?     > 1) Local only (recommended)  [Enter]
  Found Home Assistant at homeassistant.local                          [Enter]
  ✓ NOVA Relay is already running in Home Assistant. Skipping the install step.

  ● ● ○ ○  Step 2 of 4 - Pair this device

  Sign in with Home Assistant to connect this computer. Your browser opens
  your Home Assistant login: sign in as the owner. Home Assistant then names
  "http://127.0.0.1:…/ha-nova" as the app asking for access: that is HA NOVA
  on this computer. This computer gets its own connection, and you can
  revoke it in NOVA at any time.

  Press Enter to sign in, or type 'code' to use a six-digit code from NOVA:  [Enter]
  Opening Home Assistant in your browser...

        (browser) "You're about to give http://127.0.0.1:53817/ha-nova access
        to your Home Assistant instance."  Username / Password  [Log in]
        (browser) "Done. You can close this tab and go back to the terminal."

  ⠋ Waiting for you to sign in (4:52)
  ✓ Signed in to Home Assistant as Markus (owner)
  ✓ This computer is connected to NOVA Relay
  ● ● ● ○  Step 3 of 4 - Verifying connection     ✓
  ● ● ● ●  Step 4 of 4 - Installing HA NOVA skills ✓
  [first look from `ha-nova map` — integration point, see below]
  ✓ HA NOVA is ready. Start a new Claude Code session and ask about your home.
```

### Action count

"Deliberate action" = a decision or something the user must do; pressing Enter
on a pre-selected default is listed separately.

| Journey | Before this change | With this change |
|---|---|---|
| Pairing step alone | Enter to open NOVA, find NOVA, click "Connect a device", read the code, type six digits, Enter (5-6 actions, 2 context switches, 1 transcription) | Enter, sign in in the browser (1 Enter + 1 action; the browser login is the approval) |
| Extra computer, App running, from "I have HA and an AI client" | 1 command + 3 default Enters + 5-6 pairing actions | 1 command + 4 default Enters + 1 sign-in |
| First computer | as above + App install clicks (repository, Install, Start) | unchanged install clicks until PR4 automates them through the same sign-in |

Target from the maintainer: ≤ 3 deliberate actions (run one command, sign in,
approve once) and no copy/paste of codes, URLs, or tokens. This change
delivers the sign-in and removes the only transcription. The remaining
default-Enters (client, connection mode, address) are follow-ups: auto-accept a
single detected client, keep "Local only" without a prompt when Home Assistant
Cloud is not detected, accept a single verified address. Each is a small wizard
change of its own; none is part of this security change.

## Flow

```
CLI                         Home Assistant (LAN origin)       Supervisor        NOVA Relay
 | bind 127.0.0.1:0, open browser /auth/authorize (client_id = redirect host)   |
 |<-- GET /oauth/callback?code&state (loopback, one shot)                        |
 |-- POST /auth/token (code) ---------->|                                         |
 |-- WS /api/websocket auth, auth/current_user (owner? else fallback)            |
 |-- WS supervisor/api GET /addons/<slug>/info (started, ingress root)           |
 |-- WS supervisor/api POST /ingress/session ---------->| session bound to user  |
 |-- POST /auth/revoke (refresh token) + verify invalid_grant; WS closed          |
 |   ...from here on only the in-memory ingress cookie...                         |
 |-- POST <ingress>/pair/v2/arm (JSON) ----------------------------->| owner check (config/auth/list)
 |<-- { pairing_secret (256-bit), expires_in_s, relay_instance_id } -----------|
 |-- POST <ingress>/pair/v2/start, /pair/v2/finish (OPAQUE with that secret) -->|
 |<-- sealed { credential, device_id, relay_instance_id, spki_pin, secure_port } |
 |== existing local tail: store pending -> activate over pinned TLS -> promote ==|
```

The HA access token authenticates the WebSocket only. It never reaches the
relay: ingress requests carry only the `ingress_session` cookie. Both tokens
live in process memory and the refresh token is revoked as soon as the
ingress session exists, before any pairing request.

## Relay contract

### `POST /pair/v2/arm` (ingress listener only)

Registered with the other pair-v2 ingress routes (present when the relay build
has Cloud remote enabled, as in current release metadata). The unauthenticated
`GET /pair/v1/info` capability probe adds `sign_in: true|false`, so the CLI
offers sign-in only when the relay serves it (older CLIs ignore the field).

- Requires the strict ingress gate: exact Supervisor peer, exactly one
  `X-Remote-User-Id`, exactly one `X-Ingress-Path`.
- Machine-only: `Content-Type: application/json` is required (415 otherwise);
  a request carrying `Sec-Fetch-Site` (every browser request) is refused (403).
- Fresh owner check against `config/auth/list` on every call (no cache): the
  user must be active, not system-generated, and `is_owner`. Not the owner:
  403 `OWNER_REQUIRED`. Ownership not verifiable: 503 `OWNER_CHECK_UNAVAILABLE`.
- Secure port not mapped: 503 `PAIRING_UNAVAILABLE` (the device could never
  reach the TLS listener).
- Success: `{ pairing_secret, expires_in_s: 120, relay_instance_id }` with
  `Cache-Control: no-store`. The secret is 32 random bytes (base64url).

### Armed pairing slot

The relay keeps one pairing slot (unchanged). Arming fills it like the NOVA
page's "Connect a device", with three differences:

1. The password is the 256-bit secret, not six digits; TTL two minutes.
2. The slot is bound to the arming Home Assistant user and accepted only by
   `/pair/v2/start` from that same ingress user. `/pair/v1/start` on the LAN
   port answers as if no code were active, before the rate limiter, so the
   secret is useless on the LAN and LAN noise cannot spend the pairing budget.
3. The NOVA page never shows an armed secret (status reads as no active code).

Arming replaces an open NOVA-page code; "Connect a device" replaces an armed
slot. The slot is in memory only, single use, and gone on App restart.

An armed `/pair/v2/finish` creates a normal local device: pending, without a
Cloud user binding, activatable only over pinned TLS via
`/auth/device/activate`. Its sealed response adds `spki_pin` and
`secure_port` to the existing fields. Typed-code v2 (Cloud remote-first)
keeps its exact response shape and its user-bound pending device.

Why unbound: the CLI reuses the existing local pending → activate → promote →
resume path unchanged, and a later `ha-nova cloud add` with a standard Home
Assistant user (recommended by the Cloud spec) can still bind the device; an
owner binding would make that bind a conflict.

### Decision: owner, not admin

Arming is the machine form of the NOVA page's "Connect a device", which is
server-side owner-only today (`nova/src/security/owner-check.ts`;
`panel_admin` is only a UI filter). A device credential reaches everything the
relay's Supervisor token reaches. Allowing admins would create a second,
broader pairing authority than the console. Admins and standard users get a
plain message and the typed code (which an owner provides).

## CLI contract

- Offered only in the interactive wizard pairing step when the relay supports
  secure pairing and advertises `sign_in: true`, stdin and stdout are
  terminals, the session is not SSH, and the saved Home Assistant URL is a
  plain origin (`http`/`https`, host, optional port, no path, query, fragment,
  or user info). Otherwise the step is exactly today's code prompt, so an older
  NOVA Relay App never sends the user to a browser for nothing. `ha-nova pair`
  and scripted input are unchanged.
- OAuth reuses `cli/cloud_oauth_loopback.go`: 127.0.0.1 random port, client ID
  `http://127.0.0.1:<port>/ha-nova`, callback on the same host and port, 256-bit
  state, one GET on the exact path, Host header pinned (no DNS rebinding),
  static no-store page. Wait bounded to five minutes with a visible countdown.
- Every Home Assistant request goes to exactly the saved origin; redirects are
  never followed; HTTPS uses the system trust store (no "trust anyway").
- No PKCE: the client and callback share one loopback origin, which Home
  Assistant accepts without PKCE on every supported version; Home Assistant
  2026.10 rejects a `code_verifier` whose authorize request carried no challenge.
  Upgrade path: S256 once the supported floor guarantees challenge pass-through.
- The App is identified exactly like the Cloud path (`NOVAAppInfo`: slug,
  started, ingress root, `/home` entry).
- Tokens never enter config, keyring, environment, arguments, logs, or error
  text (`CloudError` never carries bodies or tokens). Revocation failure prints
  one warning line pointing at Profile → Security in Home Assistant.
- The credential tail is the shared local path (`cli/pairing_flow.go`): pending
  credential stored first, pinned-TLS activation, promotion, and resume after
  interruption all behave as for a typed code.

## Failures: one plain line, then the code

Every row prints one line and continues in the same step with today's code
prompt (`promptToOpenNOVAForPairing`); address, client, and install progress
are kept.

| Failure | Line shown |
|---|---|
| Sign-in canceled or denied in the browser | Sign-in was canceled. |
| No sign-in within five minutes | No sign-in arrived within five minutes. |
| Signed-in user is not the owner (CLI check or relay 403) | Signed in as X, who is not the Home Assistant owner — only the owner can connect a computer. Ask the owner for a code from NOVA. |
| No Supervisor (Container/Core) or App not reported | Home Assistant did not report the NOVA Relay App, so sign-in cannot connect this computer. |
| Relay without `/pair/v2/arm` despite the capability flag | Your NOVA Relay App does not support sign-in yet. Update it in Home Assistant when convenient. |
| Home Assistant unreachable, TLS not trusted, unexpected answer | Could not finish the sign-in with Home Assistant (reason). |
| Pairing through Home Assistant rejected or interrupted | Connecting through Home Assistant did not finish. |

Followed by: "Use a six-digit code from NOVA instead." Security stops never
fall back: a TLS pin mismatch at activation stops exactly like the typed path,
and a credential-store failure stops before any secret is used.

## Threat model

| Threat | Control |
|---|---|
| CSRF on arm | The ingress cookie is `SameSite=Strict` (frontend `src/data/hassio/ingress.ts`); arm needs `application/json` (no simple form or text/plain request) and refuses any `Sec-Fetch-Site`; the response is unreadable cross-origin; owner check per request |
| Replay | The secret is single use and expires after two minutes; a captured arm request only yields a new secret to a holder of the owner's ingress session; finish replay returns the stored sealed response only for the same handshake, KE3, and ingress user (existing) |
| LAN attacker | The secret is never accepted on the LAN port; OPAQUE keeps the secret from passive observers; over plain-HTTP Home Assistant an active attacker who reads ingress traffic also reads the owner's password at login, so this adds no new exposure; over HTTPS the whole exchange is protected; the SPKI pin travels inside the OPAQUE-sealed response |
| Malicious local process on the loopback port | The port is bound before the browser opens; exact host, path, and method; 256-bit state; one shot. Not covered: a same-user process that reads the browser URL could exchange the code first; such a process can already read the browser's Home Assistant session (PKCE is the upgrade path) |
| Token leakage into logs or backups | Tokens stay in memory; the refresh token is revoked before pairing starts, which also ends the access token, so `.storage/auth` (and backups) keep no HA NOVA session; the relay never sees an HA token and never logs the secret, cookies, or headers; the registry stores only digests |
| Non-admin or non-owner Home Assistant users | Any user may create an ingress session (`WS_NO_ADMIN_ENDPOINTS`), so the relay checks ownership itself; the CLI checks first only for a friendlier message |
| Other Apps on the internal network | Ingress identity requires the exact Supervisor peer `172.30.32.2`; headers on direct ports are ignored (existing) |
| Home Assistant Container/Core | No `supervisor/api`: fallback to the existing token or code paths |
| Concurrent NOVA-page code | One slot: the newer action wins and the other attempt fails generically and falls back |

## Home Assistant source evidence

- Same-origin loopback client: `verify_redirect_uri` accepts a redirect with
  the client ID's scheme and netloc (`homeassistant/components/auth/indieauth.py`
  lines 41-48 at core 2026.10.1, rule present since #15911, 2018-08); 127.0.0.1
  is an allowed client host (`_parse_client_id`, lines 345-408, unchanged in
  substance by #186537). The login flow applies it (`login_flow.py` 312-317).
- `code_verifier` without a stored challenge is rejected
  (`auth/__init__.py` 343-369); `/auth/revoke` removes the refresh token
  (`auth/__init__.py` 234-260).
- `supervisor/api` allows `/ingress/session` and `/addons/<slug>/info` for
  non-admins and binds the session to the WebSocket user
  (`hassio/websocket_api.py` 51-59, 135-139).
- Supervisor 2026.10.2 sets `X-Remote-User-*` from the session and strips
  client-supplied copies (`supervisor/api/ingress.py` 370-397) and invalidates
  sessions of removed or inactive users (333-367).
- Frontend 20260930.3 shows "You're about to give {clientId} access" and adds
  `storeToken` only for its own client ID (`src/auth/ha-authorize.ts`,
  `src/auth/ha-auth-flow.ts`), so the strict callback stays valid.

## First look (integration point)

Right after a verified pairing the wizard should show the read-only first look
from `ha-nova map` (feat/home-map; house in one line, 1-2 observations, three
example prompts with real names, one safety line; < 5 s, read-only). This
change builds no first-look logic: it marks the call site in
`cli/setup_interactive.go`, after the skills step and before the completion
banner, where the first-look mode is invoked once it lands.

## Demo-first (design note, not in this change)

Pending maintainer decision. Proposal: Home Assistant found → connect the own
home directly, plus one hint line "Want to look around first? Try the demo:
ha-nova setup --demo"; no Home Assistant found → one question "Enter your Home
Assistant address, or try the demo first?" (Enter = address); after the demo →
"Connect your own home now?" in one step that lands in the sign-in above.

## Delivery

Two stacked PRs, each reviewable on its own:

1. Relay: `/pair/v2/arm`, armed slot, tests, reference docs, this spec.
2. CLI: LAN origin generalization of the Cloud OAuth/WebSocket/Ingress
   clients, the sign-in pairing step, tests, release-body line.

## Tests

- Relay (Vitest): owner/admin/standard user, missing or duplicate identity
  headers, wrong peer, content type, `Sec-Fetch-Site`, secure port missing,
  armed slot refused on `/pair/v1/start` without spending the rate budget,
  other user refused on `/pair/v2/start`, armed finish returns pin + port and
  creates an unbound local pending device activatable over TLS, typed-code v2
  response unchanged, NOVA page never shows the secret, TTL expiry, arm
  replaces a code and vice versa, idempotent finish replay.
- CLI (Go): LAN origin parsing, WebSocket scheme, loopback flow on a LAN origin,
  armed v2 decode (strict fields), orchestration with fakes for every failure
  row (fallback reached, refresh token revoked on every path), owner pre-check,
  offer rules (TTY, SSH, origin), wizard fallback keeps progress.
- E2E, throwaway only: the local test bed (Home Assistant Container) proves the
  real loopback authorize → token → WebSocket → revoke chain and the
  Container fallback; the Supervisor sandbox proves arm + pairing through real
  ingress with the CLI in a Linux container.

## Limits

- Needs Home Assistant OS/Supervised, a NOVA Relay App with this change, and a
  browser on the same computer (not over SSH).
- A Home Assistant behind HTTPS with a certificate the system does not trust
  falls back to the code.
- The relay registers arm only in builds with Cloud remote enabled (current
  release metadata); otherwise the CLI sees 404 and falls back.
- If the CLI is killed between token exchange and revocation, Home Assistant
  keeps one unused HA NOVA refresh token until it expires (90 days unused) or
  the user removes it under Profile → Security.
