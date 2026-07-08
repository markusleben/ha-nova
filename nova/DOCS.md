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
| **Relay Auth Token** | Shared secret between your AI client and this relay. Generated automatically during setup. The relay and your AI client must use the exact same value. |
| **Home Assistant Access Token** | A Long-Lived Access Token from your HA profile. Create one at: **Profile > Security > Long-Lived Access Tokens**. |

> **Where is this page?** Home Assistant 2026.2 renamed Add-ons to Apps: this
> page moved from `/hassio/addon/<slug>/info` to `/config/app/<slug>/info`
> (Settings > Apps > NOVA Relay). The setup wizard links through HA's own
> `/_my_redirect/` endpoint, so its links work on both old and new versions.

### Network

| Port | Description |
|------|-------------|
| 8791/tcp | Relay HTTP API |

## Endpoints

The relay exposes three endpoints. All relay requests, including `GET /health`, require the Relay Auth Token via `Authorization: Bearer <token>`.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Relay health check (version, uptime, WS status) |
| POST | `/ws` | WebSocket proxy — forwards commands to HA's WS API |
| POST | `/core` | REST API proxy — forwards requests to HA's Core REST API (`/api/...`) |

## Setup

The easiest way to set up everything (relay + AI client + skills):

- open [README.md](../README.md)
- open the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest)
- copy the stable install one-liner for your OS and run it unchanged

Or if you already have the repo:

```bash
ha-nova setup
```

The setup wizard handles token generation, relay configuration, and skill installation.

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

- The relay auth token must match exactly on both sides
- Re-run `ha-nova setup` to regenerate and sync tokens
- Check that the HA Access Token is still valid (not revoked)

**WebSocket not connected**

- The relay connects to HA's WebSocket API on startup
- Check the App logs for connection errors
- Restart the App to force a reconnect

## Logs

App logs are available in the **Log** tab above. Look for:

- `Relay listening` — relay started successfully
- `Relay bootstrap` — shows auth source and capability
- Any `error` or `warn` messages indicate issues

## Support

- [GitHub Issues](https://github.com/markusleben/ha-nova/issues)
- [Setup Guide](https://github.com/markusleben/ha-nova#-get-started)
- [Full Documentation](https://github.com/markusleben/ha-nova)
