# ha-nova

## What Is This?

ha-nova is a Home Assistant AI integration. It replaces an 88,000-line MCP server with a lean API Relay plus LLM Skills.

- **Relay:** Runs as a Home Assistant App. Around 1,500 lines of code, pure transport layer (health, WS proxy, core relay).
- **Skills:** Markdown files that teach LLMs how to control Home Assistant. Best practices, workflows, API knowledge. No code.
- **Direct path:** About 60% of operations go straight to the HA REST API without relay hops.

## Client Installation Paths

Primary onboarding target is non-technical end users.
Official entrypoints are the OS bootstrapper and `ha-nova setup`.
Client-specific install docs are convenience wrappers around that flow.

Current release support matrix:
- macOS: Claude, Codex, OpenCode, Gemini
- Linux: installer and runtime path built and CI-smoked. Full release validation still needs a real Secret Service-backed Linux run.
- Windows: platform installer and runtime support is live. Claude and Gemini are smoke-validated. Codex and OpenCode lanes remain experimental.
- Windows bundle ships `amd64` only. ARM64 uses x64 emulation.
- Public Windows entrypoint is `install.ps1` until the generated `winget` manifest is published and proven.

Client setup commands:
- **Codex CLI:** `ha-nova setup codex` installs skills into `~/.agents/skills/ha-nova`
- **Claude Code / Desktop:** `ha-nova setup claude` installs or repairs the Claude plugin integration
- **OpenCode:** `ha-nova setup opencode` installs or repairs the OpenCode skill integration
- **Gemini CLI:** `ha-nova setup gemini` installs or repairs the Gemini skill integration

Each client also supports a one-link guided flow via raw instructions from the repo.

## Documentation

- `docs/reference/ha-api-matrix.md` — which HA operations need REST vs WS vs filesystem
- `docs/reference/bridge-architecture.md` — relay endpoint spec
- `docs/reference/skill-architecture.md` — skill hierarchy and bootstrap design

## Current Phase

**Phase 1: Infrastructure + skill-system consolidation**

Deliverables:
1. Relay MVP: `GET /health`, `POST /ws`, `POST /core`
2. Context skill: `ha-nova` (auto-loaded via SessionStart hook; sub-skills discovered independently)
3. Sub-skills (flat under `skills/`): write, read, helper, entity-discovery, service-call, review, fallback, onboarding
4. Shared references under `skills/ha-nova/` (relay-api, best-practices, payload-schemas, helper-schemas, template-guidelines, safe-refactoring, automation-patterns, update-guide, agents)

## Tech Stack

- **App / Relay:** TypeScript, Node.js >= 20, no HTTP framework
- **Local runtime:** Go 1.24+ CLI for install, setup, doctor, update, uninstall, relay
- **Dependencies:** ws, yaml, axios, home-assistant-js-websocket
- **Skills:** Markdown files
- **Tests:** Vitest + Go test

## Conventions

- Relay code stays intentionally dumb. No business logic, no domain validation, no caching.
- Intelligence belongs in Skills, not in the server.
- Language: skills and skill-like source docs are English only.
- Commits: English, Conventional Commits.
- Keep files under ~400 LOC when practical.
