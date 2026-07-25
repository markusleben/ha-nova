# NOVA Relay

The NOVA Relay connects your AI coding client to Home Assistant.
It provides secure, authenticated access to Home Assistant's APIs and extends them where needed.

## How It Works

```
Local: AI Client → NOVA Relay (this App) → Home Assistant APIs
Cloud: AI Client → Home Assistant Cloud → Supervisor Ingress → NOVA Relay → Home Assistant APIs
```

Your AI client (Claude Code, Claude Desktop, Codex, OpenCode, Google Antigravity, Hermes Agent) connects
to the relay with its own secure device credential — created by pairing, never
copied or pasted by hand. Intelligence lives in the AI client's skills, not in
the relay.

The optional Home Assistant Cloud path is a release-gated Beta on current
`main`. Release metadata keeps it disabled until the real validation matrix
passes. It uses the user's existing Nabu Casa remote service; HA NOVA runs no
public tunnel or broker. Local-only operation is unchanged.

## The NOVA page

Open **NOVA** in the Home Assistant sidebar to manage everything in one place.
(The **Open Web UI** button on this App's page opens the exact same screen —
it is the same owner console either way.)

From there you can:

- see whether the relay is connected to Home Assistant,
- check for App updates,
- **connect a device**: click *Connect a device* to show a one-time six-digit
  code, then enter it on your computer when `ha-nova setup` asks. The code works
  once and expires in 10 minutes,
- see connected devices and **revoke** any one of them instantly.

The page is owner-only and only reachable through Home Assistant's ingress — a
direct network request cannot open it.

## Configuration

| Option | Description |
|--------|-------------|
| **Relay Auth Token (legacy/advanced)** | Existing installs may keep their shared token. New installs leave this empty; the App creates and persists a private token automatically. |
| **File access (advanced)** | Optional access to supported Home Assistant configuration files. Defaults to off. |

The App receives Home Assistant API access automatically from Supervisor. Do
not create or paste a Home Assistant Long-Lived Access Token for a normal App
install. Standalone Container/Core deployments keep their explicit `HA_LLAT`
server-side as documented in `docs/reference/relay-container.md`.

> **Where is this page?** Home Assistant 2026.2 renamed Add-ons to Apps: this
> page moved from `/hassio/addon/<slug>/info` to `/config/app/<slug>/info`
> (Settings > Apps > NOVA Relay). The setup wizard links through HA's own
> `/_my_redirect/` endpoint, so its links work on both old and new versions.

### Network

| Port | Description |
|------|-------------|
| 8791/tcp | Pairing + legacy HTTP access |
| 8792/tcp | Secure device API (pinned TLS) — all paired-device traffic |

## Endpoints

Paired devices authenticate with their own credential over the secure TLS port
(8792), sent as `Authorization: Bearer <token>`. Requests on the plain port
(8791), including `GET /health` and legacy access, use the same header. The
pairing endpoints use the one-time code as their credential instead. You
normally never call these directly — the CLI and the NOVA page do it for you.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Relay health check (version, uptime, WS status) |
| GET/POST | `/pair/v1/*` | Secure device pairing (OPAQUE) — code as credential |
| POST | `/auth/device/*` | Device credential activate / revoke (TLS only) |
| POST | `/ws` | WebSocket proxy — forwards commands to HA's WS API |
| POST | `/core` | REST API proxy — forwards requests to HA's Core REST API (`/api/...`) |
| POST | `/files` | Opt-in, contained configuration-file operations |
| POST | `/backups` | Generic config-snapshot storage |

