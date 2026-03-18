<p align="center">
  <a href="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml"><img src="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/markusleben/ha-nova/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/node-%3E%3D20-brightgreen" alt="Node >= 20">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey" alt="macOS | Windows | Linux">
</p>

## What is HA NOVA?

HA NOVA gives your AI a way to actually control Home Assistant — without randomly breaking things.

Let an AI agent loose on your smart home and it'll happily create, change, or delete stuff without thinking twice. HA NOVA stops that. Every risky change follows a clear path: research first, preview what's about to happen, apply only after confirmation, then verify it actually worked.

The intelligence lives in plain markdown files called *skills*. They teach the AI how Home Assistant works, what to watch out for, and how to do things right. No code — just text files. Adding a new capability means writing a markdown file.

The *relay* is a small app running on your HA server. It keeps your token where it belongs — on the server, not on your laptop — and handles everything that needs direct host access. It stays small on purpose. The relay is the hands. The skills are the brain.

A setup wizard handles installation. Pick your AI client, follow the prompts, done.

Works with **Claude Code, Claude Desktop (Code tab), Codex CLI, OpenCode, and Gemini CLI**.

> **Early access.** The core works well, but expect rough edges. Back up your configs before letting AI touch anything. Hit a problem? [Open an issue](https://github.com/markusleben/ha-nova/issues).

### See it in action

<img src="assets/demo.webp" alt="HA NOVA demo: creating a smart automation from plain English">

> *"When I get home, set the living room lights to a warm welcome ambiance"* — one sentence in, a fully reviewed automation out, including suggestions you might not have thought of.

## 🚀 Quick Start

> **You need:** Home Assistant OS or Supervised. Node.js only for local dev, not for normal use.

### macOS / Linux

```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

macOS is live-validated. Linux uses the same installer and CI smoke path, but this release is not yet fully live-validated on a real Linux machine.

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
```

Windows ships an `amd64` bundle. ARM64 uses x64 emulation.

Claude and Gemini are validated on Windows. Codex and OpenCode lanes exist but are still experimental.

The wizard handles relay, tokens, and client setup. Once done, open your client and try: *"Show me all my automations."*

HA NOVA installs the integration layer. Install the AI client itself separately if you haven't already.

> Do not download the release `ha-nova-installer-bundle-*.tar.gz` / `.zip` assets and try to launch them manually. Those archives are installer payloads. Use `install.sh`, `install.ps1`, or `ha-nova update`.

**Old pre-Go install?**
- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

**Already installed?** `ha-nova setup` | **Something broken?** `ha-nova doctor`

## 💬 What Can You Do With It?

Anything that touches your config goes through a four-step safety flow:

1. **Research** — finds your devices, checks existing configs, resolves entity IDs
2. **Preview** — shows you exactly what will be written. Nothing happens until you say OK.
3. **Apply & Verify** — writes it, reads it back, checks it actually stuck
4. **Review** — audits against 40+ rules for common mistakes, conflicts, and reliability issues

Turning on a light stays lightweight. But anything that creates or changes automations goes through the full flow. No guessed entity IDs, no random writes, no surprises.

Deleting anything needs a confirmation code — not just "yes", an actual code. Because "yes" is too easy.

| You say | What happens |
|---------|-------------|
| *"Turn on the porch light at sunset and off at 11 PM"* | Creates a fully reviewed automation through the safety flow |
| *"Why didn't my motion automation trigger last night?"* | Digs into trace logs and explains what actually went wrong |
| *"Check my automations for problems"* | Runs a full config audit across your setup |
| *"Turn off the living room lights"* | Turns it off, confirms the new state |
| *"Show me all sensors in the bedroom"* | Finds entities by room, area, or name |
| *"Create a counter helper for my coffee intake"* | Creates it, shows the result |

## ⚙️ How It Works

<p align="center">
  <img src="assets/how-it-works.png" alt="How HA NOVA works: Your AI Client talks to the NOVA Relay on your HA server, which connects to Home Assistant. Skills teach the AI what to do.">
</p>

**Skills** are plain text files on your machine. Rules, logic, workflows — all in markdown. The AI loads only the skill it needs for the current task. Adding a capability means writing a markdown file. No code, no compilation, no deployment.

**The Relay** runs on your Home Assistant server. It keeps the token on the server, handles WebSocket features, and handles anything that needs direct host access. It stays small on purpose — the intelligence lives in the skills, not the relay.

### 📊 How does this compare to MCP servers?

| | MCP Servers | HA NOVA |
|---|---|---|
| 🔌 **Connectivity** | Tools call HA API directly | Relay on HA server (API + local file access) |
| 🧠 **Knowledge** | In tool code + optional resources | In modular markdown skills |
| 📦 **Context** | Tools loaded at startup | Only relevant skill loaded per task |
| 🔧 **Extending** | Write code, deploy | Edit a markdown file |
| 🛡️ **Safety** | Per-tool (annotations, confirm flags) | 4-phase: research → preview → apply → review |
| 🖥️ **Clients** | Any MCP-compatible client | Purpose-built client adapters |

Both approaches work. MCP servers have broader client support. HA NOVA trades that for simplicity — adding a new capability means editing a text file, not writing and deploying code. Different trade-off, not better or worse.

## 🧩 Skills

| Skill ID | What it does |
|-------|-------------|
| ✏️ **write** | Create, update, delete automations and scripts through the 4-phase safety flow |
| 📖 **read** | Browse configs, inspect automations, debug with trace analysis |
| 🔍 **review** | Audit for 40+ common mistakes, conflicts, and best-practice violations |
| 🎛️ **service-call** | Control devices: lights, climate, covers, switches, media players |
| 🔎 **entity-discovery** | Find entities by name, room, or area |
| 🧩 **helper** | Manage helpers (input_boolean, counter, timer, schedule, and more) |
| 🛡️ **fallback** | Safety fallback for dashboards, blueprints, energy, areas, and more |
| 🚀 **onboarding** | Setup diagnostics and troubleshooting |

Want to add a new capability? → [CONTRIBUTING.md](CONTRIBUTING.md)

## 🛡️ Safety

- **Preview first** — every change is shown before it happens
- **Confirmation codes** — deletes need a specific code, not just "yes"
- **Post-write review** — after every change, the AI checks for mistakes
- **Token isolation** — your HA token stays on the server, never on your machine
- **Encrypted auth** — client credentials stay in the OS credential store, not config files
- **Your network, your data** — no cloud dependency, no tracking (your AI client's own cloud usage is separate)

## 🖥️ Supported AI Clients

| Client | Type |
|--------|------|
| [Claude Desktop](https://claude.com/download) (Code tab) | Desktop app |
| [Claude Code](https://github.com/anthropics/claude-code) | Terminal |
| [Codex CLI](https://github.com/openai/codex) | Terminal |
| [OpenCode](https://github.com/nicepkg/OpenCode) | Terminal |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Terminal |

Current validation matrix:
- macOS: Claude, Codex, OpenCode, Gemini
- Linux: installer/update/uninstall path is covered by build + CI smoke, but this release is not yet fully live-validated on a real Linux machine
- Windows: Claude and Gemini are validated. Codex and OpenCode lanes exist, but are still experimental

> **Not a terminal person?** Claude Desktop gives you the same thing in a regular app window. Run the installer, select "Claude Code" (same integration path), open the **Code** tab, pick a workspace folder, and start.

## 🤝 Contributing

HA NOVA is early. Good time to help shape it.

- **Write a skill** — just a markdown file, no code
- **Test on your setup** — find what works, report what doesn't
- **Tackle an [open issue](https://github.com/markusleben/ha-nova/issues)**

→ [CONTRIBUTING.md](CONTRIBUTING.md) for details

## 📖 The Story Behind It

I spent over a year building an MCP server for Home Assistant. Hundreds of tool definitions, thousands of lines of code. Kept polishing, kept adding features, never releasing. By the time I looked up, others had shipped theirs while mine was still sitting on my machine.

**[Here's an early demo](https://youtu.be/ylak867RkzM)** from that era.

Then I realized the whole approach was wrong. Instead of encoding everything into server code, I could just write it down. Plain text that the AI reads directly. I scrapped everything and started fresh.

HA NOVA is the result.

## 📁 Project Structure

```
cli/         Go local runtime (setup, doctor, update, uninstall, relay)
nova/        Relay app (runs on your HA server)
skills/      AI skills (markdown files)
scripts/     Setup, deploy, diagnostics
tests/       Test suite
```

## 📄 License

[MIT](LICENSE)

## 🙏 Acknowledgments

Some Home Assistant safety-rule ideas were inspired by [HALMark](https://github.com/nathan-curtis/HALMark) by Nathan Curtis.

Automation best-practice patterns, helper selection guidance, and Zigbee device-control patterns were adapted from [homeassistant-ai/skills](https://github.com/homeassistant-ai/skills) by Sergey Kadentsev ([@sergeykad](https://github.com/sergeykad)) and Julien Lapointe ([@julienld](https://github.com/julienld)).
