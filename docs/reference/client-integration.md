# Client Integration

HA NOVA clients are registered in `clients/registry.json`.

The registry is metadata and capability truth. Install behavior still lives in explicit client adapter code; do not add a registry-only client and expect setup/install to work without adapter coverage.

## Minimal Client

```json
{
  "id": "future",
  "label": "Future Client",
  "adapter_kind": "skill_tree",
  "supported_os": ["macos", "linux"]
}
```

## Service Credentials

Some clients can run from a service, gateway, SSH, or headless session where the desktop keyring is not unlocked. Those clients can opt in to a setup prompt:

```json
{
  "id": "hermes",
  "label": "Hermes Agent",
  "adapter_kind": "skill_tree",
  "supported_os": ["macos", "linux"],
  "setup": {
    "service_credentials": {
      "recommended_when": ["headless", "locked_keyring", "systemd_user_service"],
      "label": "Service / gateway mode",
      "help": "Use this when the client runs without an unlocked desktop keyring."
    }
  }
}
```

This enables `ha-nova setup --service <client>` and lets interactive setup offer a service token file when the selected client declares the capability. The setup flow must stay generic: do not hardcode a Hermes-only branch for this prompt.

The service token file stores only the Relay Auth Token. It never stores upstream Home Assistant credentials: the App keeps `SUPERVISOR_TOKEN` process-local and standalone relays keep `HA_LLAT` server-side.

The paired **device credential** has its own file-backend equivalent: `setup --service` forces it, and `ha-nova pair --credential-store=file` opts in without a full setup. Both route the device credential to a private 0600 file under `~/.config/ha-nova/secrets/` — the explicit answer for machines whose desktop keyring exists but is never unlocked (headless VMs, autologin boxes, systemd user services). A present-but-locked keyring without one of these opt-ins stays a hard error by design; the storage never downgrades silently.

## OS Overrides

Use `per_os` when a setup capability differs by platform:

```json
{
  "per_os": {
    "windows": {
      "setup": {
        "service_credentials": {
          "disabled": true
        }
      }
    }
  }
}
```

Keep registry metadata declarative. Do not add cleanup globs, shell hooks, or dynamic scripts to client entries.

## Experimental: MCP adapter (`ha-nova mcp`)

> **Experimental — not supported yet; the interface may change.** This section is for testers. It is not an advertised client path: the supported way to use HA NOVA is a client that loads the skills. The adapter is planned to carry the skills before it is announced.

`ha-nova mcp` is a local stdio MCP server that reuses this computer's pairing — run `ha-nova setup` first; the server never asks for credentials and never opens a keyring prompt. Add it to the client's MCP configuration (Claude Desktop: `claude_desktop_config.json`; other clients use the same `command`/`args` shape):

```json
{
  "mcpServers": {
    "ha-nova": {
      "command": "ha-nova",
      "args": ["mcp"]
    }
  }
}
```

With several servers, pin one: `"args": ["mcp", "--server", "<name>"]` (or set `HA_NOVA_SERVER` in the entry's `env`).

The server offers ten tools. `ha_skill` returns a skill's workflow — the same text skill-mode clients read, with the CLI commands mapped to the tools — and reads no Home Assistant data; the server's instructions tell the model to load the matching skill before it builds, changes, reviews, or diagnoses anything. The other nine run the same checks as the CLI verbs: `ha_find`, `ha_get`, `ha_history`, `ha_map` (exact home counts and where-used), `ha_trace`, and `ha_read` are read-only (`ha_read` reads what `ha-nova relay` reads, through the same guard: REST GETs except webhooks, backup downloads, Supervisor requests, media proxies, streams, and flow steps (config entry, options, subentry, and repair flows), plus `POST /api/template` and `/api/config/core/check_config`; WebSocket reads, also in a `collect_events` envelope; a camera image (JPEG, PNG, GIF, or WebP up to 5 MiB, as image content); `/config` files and config snapshots (list and read only); undo checkpoints and `diff` offline; with an optional `jq` filter, and results over 64 KiB refused. `blueprint/import`, `camera/stream`, and `media_source/resolve_media` are left to a plan the user sees); `ha_plan` previews changes (kind raw: up to 20 requests to Home Assistant's own API that the skills make — registry, helper, scene, integration setup, file, and config snapshot writes — each through the same guard as `ha-nova relay` at planning and again right before `ha_apply` sends it; reads other than `blueprint/import`, `camera/stream`, `media_source/resolve_media`, and a flow step GET go to `ha_read`); `ha_call` controls devices (ordinary calls run immediately and are verified); `ha_apply` executes a confirmed plan. A plan that needs a four-digit code (locks, gates, garage doors, alarm disarm, deletes, new accounts, HACS downloads, dashboard resources, an energy save that empties a list or drops several of its entries) runs only with its exact code. Each skill is available as a prompt (`ha-nova`, `ha-nova-<skill>`) and the shared references as `ha-nova://skills/…` resources. The server speaks MCP `2026-07-28` (per-request metadata, `server/discover`) and the `initialize`-based revisions `2025-11-25`, `2025-06-18`, and `2024-11-05`.

Where this computer can show a confirmation window, or else a phone is linked to HA NOVA (Companion app, `ha-nova setup confirm-phone`), that window or phone asks first and no code is shown anywhere (`docs/work/2026-10-17-human-confirmation-channels-spec.md`); the form below applies only where neither can. When your client can show confirmation forms (MCP elicitation, form mode — for example Claude Code, Codex, VS Code, Cursor), the model never sees the code: `ha_apply` makes your app show a form with the plan's summary and its four-digit one-time code from HA NOVA, and only what you type there confirms the plan; a code the model passes is ignored, and declining or closing the form sends nothing. If your app turns the form down without showing it (some do in unattended or extension modes), add `"--no-forms"` to `args`: the code is then shown in the tool result again, so the model sees it, and your app's approval prompt for `ha_apply` stays the gate. A preview (a plain yes) is still confirmed in the chat, so in every client keep the approval prompt for `ha_apply` on, read what it shows, and never choose "always allow" for `ha_apply`. A client without forms (Claude Desktop, as of October 2026) shows the code in the tool result, so the model could in principle pass it on its own; there that approval prompt is the human gate for coded plans too. `ha_apply` sends a raw plan's items in order and stops at the first one that doesn't go through. Dashboard edits run through the CLI only (`ha-nova plan write --domain dashboard`) until the bridge maps them.
