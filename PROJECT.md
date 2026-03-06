# ha-nova

## What Is This?

**ha-nova** is the next-generation Home Assistant AI integration.
It replaces an 88,000-line MCP server with a lean API Relay plus LLM Skills.

- **Relay:** Runs as a Home Assistant App, ~2,000-3,000 LOC, pure transport layer (WS proxy, filesystem, backups)
- **Skills:** Markdown files that instruct LLMs on Home Assistant control (best practices, workflows, API knowledge)
- **Direct path:** ~60% of operations go directly to the HA REST API without relay hops

## Client Installation Paths

Primary onboarding target is non-technical end users.
Use a client-specific entrypoint instead of manual environment editing.

- Codex CLI:
  - one-link guided flow via raw instructions
  - `Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md`
  - skill install via symlink: `npm run install:codex-skill` (symlinks `~/.agents/skills/ha-nova` → repo)
  - daily usage: regular `codex` startup (no custom launcher required)
- Claude Code:
  - Plugin system: `claude plugin add /path/to/ha-nova` (auto-discovers `skills/ha-nova/`)
  - one-link guided flow via raw instructions
  - `Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.claude/INSTALL.md`
  - same onboarding + verify flow as Codex (`scripts/onboarding/macos-onboarding.sh`)
  - marketplace packaging not published yet
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
1. Relay MVP: `POST /ws` + `GET /health`
2. Context skill: `ha-nova` (auto-loaded via SessionStart hook; sub-skills discovered independently)
3. Sub-skills (flat under `skills/`): write, read, entity-discovery, service-call, review, onboarding
4. Shared references under `skills/ha-nova/` (`relay-api.md`, `best-practices.md`, `agents/*`)

## Tech Stack

- **Relay:** TypeScript, Node.js >=20, no HTTP framework
- **Dependencies:** ws, yaml, axios, home-assistant-js-websocket
- **Skills:** Markdown files
- **Tests:** Vitest

## Conventions

- Relay code must stay intentionally dumb: no business logic, no domain validation, no caching.
- Intelligence belongs in Skills, not in the server.
- Language policy: English-only across the whole project.
- Commit messages: English, Conventional Commits.
- Keep files under ~400 LOC when practical.
