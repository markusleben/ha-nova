# Why skills instead of tools

HA NOVA is deliberately **not** an MCP server. This page explains that choice honestly, including where the alternatives are ahead.

Everything below reflects the state on **2026-07-11**. The main alternative — [ha-mcp](https://github.com/homeassistant-ai/ha-mcp), "The Unofficial and Awesome Home Assistant MCP Server" (MIT, ~87 tools) — is a good, actively maintained project, and HA NOVA is not trying to pretend otherwise. Their approach is different, and the differences are real in both directions.

## The three approaches

| | **Home Assistant's official MCP server** | **ha-mcp** (community MCP server) | **HA NOVA** |
|---|---|---|---|
| What the AI gets | The Assist API (exposed entities) | ~87 MCP tools | 24 markdown skills + 3 generic relay endpoints |
| Can it edit automations, dashboards, the registry? | No | Yes | Yes |
| Where the domain knowledge lives | In Home Assistant | In the server's Python code | In markdown you can read and edit |
| Protocol dependency | MCP | MCP | none — skills are plain text |

## What "skills instead of tools" actually buys you

**Context cost.** An MCP client loads every tool definition into the model's context at the start of a session, whether you use them or not. At ~87 tools, that is real weight — enough that ha-mcp built a BM25 search mode to hide tools from smaller models. HA NOVA loads a skill only when the task needs it: a light-switching request never sees the dashboard rules.

**You can read and change the behavior.** When HA NOVA gets something wrong, the fix is a markdown edit you can make yourself. When a tool-based server gets something wrong, the fix is a Python change, a release, and an update. Every safety rule on this page is a sentence in a file you can open.

**It does not depend on a protocol.** MCP is a good standard and it is not going away — but skills are just text. The same skills work in Claude Code, Codex, OpenCode, Antigravity, and Hermes today, and they will work in whatever comes next, because there is nothing to port. (Agent Skills is now an open standard under the Linux Foundation's Agentic AI Foundation, which makes this bet less lonely than it was.)

**The server stays dumb on purpose.** HA NOVA's relay is ~1,800 lines that forward requests and nothing more — no domain handlers, no caching, no interpretation. A build-time check fails if that ever stops being true. The consequence: the relay cannot silently reinterpret your request, because it does not know what a light is.

## Safety, side by side

This is where the projects differ most, and it is the reason HA NOVA exists.

| | ha-mcp | HA NOVA |
|---|---|---|
| Preview before a write | Per-tool `confirm` parameters and approval rules you configure | Enforced for every mutation skill, in a block that is byte-identical across all of them and asserted by a test |
| Confirmation binding | Parameter-level | Bound to the exact preview shown; expires if the payload, target, or scope changes |
| Deletes | `destructiveHint` / confirm parameter | A typed token (`confirm:del-…`); "yes" is rejected, and a click-menu is never offered instead |
| Verification after a write | Per-tool | The config is read back and compared; "it saved" is never reported as "it works" |
| Undo | Full-system backups | Per-config revert of the last 5 changed targets — plus an honest statement wherever revert does *not* exist |
| Unknown APIs | The tool set is the boundary | An AI may not trial-and-error an unfamiliar Home Assistant API: it must go through the fallback skill first |
| Token storage | Long-lived token in client config (or in-process, with the HACS component) | Home Assistant token stays on the server; the relay token lives in your OS keychain |
| Process isolation | The recommended install runs inside Home Assistant | Separate process (App or container): a relay crash cannot take Home Assistant down |

Every HA NOVA row is backed by a file and a test in **[safety.md](safety.md)** — read that page instead of trusting this one.

## Where ha-mcp is ahead (as of 2026-07-12)

Being honest about this is the point of the page:

- **Breadth of file editing.** ha-mcp can read/write files broadly (beta, with automatic backups). HA NOVA's file access (`ha-nova:yaml-config`, relay ≥ 0.4.0) is deliberately narrower: opt-in and off by default, configuration formats only, executable paths refused outright — and the write flow it shipped with is diff → confirm → `check_config` → reload → verify → rollback.
- **Add-on and HACS management.** ha-mcp can install and manage those. HA NOVA points you at the Home Assistant UI.
- **Dashboard screenshots** and **ZHA device inspection**: ha-mcp has them, HA NOVA does not.
- **Maturity of reach.** ha-mcp has more stars, more contributors, and more integrations wired up. HA NOVA is younger.

## Where HA NOVA is ahead (as of 2026-07-12)

- **Media, notifications, MQTT, and voice** have dedicated skills with real domain rules — feature-bit gating before a media call, the iOS/Android payload split for notifications, retained-vs-live distinction when listening to MQTT, utterance testing that says plainly it executes what it understands. ha-mcp does not cover these.
- **Root-cause diagnosis** as a workflow (traces → logs → bounded history → template probe), not just raw log access.
- **The safety model above**, which is enforced structurally rather than per tool.
- **Runs on every Home Assistant install** — App for HA OS/Supervised, container for Container/Core — from one codebase.

## Which should you use?

If you want the broadest tool coverage today and are comfortable with MCP, ha-mcp is a solid choice and we would rather you use it than nothing.

Use HA NOVA if you want an AI that shows you what it will change before it changes it, verifies afterwards, tells you when it cannot undo something, and whose behavior you can read in plain text and edit yourself.