The Cloud Beta adds a separate Supervisor-ingress-only machine surface:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/cloud/v1/info` | Relay identity and Cloud capability discovery |
| POST | `/cloud/v1/device/bind` | Bind an existing local device to the authenticated Home Assistant user |
| POST | `/cloud/v1/device/revoke-self` | Revoke the exact presented, user-bound device; remains available for cleanup when Cloud is disabled |
| GET/POST | `/pair/v2/*` | OPAQUE remote-first pairing through Ingress |
| POST | `/cloud/v1/device/activate` | Atomically activate and user-bind a pending remote device |
| GET | `/health` | The same bounded health route through Ingress |
| POST | `/ws`, `/core`, `/files`, `/backups` | The same bounded functional routes through Ingress |

These routes are not exposed as a new public Relay port. Every machine request
must come from the exact Supervisor Ingress peer with exactly one Home Assistant
user identity. Functional calls also require an active device credential bound
to that user and this persistent Relay instance. Legacy shared Relay tokens are
not accepted through Ingress.

When Cloud Remote is disabled, `/cloud/v1/info` advertises cleanup-only
capabilities and `/cloud/v1/device/revoke-self` remains registered. Setup,
pairing, and functional machine routes are absent.

## Setup

The easiest way to set up everything (relay + AI client + skills):

- open [README.md](../README.md)
- open the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest)
- copy the stable install one-liner for your OS and run it unchanged

Or if you already have the repo:

```bash
ha-nova setup
```

When a validated build enables Cloud Remote, a supported interactive desktop
offers:

- **Local only (recommended default)** — unchanged local setup; no Home
  Assistant Cloud subscription or background Cloud probe required.
- **Local + Home Assistant Cloud** — pair locally once, then explicitly add
  secure remote access with the same device credential.
- **Home Assistant Cloud only** — authorize remotely, then pair through
  Supervisor Ingress with a six-digit code from the owner-only NOVA page.

A standard Home Assistant user is recommended for everyday Cloud calls; owner
or admin rights are not required for the machine routes. An owner is still
required to open the NOVA console and generate or revoke pairing codes.

The setup wizard handles Relay configuration and skill installation. Local
setup automatically uses one reachable Home Assistant instance, shows a
source-labeled pick list when it finds several, and keeps manual address entry
when discovery finds none. Its normal App flow asks only for the six-digit code
from the NOVA page, then pairs securely: the device receives its own credential
(kept in the client's OS credential store — or in a private file for
`--service` installs, explicit `ha-nova pair --credential-store=file` opt-ins,
and headless systems without a keyring) and talks to the Relay over pinned TLS.
Nothing to copy or paste. Existing saved tokens, `--relay-token`, service token
files, and standalone Container/Core setups keep their explicit-token paths.

Cloud OAuth refresh tokens are separate from Relay/device credentials and live
only in native OS secure storage. There is no file, environment-variable,
argument, or config fallback for them. Setup, reconnect, and explicit unlock
may show bounded native prompts for the selected device and OAuth slots. Normal
Relay calls use no-UI reads and fail fast if the store is locked.

Cloud management commands:

```bash
ha-nova cloud add [--server <name>] [--url https://<cloud-host>]
ha-nova cloud status [--server <name>] [--json]
ha-nova cloud unlock [--server <name>]
ha-nova cloud reconnect [--server <name>] [--url https://<cloud-host>]
ha-nova cloud remove [--server <name>]
ha-nova server route <local|automatic|cloud> [--server <name>]
ha-nova relay health --via <local|cloud> [--server <name>]
```

`automatic` routing prefers the pinned local device transport. It selects Cloud
only when a short authenticated local health preflight ends in a pure network
failure. Authentication, authorization, certificate-pin, identity, redirect,
or protocol failures stop without Cloud fallback. A functional request is sent
through one selected transport and is never replayed through the other.

Only a verified canonical `https://*.ui.nabu.casa` origin receives credentials.
A custom domain may be used for discovery only after its DNS CNAME resolution
ends at that canonical origin and one publicly trusted TLS certificate covers
both names.

## Checking Status

Open **NOVA** in the Home Assistant sidebar (see [The NOVA page](#the-nova-page))
for connection status, updates, and device management.

Run the built-in health check:

```bash
ha-nova doctor
```

This verifies: config file, local Relay token, relay reachability, WebSocket connection,
and relay version.

For a configured Cloud profile:

```bash
ha-nova cloud status --server <name>
ha-nova relay health --server <name> --via cloud
```

`cloud status` verifies OAuth, Home Assistant Cloud state, the current user,
NOVA Relay Ingress discovery, Relay instance identity, and the bound device
credential. With `--json`, it always emits one object; failures include typed
`verification_error` and `next_command` fields. It does not print tokens,
cookies, or Ingress paths. A named Cloud-only profile can run
`ha-nova setup --server <name>` to resume onboarding and install client skills.

You can also check the relay directly:

```bash
curl \
  -H "Authorization: Bearer <relay-auth-token>" \
  http://<your-ha-ip>:8791/health
```

A healthy response looks like:

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "ha_ws_connected": true,
    "version": "0.2.6",
    "uptime_s": 3621
  }
}
```

## Troubleshooting

**Relay not reachable**

- Verify the App is running (green icon in the header)
- Check that port 8791 is not blocked by your network/firewall
- Ensure the correct host IP is configured in your AI client

**Authentication failed**

- Existing installs should verify that the relay auth token matches on both sides
- Re-run `ha-nova setup` to repair the current client configuration
- App install: update or restart NOVA Relay so Supervisor access is refreshed
- Standalone Container/Core: verify that server-side `HA_LLAT` is still valid

**WebSocket not connected**

- Health and REST calls do not open the lazy HA WebSocket connection
- Run `ha-nova doctor` to send a WS ping and verify the following health state
- Check the App logs only when that active readiness check fails

**Cloud secure storage is locked**

- Run `ha-nova cloud unlock --server <name>` from the same interactive desktop
  session
- Do not run Cloud commands through `sudo`, SSH, WSL, a service, or a container
- Linux requires a validated desktop Secret Service session

**Cloud remote access is unavailable**

- Confirm the Home Assistant Cloud subscription and Remote UI are active
- Confirm the NOVA Relay App is installed, current, and running
- Run `ha-nova cloud status --server <name>` for a typed remediation code
- Treat any Relay-instance or user mismatch as a security stop; do not bypass it
- If reconnect signs in as a different Home Assistant user, HA NOVA restores the
  previous Cloud authorization. Pair this computer for the intended user before
  reconnecting again.
- After an intentional App reinstall, a Cloud-only profile must run
  `cloud remove --yes` and then `cloud add --url ...`. A hybrid profile must
  remove Cloud access, pair locally with the reinstalled App, and then add
  Cloud access again. Never use this sequence for an unexplained mismatch.

## Removing Cloud Access

`ha-nova cloud remove` revokes and verifies HA NOVA's OAuth authorization, then
removes its local Cloud secret and metadata. It leaves the Home Assistant Cloud
subscription and local NOVA pairing unchanged.

A standard `ha-nova uninstall` keeps Home Assistant connection config, device
pairing, and Cloud authorization for reinstall. `ha-nova uninstall --purge`
revokes and verifies every configured Cloud authorization before removing
local secrets and config. If safe revocation cannot be completed, purge stops
before destructive local cleanup.

## Cloud Beta Availability

Release metadata currently keeps Cloud Remote disabled. The implementation is
desktop-only on macOS desktop terminals, Windows console/RDP sessions after
real-device validation, and validated Linux desktop Secret Service providers.
SSH, WSL, containers, services, gateways, and other headless contexts stay
local-only. Release remains blocked until real Home Assistant Cloud parity,
native-keyring, user-role, redirect, lifecycle, App restart/update/reinstall,
and 10,000-command Ingress-session stress gates pass. See
[`docs/work/2026-07-25-home-assistant-cloud-remote-spec.md`](../docs/work/2026-07-25-home-assistant-cloud-remote-spec.md).

## Logs

App logs are available in the **Log** tab above. Look for:

- `Relay listening (app mode)` — relay started successfully
- `Relay bootstrap (app mode)` — shows the upstream auth source
- Pairing codes are **never** logged — generate and read them on the NOVA page
- Any `error` or `warn` messages indicate issues

## Support

- [GitHub Issues](https://github.com/markusleben/ha-nova/issues)
- [Setup Guide](https://github.com/markusleben/ha-nova#-get-started)
- [Full Documentation](https://github.com/markusleben/ha-nova)
