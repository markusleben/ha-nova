<p align="center">
  <img src="assets/banner.svg" alt="HA NOVA — Talk to your smart home with AI" width="700">
</p>

<p align="center">
  <a href="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml"><img src="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/markusleben/ha-nova/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/node-%3E%3D20-brightgreen" alt="Node >= 20">
  <img src="https://img.shields.io/badge/platform-macOS-lightgrey" alt="macOS">
</p>

<p align="center">
  <b>Talk to your smart home. In plain language. From your terminal.</b>
</p>

---

## 💬 What Can You Do?

| You say | What happens |
|---------|-------------|
| *"Turn off the living room lights"* | Calls `light.turn_off` via relay |
| *"List my automations"* | Reads automation registry |
| *"Create an automation that turns on the porch light at sunset"* | Builds config, previews, asks confirmation, applies |
| *"Why didn't my motion automation trigger last night?"* | Fetches traces, analyzes trigger/condition/action nodes |
| *"Show me all sensors in the bedroom"* | Discovers entities by area with device fallback |
| *"Set the thermostat to 21°C"* | Calls `climate.set_temperature` with verification |

## 🧠 How It Works

Most Home Assistant AI integrations bake domain logic into server code — tool definitions, entity validation, config normalization. Thousands of lines of it. HA NOVA flips this: **all intelligence lives in Markdown skills** that your AI client reads directly. The relay is pure infrastructure. Want to update a capability? Edit a text file.

```
┌─────────────────┐        ┌──────────────────────┐        ┌──────────────────┐
│   AI Client     │        │   HA NOVA Relay       │        │  Home Assistant  │
│                 │  HTTP   │   (HA App)            │  WS    │                  │
│  Claude Code,   │───────▶│  Runs on HA host      │═══════▶│  WebSocket API   │
│  Codex, Gemini  │        │  ~1.5K LOC            │persist.│  REST API        │
│                 │        │  Zero business logic  │  conn  │                  │
└─────────────────┘        └──────────────────────┘        └──────────────────┘
        │
        │ reads
        ▼
┌─────────────────┐
│   LLM Skills    │
│   (Markdown)    │
│                 │
│  7 skill files  │
│  teach your AI  │
│  how to operate │
│  Home Assistant  │
└─────────────────┘
```

HA NOVA is **not** an MCP server. It's two things:

- **Relay** — A tiny HA App (~1.5K LOC) that proxies WebSocket and REST calls. No business logic. No tool definitions. Just a secure bridge.
- **Skills** — Markdown files that teach your AI client how to operate Home Assistant. Your AI reads them, understands the API, and acts.

## 🔑 Why a Relay?

Every HA AI integration needs a server-side process — there's no way around it. HA NOVA's relay is deliberately minimal, but it exists for three concrete reasons.

### 🔒 Token Isolation

Two tokens, two trust zones:

- **Relay token** (client ↔ relay) — stored in macOS Keychain on your machine
- **HA Long-Lived Access Token** (relay ↔ HA) — stored in App config on the HA host, never leaves

Without a relay, the LLAT would live in your shell environment — exposed to prompts, logs, and history. With the relay, revoke a client by changing one token. HA stays untouched.

### ⚡ Persistent Connection

HA's WebSocket API requires a multi-step auth handshake per connection: connect → `auth_required` → authenticate → `auth_ok` → command. The relay maintains one persistent connection and exposes it as a simple `POST /ws`. Your AI skips the handshake on every call (~500ms–2s saved per request).

Direct WebSocket calls from a shell are technically possible (curl 8.11+, wscat, Node scripts), but each would repeat the full handshake — and expose the LLAT in the process.

### 🔌 Extensible Platform

The relay runs on the HA host with local filesystem and network access. This enables features beyond HA's standard API:

- Auto-backup before config changes (planned)
- Filesystem access for YAML-only configs (planned)
- Real-time state update streaming (planned)

New endpoint = one handler file + tests + one line of registration.

### 📐 The Boundary

The relay provides infrastructure — transport, file access, backups. It does not contain business logic, tool definitions, or domain validation. That's the skills' job.

**Relay = capabilities. Skills = intelligence.**

## 🚀 Quick Start

> **Requirements:** macOS, Node.js >= 20, Home Assistant OS or Supervised

