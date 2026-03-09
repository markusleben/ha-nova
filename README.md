<p align="center">
  <img src="assets/social-preview.png" alt="HA NOVA">
</p>

<p align="center">
  <a href="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml"><img src="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/markusleben/ha-nova/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/node-%3E%3D20-brightgreen" alt="Node >= 20">
  <img src="https://img.shields.io/badge/platform-macOS-lightgrey" alt="macOS">
</p>

## What is HA NOVA?

HA NOVA connects your AI to Home Assistant. You talk in plain language, like *"create an automation that turns on the porch light at sunset"*, and your AI handles it safely, with a preview before any change is made.

How? A small app on your HA server acts as a secure bridge (we call it the *relay*), while plain text files called *skills* teach your AI how Home Assistant works. A setup wizard installs the relay on your HA server and configures your AI client — you don't need to understand the internals to get started.

Works with **Claude Desktop, Claude Code, Codex CLI, OpenCode, and Gemini CLI**.

> **Early Access:** The core works well, but you might hit rough edges. macOS only for now (Linux/Windows coming). Back up your configs before letting AI make changes. Found a problem? [Open an issue](https://github.com/markusleben/ha-nova/issues).

### See it in action

<img src="assets/demo.webp" alt="HA NOVA demo: creating a smart automation from plain English">

> One sentence in, a fully reviewed automation out — including suggestions you might not have thought of.

## Quick Start

> **You need:** macOS, [Node.js 20+](https://nodejs.org), Home Assistant OS or Supervised

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

The wizard handles everything: relay, tokens, skills. Just pick your AI client. Once done, open your AI client and try: *"Show me all my automations."*

**Cloned the repo manually?** `npx ha-nova setup` | **Something broken?** `npx ha-nova doctor`

## What Can You Do?

Every automation and script change follows a careful process:

1. **Research:** Looks up your devices, checks existing configs, resolves the right entities
2. **Preview:** Shows you the exact config that will be written. Nothing happens until you say OK.
3. **Apply & Verify:** Writes it, reads it back to make sure it actually stuck
4. **Review:** Checks the result against 40+ rules for common mistakes, conflicts, and reliability issues

Deleting anything requires a confirmation code, not just "yes".

| You say | What happens |
|---------|-------------|
| *"Turn on the porch light at sunset and off at 11 PM"* | Creates a fully reviewed automation using the safety flow above |
| *"Why didn't my motion automation trigger last night?"* | Analyzes the actual trace logs, explains what went wrong |
| *"Check my automations for problems"* | Runs a thorough config audit across your setup |
| *"Turn off the living room lights"* | Controls it, confirms the new state |
| *"Show me all sensors in the bedroom"* | Finds entities by room, area, or name |
| *"Create a counter helper for my coffee intake"* | Creates the helper, shows the result |

## How It Works

<p align="center">
  <img src="assets/how-it-works.png" alt="How HA NOVA works: Your AI Client talks to the NOVA Relay on your HA server, which connects to Home Assistant. Skills teach the AI what to do.">
</p>

**The Relay** runs directly on your Home Assistant server. It forwards requests securely while keeping your access token safely on the server — never on your machine. Only ~1,500 lines of code — it just forwards requests without interpreting them. That helps keep it stable across HA updates.

**The Skills** are plain text files on your machine. Each skill is self-contained — your AI loads only the one it needs for the current task. Want to teach your AI something new about HA? Write a markdown file. No code, no compilation, no deployment.

### What's coming

Because the relay sits on the HA host with local access to the config directory, it can extend what the API offers:

- **Automation versioning:** automatic backup before every change, stored locally on the HA host
- **Linux & Windows support**

### How does this compare?

| | MCP Servers | Skills-only projects | HA NOVA |
|---|---|---|---|
| **HA connectivity** | Tools call HA API directly | Typically no API access (SSH/scp) | Relay on HA server (API + local access) |
| **Intelligence** | Encoded in tool definitions | In prompt context | In modular skills |
| **Context efficiency** | All tools loaded at once | Single file loaded | Only relevant skill loaded per task |
| **Extending** | Write code, deploy, update schemas | Edit one markdown file | Edit one markdown file |
| **Safety flow** | Tool-level (per call) | Manual verification | 4-phase: research → preview → apply → review |
| **Multi-client** | Varies (1-15 clients) | Usually 1 client | 5 clients (Claude Desktop, Claude Code, Codex CLI, OpenCode, Gemini CLI) |
| **Server maintenance** | More code to maintain per HA update | None | Minimal (relay is just transport) |

**Why not an MCP server?** You can absolutely use one — and good ones exist. We chose skills because adding a new capability means editing a text file instead of writing code. Different trade-off, not a better/worse one.

**Can't I just call the HA API directly?** You can! But you'd miss the safety flow, the automated config checks, the conflict detection, and the secure token isolation the relay provides.

## Skills

| Skill | What it does |
|-------|-------------|
| **write** | Create, update, delete automations and scripts with full 4-phase safety flow |
| **read** | Browse configs, inspect automations, debug with trace analysis |
| **review** | Audit for 40+ common mistakes, conflicts, and best-practice violations |
| **service-call** | Control devices: lights, climate, covers, switches, media players |
| **entity-discovery** | Find entities by name, room, or area |
| **helper** | Manage helpers (input_boolean, counter, timer, schedule, and more) |
| **guide** | Discover HA features: dashboards, blueprints, energy management |
| **onboarding** | Setup diagnostics and troubleshooting |

Want to add a new capability? → [CONTRIBUTING.md](CONTRIBUTING.md)

## Safety

- **Preview first:** every change is shown before it happens
- **Confirmation codes:** deletes require a specific code, not just "yes"
- **Post-write review:** after every change, the AI checks for mistakes and conflicts
- **Token isolation:** your HA token stays on the HA server, never on your machine
- **Encrypted auth:** client-side credentials in macOS Keychain, not in config files
- **Runs on your network:** no cloud dependency, no tracking (your AI client's own cloud usage is separate)

## Supported AI Clients

| Client | Type |
|--------|------|
| [Claude Desktop](https://claude.com/download) (Code tab) | Desktop app |
| [Claude Code](https://github.com/anthropics/claude-code) | Terminal |
| [Codex CLI](https://github.com/openai/codex) | Terminal |
| [OpenCode](https://github.com/nicepkg/OpenCode) | Terminal |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Terminal |

> **Don't like terminals?** Claude Desktop gives you the same capabilities in a graphical chat interface:
> 1. Run the install command and select "Claude Code" (they share the same config)
> 2. Open Claude Desktop and switch to the **Code** tab
> 3. Pick any folder on your Mac as workspace and start talking

## Contributing

HA NOVA is early — a good time to help shape it.

- **Write a skill:** just a markdown file, no code needed
- **Test on your setup:** find what works, report what doesn't
- **Tackle an [open issue](https://github.com/markusleben/ha-nova/issues)**

→ [CONTRIBUTING.md](CONTRIBUTING.md) for details

## The Story Behind It

I spent over a year building an MCP server for Home Assistant. Hundreds of tool definitions, thousands of lines of code. I kept polishing, never releasing. By the time I looked up, others had shipped theirs while mine was still on my machine.

**[Here's an early demo](https://youtu.be/ylak867RkzM)** from that time.

Then I realized the approach was wrong. Instead of encoding domain knowledge into a server, I could write it as plain text that the AI reads directly. I scrapped everything and started fresh. HA NOVA is the result.

## Project Structure

```
nova/        Relay app (runs on your HA server)
skills/      AI skills (markdown files)
scripts/     Setup, deploy, diagnostics
tests/       Test suite
```

## License

[MIT](LICENSE)
