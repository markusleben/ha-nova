# NOVA Relay

The NOVA Relay connects your AI coding client to Home Assistant.
It provides secure, authenticated access to Home Assistant's APIs and extends them where needed.

## How It Works

```
AI Client → NOVA Relay (this App) → Home Assistant APIs
```

Your AI client (Claude Code, Claude Desktop, Codex, OpenCode, Google Antigravity, Hermes Agent) connects
to the relay using an auth token. Intelligence lives in the AI client's skills, not
in the relay.

## Configuration

| Option | Description |
|--------|-------------|
| **Relay Auth Token (legacy/advanced)** | Existing installs may keep their shared token. New installs leave this empty; the App creates and persists a private token automatically. |
| **Home Assistant Access Token** | A Long-Lived Access Token from your HA profile. Create one at: **Profile > Security > Long-Lived Access Tokens**. |
| **File access (advanced)** | Optional access to supported Home Assistant configuration files. Defaults to off. |

> **Where is this page?** Home Assistant 2026.2 renamed Add-ons to Apps: this
> page moved from `/hassio/addon/<slug>/info` to `/config/app/<slug>/info`
> (Settings > Apps > NOVA Relay). The setup wizard links through HA's own
> `/_my_redirect/` endpoint, so its links work on both old and new versions.

### Network

| Port | Description |
|------|-------------|
| 8791/tcp | Relay HTTP API |

## Endpoints

The relay exposes six endpoints. Normal requests, including `GET /health`, require the Relay Auth Token via `Authorization: Bearer <token>`. Exact `POST /pair` instead uses its short-lived pairing code as the credential.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Relay health check (version, uptime, WS status) |
| POST | `/pair` | One-time pairing-code exchange for the relay token |
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

The setup wizard handles relay configuration and skill installation. Until the pairing-capable CLI lands later in Wave 6, existing clients continue using the legacy relay-token option. Pairing-capable clients exchange the six-digit code without displaying the private relay token.

## Checking Status

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
- Check that the HA Access Token is still valid (not revoked)

**WebSocket not connected**

- The relay connects to HA's WebSocket API on startup
- Check the App logs for connection errors
- Restart the App to force a reconnect

## Logs

App logs are available in the **Log** tab above. Look for:

- `Relay listening` — relay started successfully
- `Relay bootstrap` — shows auth source and capability
- `Pairing code ready` — the initial short-lived fallback code; later rotations are intentionally not logged
- Any `error` or `warn` messages indicate issues

## Support

- [GitHub Issues](https://github.com/markusleben/ha-nova/issues)
- [Setup Guide](https://github.com/markusleben/ha-nova#-get-started)
- [Full Documentation](https://github.com/markusleben/ha-nova)
