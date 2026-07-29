# ha-nova

## What Is This?

ha-nova is a Home Assistant AI integration. It replaces an 88,000-line MCP server with a lean API Relay plus LLM Skills.

- **Relay:** Runs as a Home Assistant App, pure transport layer (WS proxy, REST forwarding, token storage)
- **Skills:** Markdown files that instruct LLMs on Home Assistant control (best practices, workflows, API knowledge)
- **Relay path:** Skills use HA NOVA Relay for Home Assistant REST/WS transport; business logic stays in Markdown skills, not in the Relay

This file is internal product context only.
Public install, support, and platform truth lives in `README.md`.
Contributor workflow lives in `CONTRIBUTING.md`.
Release/runbook truth lives in `docs/releasing.md`.
Active doc ownership lives in `docs/reference/documentation-governance.md`.

## Current Phase

**Post-masterplan-2026 program** — the active roadmap is `docs/work/masterplan-2026-h2.md`.

Shipped and current:
1. Relay: `GET /health`, `POST /ws`, `POST /core`, `POST /files` (opt-in, default off); App + standalone container from one codebase
2. Go CLI: install, setup wizard, doctor, update (incl. guided relay update), uninstall (incl. guided server-side teardown), relay proxy
3. Context skill `ha-nova` plus 29 task skills, flat under `skills/` — the dispatch table in `skills/ha-nova/SKILL.md` is the authoritative inventory
4. Shared references under `skills/ha-nova/` (relay API contract, output rules, write safety, batch safety, schemas, agent templates)

HA NOVA also contains a release-gated, opt-in Home Assistant Cloud remote
transport Beta for Home Assistant OS/Supervised. Release metadata enables it
only for exact candidates that pass the real validation matrix:

1. The wizard offers `Local + Home Assistant Cloud`, `Local only`, and
   `Home Assistant Cloud only`. Service and headless setup stay local-only.
2. Cloud setup uses Home Assistant OAuth, Supervisor WebSocket APIs, and a
   process-local Ingress session. HA NOVA operates no public tunnel or broker.
3. OAuth refresh tokens use a dedicated native OS credential store. Normal
   Relay calls use no-UI reads and fail fast when secure storage is locked.
4. Ingress functional calls require both the Home Assistant ingress user and a
   device credential bound to that user and the persistent Relay instance.
5. `automatic` routing prefers the pinned local device transport and selects
   Cloud only after a bounded authenticated preflight ends in a pure network
   failure. Security, identity, protocol, or authorization errors never fall
   back.

Publication stays fail-closed unless the exact candidate passes the real Nabu
Casa parity, native-keyring, identity/role, lifecycle, and stress gates in
`docs/work/2026-07-25-home-assistant-cloud-remote-spec.md`.

## Tech Stack

- **App / Relay:** TypeScript, Node.js >= 20, no HTTP framework
- **Local runtime:** Go 1.24+ CLI for install, setup, doctor, update, uninstall, relay
- **Dependencies:** home-assistant-js-websocket
- **Skills:** Markdown files
- **Tests:** Vitest + Go test

## Conventions

- Relay code stays intentionally dumb. No business logic, no domain validation, no caching.
- Intelligence belongs in Skills, not in the server.
- Language: skills and skill-like source docs stay English-only.
- Commits: English, Conventional Commits.
- Keep files under ~400 LOC when practical.