**One-liner install:**
```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

**Or with npx (if you already have the repo):**
```bash
npx ha-nova setup
```

The wizard asks which AI client you use, then handles everything — relay installation, token configuration, skill setup.

**Non-interactive setup** (for automation / WebUI):
```bash
ha-nova setup claude --host=<your-ha-ip> --token=<relay-auth-token>
```

## 🤖 Supported AI Clients

| Client | Status |
|--------|--------|
| [Claude Code](https://github.com/anthropics/claude-code) | ✅ Supported |
| [Codex CLI](https://github.com/openai/codex) | ✅ Supported |
| [OpenCode](https://github.com/nicepkg/OpenCode) | ✅ Supported |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | ✅ Supported |

## 📖 Skills

All skills use the `ha-nova:<skill>` naming convention:

| Skill | What it does |
|-------|-------------|
| **ha-nova:write** | Create, update, delete automations and scripts — 3-phase safety flow (Resolve → Preview → Apply) |
| **ha-nova:read** | List configs, inspect automations/scripts, debug with trace analysis |
| **ha-nova:entity-discovery** | Search entities by name, domain, room, or area |
| **ha-nova:service-call** | Direct device control — lights, climate, covers, switches, and more |
| **ha-nova:review** | Analyze automations for best-practice violations and conflicts |
| **ha-nova:onboarding** | Guided setup diagnostics and troubleshooting |

## 🛡️ Safety

Every write goes through three phases:

1. **Resolve** — Read-only agent finds entities, checks config, scores candidates
2. **Preview** — Shows you exactly what will change, asks for confirmation
3. **Apply** — Writes config, reloads, reads back to verify

On top of that:
- 🔐 Delete requires tokenized confirmation (`confirm:tok-...`)
- 🧪 Best-practice gate for complex automations
- 🔑 All auth via macOS Keychain — tokens never appear in prompts
- 🚫 Agents restricted to relay API only — no direct HA access
- 📡 No cloud, no telemetry, fully local

## 🤝 For Contributors

### The Core Idea

Traditional HA integrations put domain knowledge in server code — which services each entity supports, how to normalize configs, how to fuzzy-match names. HA NOVA doesn't need any of that. The LLM already knows these things. Skills teach it the specifics; the relay just moves data.

**Before adding logic to the relay, ask: could the LLM do this?**

Entity fuzzy search? LLMs handle typos, abbreviations, and multiple languages natively — no matching algorithm needed. Domain knowledge? The LLM knows that lights have brightness and color. Config validation? HA validates on write; the skill teaches the AI to handle errors.

The relay grows by adding **infrastructure** — new ways to move data, access files, interact with platform services. Skills grow by adding **intelligence** — new ways for the AI to reason about Home Assistant.

### Where does my feature go?

| Ask yourself | → |
|---|---|
| Does it move, filter, or store data? | Relay |
| Does it interpret data or make decisions? | Skill |
| Does it need filesystem/network access on the HA host? | Relay |
| Does it teach the AI how to do something? | Skill |
| Could an LLM do this given the raw data? | Skill |
| Does it need to work without an LLM? | Relay |

> **🧪 The litmus test:** if you removed the endpoint and gave the LLM the raw data instead, would the feature still work? If yes — it's a skill.

**Relay examples:** backup files before config writes, proxy a new HA API, stream state changes, cache entity registries.

**Skill examples:** suggest energy-saving automations, detect conflicting triggers, build YAML from natural language, analyze traces for debugging.

### Two ways in

🔧 **Relay endpoints** (TypeScript) — ~1,500 LOC, no framework, pure functions. Handler + tests + one line of registration.

📝 **Skills** (Markdown) — No TypeScript, no compilation, no deploy. Edit a text file. The AI gains a new capability.

→ [CONTRIBUTING.md](CONTRIBUTING.md) for setup, guidelines, and architecture rules.

## 🏗️ Architecture

```
src/                    Relay server (TypeScript, ~1.5K LOC)
skills/                 LLM skills (flat layout — context skill + 6 sub-skills)
  ha-nova/              Context skill (auto-loaded) + reference docs + agent templates
scripts/onboarding/     Setup wizard and diagnostics
.claude-plugin/         Claude Code plugin manifest
```

## 🔧 Troubleshooting

```bash
npx ha-nova doctor
```

## Contributing

→ [CONTRIBUTING.md](CONTRIBUTING.md)

## License

[MIT](LICENSE)
