# Why skills instead of tools

HA NOVA is deliberately **not** an MCP server. This page explains that choice honestly, including where the alternatives are ahead.

Everything below reflects the state on **2026-07-20**. The main alternative — [ha-mcp](https://github.com/homeassistant-ai/ha-mcp), "The Unofficial and Awesome Home Assistant MCP Server" (MIT, 87 tools) — is a good, actively maintained project, and HA NOVA is not trying to pretend otherwise. Their approach is different, and the differences are real in both directions.

## The three approaches

| | **Home Assistant's official MCP server** | **ha-mcp** (community MCP server) | **HA NOVA** |
|---|---|---|---|
| What the AI gets | The Assist API (exposed entities) | 87 MCP tools | 29 task skills (+ 1 context skill) + 4 generic relay endpoints |
| Can it edit automations, dashboards, the registry? | No | Yes | Yes |
| Where the domain knowledge lives | In Home Assistant | In the server's Python code | In markdown you can read and edit |
| Protocol dependency | MCP | MCP | none — skills are plain text |

## What "skills instead of tools" actually buys you

**The know-how travels with the task.** A tool definition tells the model what it *can* call; it carries no procedure. A skill is the procedure: what to research first, what to show you before writing, what to verify afterwards, when to refuse. ha-mcp ships 87 capable tools; how carefully they are used depends on the model and your client's approval settings. HA NOVA ships the workflow itself, and the workflow is the same every time.

**Context, honestly.** By default an MCP client receives the full tool catalog (ha-mcp's README says so plainly, and offers an opt-in search mode); clients with deferred tool loading — Claude Code among them — mitigate this well today, pulling tools in only when needed. So we will not pretend token cost alone is the argument in 2026. The difference that remains: deferred loading fetches tool *names and schemas* on demand — a skill fetches *domain knowledge* on demand. One tells the model what exists; the other tells it how to do the job safely.

**You can read and change the behavior.** When HA NOVA gets something wrong, the fix is a markdown edit you can make yourself. When a tool-based server gets something wrong, the fix is a Python change — yours to patch and maintain, or upstream's to release. Every safety rule on this page is a sentence in a file you can open.

**It does not depend on a protocol.** MCP is a good standard and it is not going away — but skills are just text. The same skills work in Claude Code, Codex, OpenCode, Antigravity, and Hermes today, and they will work in whatever comes next, because there is nothing to port. (Agent Skills is now an open standard under the Linux Foundation's Agentic AI Foundation, which makes this bet less lonely than it was.)

**The server stays dumb on purpose.** HA NOVA's relay forwards requests and nothing more — no domain handlers, no caching, no interpretation. A build-time check fails if that ever stops being true. The consequence: the relay cannot silently reinterpret your request, because it does not know what a light is.

## Safety and access, side by side

This is where the projects differ most, and it is the reason HA NOVA exists.

| | ha-mcp | HA NOVA |
|---|---|---|
| Preview before a write | Approval policies and per-tool controls you configure, plus automatic edit backups | Enforced for every mutation skill, in a block that is byte-identical across all of them and asserted by a test |
| Confirmation binding | Client-side approval prompts | Bound to the exact preview shown; expires if the payload, target, or scope changes |
| Deletes | A separated delete tool so clients can apply stricter policies | A typed confirmation code (`confirm:del-…`); "yes" is rejected, and a click-menu is never offered instead — plus an automatic config snapshot as the restore path |
| Verification after a write | Per-tool | The config is read back and compared; "it saved" is never reported as "it works" |
| Undo | Automatic edit backups; full-system backups | Per-config revert of the last verified update, config-snapshot restore for supported deletes — plus an honest statement wherever revert does *not* exist |
| Unknown APIs | The tool set is the boundary | An AI may not trial-and-error an unfamiliar Home Assistant API: it must go through the fallback skill first |
| Where it runs | The recommended install runs in-process inside Home Assistant ("no access token to manage") | A separate process (App or container): a relay crash cannot take Home Assistant down, and the relay holds one scoped upstream credential |
| Connecting a device | Implicit in-process; remote clients use a secret webhook URL, `ha_auth`, or OAuth where configured | A one-time six-digit code per device (OPAQUE, RFC 9807 over SPKI-pinned TLS 1.3); each device individually revocable from the NOVA console. Standalone Container/Core stays explicit-token-first |
| Upstream HA credential | Implicit in-process; token env vars for standalone deployments | Never on the client: the App uses Home Assistant's own Supervisor access, standalone keeps `HA_LLAT` server-side |

Every HA NOVA row is backed by a file and a test — see **[safety.md](safety.md)**; read that page instead of trusting this one.

## Where ha-mcp is ahead (as of 2026-07-20)

Being honest about this is the point of the page:

- **Breadth of tools.** Add-on and HACS management, dashboard screenshots (beta), ZHA device inspection, broad file editing with automatic backups. HA NOVA's file access is deliberately narrower: opt-in and off by default, configuration formats only, executable paths refused outright.
- **Zero-setup auth in its recommended mode.** The in-process component needs no token management at all, and OAuth/OIDC options exist for remote access.
- **Maturity of reach.** More stars, far more contributors, more integrations wired up, and an older public history. HA NOVA is younger.

## Where HA NOVA is ahead (as of 2026-07-20)

- **Per-device pairing you can see and revoke.** One six-digit code per device, a console that lists every paired machine, one-click revocation — and no Home Assistant token to create anywhere in the App flow.
- **Media, notifications, MQTT, and voice** have dedicated skills with real domain rules — feature-bit gating before a media call, the iOS/Android payload split for notifications, retained-vs-live distinction when listening to MQTT, utterance testing that says plainly it executes what it understands.
- **Root-cause diagnosis** as a workflow (traces → logs → bounded history → template probe), not just raw log access.
- **The safety model above**, enforced structurally rather than configured per tool.
- **Process isolation by design** — the AI-facing surface never runs inside Home Assistant's own process.
- **Runs on every Home Assistant install** — App for HA OS/Supervised, container for Container/Core — from one codebase.

## Which should you use?

If you want the broadest tool coverage today and are comfortable with MCP, ha-mcp is a solid choice and we would rather you use it than nothing.

Use HA NOVA if you want an AI that shows you what it will change before it changes it, verifies afterwards, tells you when it cannot undo something, and whose behavior you can read in plain text and edit yourself.
