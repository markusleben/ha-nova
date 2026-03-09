<p align="center">
  <img src="assets/social-preview.png" alt="HA NOVA">
</p>

<p align="center">
  <a href="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml"><img src="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/markusleben/ha-nova/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/node-%3E%3D20-brightgreen" alt="Node >= 20">
  <img src="https://img.shields.io/badge/platform-macOS-lightgrey" alt="macOS">
</p>

<p align="center">
  <b>Your AI meets Home Assistant.</b><br>
  <sub>AI that reads before it writes.</sub>
</p>

---

## What is HA NOVA?

HA NOVA connects your AI to Home Assistant. You talk in plain language, like *"create an automation that turns on the porch light at sunset"*, and your AI handles it safely, with a preview before any change is made.

How? A small app on your HA server acts as a secure bridge (we call it the *relay*, [more on how it works below](#how-it-works)), while plain text files called *skills* teach your AI how Home Assistant works. The setup wizard handles everything. You don't need to understand the internals to get started.

Works with **Claude Desktop, Claude Code, Codex CLI, OpenCode, and Gemini CLI**.

> **Early Access:** The core works well, but you might hit rough edges. macOS only for now (Linux/Windows coming). Back up your configs before letting AI make changes. Found a problem? [Open an issue](https://github.com/markusleben/ha-nova/issues).

## What Can You Do?

### Automations & Scripts: with a safety net

*"Create an automation that turns on the porch light at sunset"*
*"Write a script that announces 'dinner is ready' on all speakers"*

Most AI integrations just fire an API call and hope for the best. HA NOVA follows a careful process for every automation and script change:

1. **Research:** Looks up your devices, checks existing configs, resolves the right entities
2. **Preview:** Shows you the exact config that will be written. Nothing happens until you say OK
3. **Apply & Verify:** Writes it, reads it back to make sure it actually stuck
4. **Review:** Checks the result against 40+ rules for common mistakes, conflicts, and reliability issues

Deleting anything requires a confirmation code, not just "yes".

### Everything else you'd expect

| You say | What happens |
|---------|-------------|
| *"Why didn't my motion automation trigger last night?"* | Analyzes the actual trace logs, explains what went wrong |
| *"Check my automations for problems"* | Audits for 40+ common mistakes and conflicts |
| *"Turn off the living room lights"* | Controls it, confirms the new state |
| *"Show me all sensors in the bedroom"* | Finds entities by room, area, or name |
| *"Set the thermostat to 21°C"* | Sets it and confirms |
| *"Create a counter helper for my coffee intake"* | Creates the helper, shows the result |

---

## What Makes HA NOVA Different

### The relay lives on your HA server. That's the real power.

The relay isn't just a bridge for API calls. It runs directly on your Home Assistant machine with local access to the config directory. Today it proxies WebSocket and REST requests securely, so your AI can read, write, and control anything the HA API exposes.

But the architecture enables much more. Because the relay sits on the HA host, it can extend what the API offers. On the roadmap:

- **Automation versioning:** automatic backup before every change, stored locally on the HA host
- **Config management:** manage template sensors and YAML-only settings that HA's API doesn't expose
- **Local diagnostics:** filesystem-level health checks no remote tool can run

The goal: your AI gets capabilities that even the HA UI doesn't have. All powered by a relay that's ~1,500 lines of code.

Your HA access token stays on the server. Auth on your machine is encrypted in macOS Keychain, not in config files, not in URLs, not in logs.

### Skills are modular. Your AI loads only what it needs.

MCP servers register all their tools at once, whether you need them or not. A typical HA MCP server loads 50-100 tools into every conversation. That eats context and slows your AI down.

HA NOVA works differently. Each skill is a self-contained markdown file. Your AI loads only the skill it needs for the current task: write, read, review, or control. The rest stays out of the way.

Want to teach your AI something new about HA? Write a markdown file. No code, no compilation, no deployment, no schema definitions. Anyone can extend HA NOVA without touching a line of server code.

---

## How It Works

<p align="center">
  <img src="assets/how-it-works.png" alt="How HA NOVA works: Your AI Client talks to the NOVA Relay on your HA server, which connects to Home Assistant. Skills teach the AI what to do.">
</p>

**The Relay** is a small app that runs directly on your Home Assistant server. It forwards requests securely while keeping your access token safely on the server. Only ~1,500 lines of code, no business logic. That keeps it stable: HA updates rarely affect the relay.

**The Skills** are plain text files on your machine. They teach your AI how Home Assistant works: what to call, in what order, what to check. No tool schemas, no compilation. Updating a skill means editing a text file.

### How does this compare to other approaches?

| | MCP Servers | Skills-only projects | HA NOVA |
|---|---|---|---|
| **HA connectivity** | Tools call HA API directly | No API access (SSH/scp) | Relay on HA server (API + local access) |
| **Intelligence** | Encoded in tool definitions | In prompt context | In modular skills |
| **Context efficiency** | All tools loaded always | Single file loaded | Only relevant skill loaded per task |
| **Extending** | Write code, deploy, update schemas | Edit one markdown file | Edit one markdown file |
| **Safety flow** | Tool-level (per call) | Manual verification | 4-phase: research → preview → apply → review |
| **Multi-client** | Varies (1-15 clients) | Usually 1 client | 5 clients (Claude Desktop, Claude Code, Codex, OpenCode, Gemini) |
| **Server maintenance** | More code to maintain per HA update | None | Minimal (relay is just transport) |

**Why not an MCP server?** MCP servers encode domain knowledge in code. Every new feature means more tools, more code, more maintenance. With skills, the AI reads plain text and adapts. No compilation, no deploy.

**Can't I just call the HA API directly?** You can! But you'd miss the safety flow (preview → confirm → verify → review), the 40+ config checks, the conflict detection, and the secure token isolation the relay provides.

---

## The Story Behind It

I spent over a year building an MCP server for Home Assistant. Hundreds of tool definitions, thousands of lines of code. I kept polishing, never releasing. By the time I looked up, others had shipped theirs while mine was still on my machine.

**[Here's an early demo](https://youtu.be/ylak867RkzM)** from that time.

Then I realized the approach was wrong. Instead of encoding domain knowledge into a server, I could write it as plain text that the AI reads directly. I scrapped everything and started fresh. HA NOVA is the result.

---

## Quick Start

> **You need:** macOS, Node.js 20+, Home Assistant OS or Supervised

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

The wizard handles everything: relay, tokens, skills. Just pick your AI client.

**Already have the repo?**
```bash
npx ha-nova setup
```

## Supported AI Clients

| Client | Type |
|--------|------|
| [Claude Desktop](https://claude.com/download) (Code tab) | Desktop app |
| [Claude Code](https://github.com/anthropics/claude-code) | Terminal |
| [Codex CLI](https://github.com/openai/codex) | Terminal |
| [OpenCode](https://github.com/nicepkg/OpenCode) | Terminal |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Terminal |

> **Don't like terminals?** Claude Desktop gives you the same capabilities in a graphical chat interface. Run the install command, select "Claude Code" (they share the same config), then open Claude Desktop and switch to the Code tab. Pick any folder on your Mac as workspace and start talking.

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

Want to add a new capability? Write a markdown file. No code required. → [CONTRIBUTING.md](CONTRIBUTING.md)

## Safety

- **Preview first:** every change is shown before it happens
- **Confirmation codes:** deletes require a specific code, not just "yes"
- **Post-write review:** after every change, the AI checks for mistakes and conflicts
- **Token isolation:** your HA token stays on the HA server, never on your machine
- **Encrypted auth:** client-side credentials in macOS Keychain, not in config files
- **Fully local:** no cloud, no tracking, everything on your network

## Troubleshooting

```bash
npx ha-nova doctor
```

## Contributing

HA NOVA is early. The best time to help shape it.

- **Write a skill:** just a markdown file, no code needed
- **Test on your setup:** find what works, report what doesn't
- **Tackle an [open issue](https://github.com/markusleben/ha-nova/issues)**

→ [CONTRIBUTING.md](CONTRIBUTING.md)

## Project Structure

```
nova/        Relay app (runs on your HA server)
skills/      AI skills (markdown files)
scripts/     Setup, deploy, diagnostics
tests/       Test suite
```

## License

[MIT](LICENSE)
