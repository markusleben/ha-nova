# ha-nova

## What Is This?

ha-nova is a Home Assistant AI integration. It replaces an 88,000-line MCP server with a lean API Relay plus LLM Skills.

- **Relay:** Runs as a Home Assistant App, ~1,500 LOC, pure transport layer (WS proxy, REST forwarding, token storage)
- **Skills:** Markdown files that instruct LLMs on Home Assistant control (best practices, workflows, API knowledge)
- **Direct path:** Many operations go directly to the HA REST API without relay hops

This file is internal product context only.
Public install, support, and platform truth lives in `README.md`.
Contributor workflow lives in `CONTRIBUTING.md`.
Release/runbook truth lives in `docs/releasing.md`.
Active doc ownership lives in `docs/reference/documentation-governance.md`.

## Current Phase

**Phase 1: Infrastructure + skill-system consolidation**

Deliverables:
1. Relay MVP: `GET /health`, `POST /ws`, `POST /core`
2. Context skill: `ha-nova` (auto-loaded via SessionStart hook; sub-skills discovered independently)
3. Sub-skills (flat under `skills/`): write, read, review, dashboard, organize, history, helper, entity-discovery, service-call, fallback, onboarding
4. Shared references under `skills/ha-nova/` (`relay-api.md`, `best-practices.md`, `payload-schemas.md`, `helper-schemas.md`, `template-guidelines.md`, `safe-refactoring.md`, `automation-patterns.md`, `update-guide.md`, `agents.md`)

## Tech Stack

- **App / Relay:** TypeScript, Node.js >= 20, no HTTP framework
- **Local runtime:** Go 1.24+ CLI for install, setup, doctor, update, uninstall, relay
- **Dependencies:** ws, yaml, axios, home-assistant-js-websocket
- **Skills:** Markdown files
- **Tests:** Vitest + Go test

## Conventions

- Relay code stays intentionally dumb. No business logic, no domain validation, no caching.
- Intelligence belongs in Skills, not in the server.
- Language: skills and skill-like source docs stay English-only.
- Commits: English, Conventional Commits.
- Keep files under ~400 LOC when practical.
