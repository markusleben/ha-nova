# NOVA Relay

The NOVA Relay connects your AI coding client to Home Assistant.
It provides secure, authenticated access to Home Assistant's APIs and extends them where needed.

## How It Works

```
AI Client → NOVA Relay (this add-on) → Home Assistant APIs
```

Your AI client (Claude Code, Codex, OpenCode, Gemini CLI) connects to the relay
using an auth token. Intelligence lives in the AI client's skills, not in the relay.

## Configuration

| Option | Description |
|--------|-------------|
| **Relay Auth Token** | Shared secret between your AI client and this relay. Generated automatically during setup. Both sides must use the exact same value. |
| **Home Assistant Access Token** | A Long-Lived Access Token from your HA profile. Create one at: **Profile > Security > Long-Lived Access Tokens**. |

### Network

| Port | Description |
|------|-------------|
| 8791/tcp | Relay HTTP API |

## Setup

The easiest way to set up everything (relay + AI client + skills):

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

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

This verifies: config file, keychain token, relay reachability, WebSocket connection,
and relay version.

You can also check the relay directly:

```bash
curl http://<your-ha-ip>:8791/health
```

A healthy response looks like:

```json
{"status": "ok", "ha_ws_connected": true, "version": "0.1.0"}
```

## Troubleshooting

**Relay not reachable**

- Verify the add-on is running (green icon in the header)
- Check that port 8791 is not blocked by your network/firewall
- Ensure the correct host IP is configured in your AI client

**Authentication failed**

- The relay auth token must match exactly on both sides
- Re-run `ha-nova setup` to regenerate and sync tokens
- Check that the HA Access Token is still valid (not revoked)

**WebSocket not connected**

- The relay connects to HA's WebSocket API on startup
- Check the add-on logs for connection errors
- Restart the add-on to force a reconnect

## Logs

Add-on logs are available in the **Log** tab above. Look for:

- `Relay listening` — relay started successfully
- `Relay bootstrap` — shows which auth method is active
- Any `error` or `warn` messages indicate issues

## Support

- [GitHub Issues](https://github.com/markusleben/ha-nova/issues)
- [Setup Guide](https://github.com/markusleben/ha-nova#quick-start)
- [Full Documentation](https://github.com/markusleben/ha-nova)
