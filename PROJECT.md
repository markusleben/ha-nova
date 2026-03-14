# ha-nova

## What Is This?

**ha-nova** is the next-generation Home Assistant AI integration.
It replaces an 88,000-line MCP server with a lean API Relay plus LLM Skills.

- **Relay:** Runs as a Home Assistant App, ~2,000-3,000 LOC, pure transport layer (WS proxy, filesystem, backups)
- **Skills:** Markdown files that instruct LLMs on Home Assistant control (best practices, workflows, API knowledge)
- **Direct path:** ~60% of operations go directly to the HA REST API without relay hops

## Client Installation Paths

Primary onboarding target is non-technical end users.
Official product entrypoints are OS bootstrapper + `ha-nova setup`.
Client-specific install docs are convenience wrappers around that flow.

- Codex CLI:
  - one-link guided flow via raw instructions
  - `Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md`
  - `ha-nova setup codex` installs skills into `~/.agents/skills/ha-nova`
  - daily usage: regular `codex` startup
- Claude Code:
  - one-link guided flow via raw instructions
  - `Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.claude/INSTALL.md`
  - `ha-nova setup claude` registers the local bundle as a Claude plugin
- Claude Desktop:
  - planned after Codex/Claude Code flow is stable
  - no public package yet

## Documentation

- `docs/reference/ha-api-matrix.md` — which HA operations require REST vs WS vs filesystem
- `docs/reference/bridge-architecture.md` — relay endpoint specification
- `docs/reference/skill-architecture.md` — skill hierarchy and bootstrap design

## Current Phase

**Phase 1: Infrastructure + skill-system consolidation**

Current deliverables:
1. Relay MVP: `GET /health`, `POST /ws`, `POST /core`
2. Context skill: `ha-nova` (auto-loaded via SessionStart hook; sub-skills discovered independently)
3. Sub-skills (flat under `skills/`): write, read, helper, entity-discovery, service-call, review, guide, onboarding
4. Shared references under `skills/ha-nova/` (`relay-api.md`, `best-practices.md`, `payload-schemas.md`, `helper-schemas.md`, `template-guidelines.md`, `safe-refactoring.md`, `update-guide.md`, `agents/*`)

## Tech Stack

- **App / Relay:** TypeScript, Node.js >=20, no HTTP framework
- **Local runtime:** Go 1.24+ CLI for install/setup/doctor/update/uninstall/relay
- **Dependencies:** ws, yaml, axios, home-assistant-js-websocket
- **Skills:** Markdown files
- **Tests:** Vitest + Go test

## Conventions

- Relay code must stay intentionally dumb: no business logic, no domain validation, no caching.
- Intelligence belongs in Skills, not in the server.
- Language policy: English-only across the whole project.
- Commit messages: English, Conventional Commits.
- Keep files under ~400 LOC when practical.
