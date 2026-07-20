# NOVA Relay

The NOVA Relay connects your AI coding client to Home Assistant.
It provides secure, authenticated access to Home Assistant's APIs and extends them where needed.

## How It Works

```
AI Client → NOVA Relay (this App) → Home Assistant APIs
```

Your AI client (Claude Code, Claude Desktop, Codex, OpenCode, Google Antigravity, Hermes Agent) connects
to the relay with its own secure device credential — created by pairing, never
copied or pasted by hand. Intelligence lives in the AI client's skills, not in
the relay.

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

## Setup

The easiest way to set up everything (relay + AI client + skills):

- open [README.md](../README.md)
- open the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest)
- copy the stable install one-liner for your OS and run it unchanged

Or if you already have the repo:

```bash
ha-nova setup
```

The setup wizard handles relay configuration and skill installation. It automatically uses one reachable Home Assistant instance, shows a source-labeled pick list when it finds several, and keeps manual address entry when discovery finds none. Its normal App flow asks only for the six-digit code from the NOVA page, then pairs securely: the device receives its own credential (kept in the client's OS credential store — or in a private file for `--service` installs, explicit `ha-nova pair --credential-store=file` opt-ins, and headless systems without a keyring) and talks to the relay over pinned TLS. Nothing to copy or paste. Existing saved tokens, `--relay-token`, service token files, and standalone Container/Core setups keep their explicit-token paths.

## Checking Status

Open **NOVA** in the Home Assistant sidebar (see [The NOVA page](#the-nova-page))
for connection status, updates, and device management.

Run the built-in health check:

```bash
ha-nova doctor
```

This verifies: config file, local Relay token, relay reachability, WebSocket connection,
and relay version.

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

- The relay connects to HA's WebSocket API on startup
- Check the App logs for connection errors
- Restart the App to force a reconnect

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
