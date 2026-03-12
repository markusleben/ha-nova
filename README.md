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

HA NOVA gives AI a clear and safe way to work with Home Assistant.

Instead of letting an agent randomly create, change, or delete things, HA NOVA pushes it through the right path for the task. Risky changes should be researched first, then previewed, then checked after they are applied.

A big part of that logic lives in plain markdown files called *skills*. They teach the AI how to work with Home Assistant.

And in the background there is a small app on your HA server called the *relay*. It is kind of the little secret weapon of the whole setup :)

The relay is not an MCP server — it is just a small helper on the Home Assistant server, while the real logic and workflows live in the skills.

It does not replace the normal Home Assistant API. The special thing is that it runs directly on the Home Assistant server. That means it can help with things where being close to Home Assistant really matters.

A setup wizard installs the relay on your HA server and configures your AI client, so you do not need to understand the internals to get started.

Works with **Claude Desktop, Claude Code, Codex CLI, OpenCode, and Gemini CLI**.

> **Early Access:** The core works well, but you might hit rough edges. macOS only for now (Linux/Windows coming). Back up your configs before letting AI make changes. Found a problem? [Open an issue](https://github.com/markusleben/ha-nova/issues).

### See it in action

<img src="assets/demo.webp" alt="HA NOVA demo: creating a smart automation from plain English">

> *"When I get home, set the living room lights to a warm welcome ambiance"* — one sentence in, a fully reviewed automation out, including suggestions you might not have thought of.

## 🚀 Quick Start

> **You need:** macOS, [Node.js 20+](https://nodejs.org), Home Assistant OS or Supervised

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

The wizard handles everything: relay, tokens, skills. Just pick your AI client. Once done, open your AI client and try: *"Show me all my automations."*

**Cloned the repo manually?** `./scripts/onboarding/bin/ha-nova setup` | **Installed already and something broken?** `ha-nova doctor`

## 💬 What Can You Do?

Automation and script changes follow a careful path:

1. **Research:** Looks up your devices, checks existing configs, resolves the right entities
2. **Preview:** Shows you the exact config that will be written. Nothing happens until you say OK.
3. **Apply & Verify:** Writes it, reads it back to make sure it actually stuck
4. **Review:** Checks the result against 40+ rules for common mistakes, conflicts, and reliability issues

Reads and simple control tasks can stay lighter. But the main goal is always the same: no random actions, no guessed IDs, and no risky writes without a clear check first.

Deleting anything requires a confirmation code, not just "yes".

| You say | What happens |
|---------|-------------|
| *"Turn on the porch light at sunset and off at 11 PM"* | Creates a fully reviewed automation using the safety flow above |
| *"Why didn't my motion automation trigger last night?"* | Analyzes the actual trace logs, explains what went wrong |
| *"Check my automations for problems"* | Runs a thorough config audit across your setup |
| *"Turn off the living room lights"* | Controls it, confirms the new state |
| *"Show me all sensors in the bedroom"* | Finds entities by room, area, or name |
| *"Create a counter helper for my coffee intake"* | Creates the helper, shows the result |

## ⚙️ How It Works

<p align="center">
  <img src="assets/how-it-works.png" alt="How HA NOVA works: Your AI Client talks to the NOVA Relay on your HA server, which connects to Home Assistant. Skills teach the AI what to do.">
</p>

**The Skills** are plain text files on your machine. They hold the rules, the logic, and the flow the AI should follow. Each skill is self-contained — your AI loads only the one it needs for the current task. Want to teach your AI something new about Home Assistant? Write a markdown file. No code, no compilation, no deployment.

**The Relay** runs directly on your Home Assistant server. It keeps your access token on the server — never on your machine — and quietly helps with the things the AI cannot or should not do directly. For example: WebSocket features today, and later maybe things like safe backup or restore flows on the Home Assistant side. It stays small on purpose and does not try to become the intelligence layer.

### 🗺️ What's coming

- **Automation versioning:** automatic backup before every change, stored locally on the HA host
- **Linux & Windows support**

### 📊 How does this compare to MCP servers?

| | MCP Servers | HA NOVA |
|---|---|---|
| 🔌 **Connectivity** | Tools call HA API directly | Relay on HA server (API + local file access) |
| 🧠 **Knowledge** | In tool code + optional resources | In modular markdown skills |
| 📦 **Context** | Tools loaded at startup | Only relevant skill loaded per task |
| 🔧 **Extending** | Write code, deploy | Edit a markdown file |
| 🛡️ **Safety** | Per-tool (annotations, confirm flags) | 4-phase: research → preview → apply → review |
| 🖥️ **Clients** | Any MCP-compatible client | 5 tested clients |

Both approaches work. MCP servers have broader client support out of the box. We chose skills because adding a new capability means editing a text file instead of writing code — different trade-off, not a better/worse one.

**Can't I just call the HA API directly?** You can! But HA NOVA adds a safer and more guided way to solve Home Assistant tasks, plus a small relay on the Home Assistant side for things where host-side access really helps.

## 🧩 Skills

| Skill ID | What it does |
|-------|-------------|
| ✏️ **ha-nova-write** | Create, update, delete automations and scripts with the 4-phase safety flow |
| 📖 **ha-nova-read** | Browse configs, inspect automations, debug with trace analysis |
| 🔍 **ha-nova-review** | Audit for 40+ common mistakes, conflicts, and best-practice violations |
| 🎛️ **ha-nova-service-call** | Control devices: lights, climate, covers, switches, media players |
| 🔎 **ha-nova-entity-discovery** | Find entities by name, room, or area |
| 🧩 **ha-nova-helper** | Manage helpers (input_boolean, counter, timer, schedule, and more) |
| 🛡️ **ha-nova-fallback** | Safety fallback for dashboards, blueprints, energy, areas, and other relay-ready features |
| 🚀 **ha-nova-onboarding** | Setup diagnostics and troubleshooting |

Want to add a new capability? → [CONTRIBUTING.md](CONTRIBUTING.md)

## 🛡️ Safety

- **Preview first:** every change is shown before it happens
- **Confirmation codes:** deletes require a specific code, not just "yes"
- **Post-write review:** after every change, the AI checks for mistakes and conflicts
- **Token isolation:** your HA token stays on the HA server, never on your machine
- **Encrypted auth:** client-side credentials in macOS Keychain, not in config files
- **Runs on your network:** no cloud dependency, no tracking (your AI client's own cloud usage is separate)

## 🖥️ Supported AI Clients

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

## 🤝 Contributing

HA NOVA is early — a good time to help shape it.

- **Write a skill:** just a markdown file, no code needed
- **Test on your setup:** find what works, report what doesn't
- **Tackle an [open issue](https://github.com/markusleben/ha-nova/issues)**

→ [CONTRIBUTING.md](CONTRIBUTING.md) for details

## 📖 The Story Behind It

I spent over a year building an MCP server for Home Assistant. Hundreds of tool definitions, thousands of lines of code. I kept polishing, never releasing. By the time I looked up, others had shipped theirs while mine was still on my machine.

**[Here's an early demo](https://youtu.be/ylak867RkzM)** from that time.

Then I realized the approach was wrong. Instead of encoding domain knowledge into a server, I could write it as plain text that the AI reads directly. I scrapped everything and started fresh. HA NOVA is the result.

## 📁 Project Structure

```
nova/        Relay app (runs on your HA server)
skills/      AI skills (markdown files)
scripts/     Setup, deploy, diagnostics
tests/       Test suite
```

## 📄 License

[MIT](LICENSE)

## 🙏 Acknowledgments

Some Home Assistant safety-rule ideas in HA NOVA were inspired by [HALMark](https://github.com/nathan-curtis/HALMark) by Nathan Curtis.

Automation best-practice patterns, helper selection guidance, and Zigbee device-control patterns were adapted from [homeassistant-ai/skills](https://github.com/homeassistant-ai/skills) by Sergey Kadentsev ([@sergeykad](https://github.com/sergeykad)) and Julien Lapointe ([@julienld](https://github.com/julienld)).
