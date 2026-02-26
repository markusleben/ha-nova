# ha-nova

## What Is This?

**ha-nova** is the next-generation Home Assistant AI integration.
It replaces an 88,000-line MCP server with a lean API Relay plus LLM Skills.

- **Relay:** Runs as a Home Assistant App, ~2,000-3,000 LOC, pure transport layer (WS proxy, filesystem, backups)
- **Skills:** Markdown files that instruct LLMs on Home Assistant control (best practices, workflows, API knowledge)
- **Direct path:** ~60% of operations go directly to the HA REST API without relay hops

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

**Phase 1a: Infrastructure + onboarding**

Deliverables:
1. Relay MVP: `POST /ws` + `GET /health` (~500 LOC)
2. Bootstrap Skill: `ha-nova.md`
3. Onboarding Skill: `ha-onboarding.md`
4. Safety Skill: `ha-safety.md`

Then Phase 1b: automation CRUD + device control.

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
