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
  - one-time skill install: `npm run install:codex-skill`
  - daily usage: regular `codex` startup (no custom launcher required)
- Claude Code:
  - one-link guided flow via raw instructions
  - `Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.claude/INSTALL.md`
  - one-time local skill install: `npm run install:claude-skill`
  - same onboarding + verify flow as Codex (`scripts/onboarding/macos-onboarding.sh`)
  - marketplace packaging not published yet
- Claude Desktop:
  - planned after Codex/Claude Code flow is stable
  - no public package yet

## Legacy Project

The legacy project is at `/Users/markus/Daten/Development/Privat/ha-mcp-addon`.
It is a TypeScript MCP server with 18 managers, 200+ actions, and 70 best-practice rules.
Code from it can be reused for Relay components (REST/WS clients, backup manager, auth).

## Documentation

- `docs/MIGRATION-WORKPAPER.md` — full migration plan across 4 phases
- `docs/reference/ha-api-matrix.md` — which HA operations require REST vs WS vs filesystem
- `docs/reference/manager-dependency-matrix.md` — manager-to-transport dependency map
- `docs/reference/bridge-architecture.md` — relay endpoint specification
- `docs/reference/skill-architecture.md` — skill hierarchy and bootstrap design
- `docs/reference/old-project-inventory.md` — inventory of the legacy MCP server

## Current Phase

**Phase 1: Infrastructure + skill-system consolidation**

Current deliverables:
1. Relay MVP: `POST /ws` + `GET /health`
2. Router skill: `ha-nova`
3. Consolidated write skill: `ha-nova-write`
4. Consolidated read skill: `ha-nova-read`
5. Entity discovery skill: `ha-nova-entity-discovery`
6. Onboarding/diagnostics skill: `ha-nova-onboarding`
7. Shared references under `skills/ha-nova/` (`relay-api.md`, `best-practices.md`, `agents/*`)

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
