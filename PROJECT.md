# ha-nova

## What Is This?

ha-nova is a Home Assistant AI integration: a lean API Relay plus LLM Skills, so AI apps can work with a home safely.

- **Relay:** Runs as a Home Assistant App, pure transport layer (WS proxy, REST forwarding, token storage)
- **Skills:** Markdown files that instruct LLMs on Home Assistant control (best practices, workflows, API knowledge)
- **Relay path:** Skills use HA NOVA Relay for Home Assistant REST/WS transport; business logic stays in Markdown skills, not in the Relay

This file is internal product context only.
Public install, support, and platform truth lives in `README.md`.
Contributor workflow lives in `CONTRIBUTING.md`.
Release/runbook truth lives in `docs/releasing.md`.
Active doc ownership lives in `docs/reference/documentation-governance.md`.

## Current Phase

**Post-masterplan-2026 program** — backlog sequencing 2026-08 shipped in full as v0.23.0 (archived); the active roadmap is the 2026-08 skill-audit issue set #513–#522, sequenced in `docs/work/2026-08-09-skill-audit.md` (the report landed with the audit train).

Shipped and current:
1. Relay: `GET /health`, `POST /ws`, `POST /core`, `POST /files` (opt-in, default off), `POST /backups` (config-snapshot store), plus the human/machine surfaces `GET /home` and the pairing/device-auth endpoints; App + standalone container from one codebase
2. Go CLI: install, setup wizard, doctor, update (incl. guided relay update), uninstall (incl. guided server-side teardown), relay proxy
3. Context skill `ha-nova` plus 34 task skills, flat under `skills/` — the dispatch table in `skills/ha-nova/SKILL.md` is the authoritative inventory
4. Shared references under `skills/ha-nova/` (relay API contract, output rules, write safety, batch safety, schemas)
5. Setup page in the browser (owner sign-in pairing, the six-digit code as fallback; installs the NOVA App after sign-in on HA OS/Supervised); confirmations ask in a window and on the linked phone together, a chat code is the last fallback
6. Chat apps: the App runs the chat-app bridge (`ha-nova serve` plus the small `ha_nova` integration), so Claude (web, desktop, phone) and ChatGPT (web) reach a home through Home Assistant Link with remote access (a subscription from the Home Assistant team) or its own HTTPS address, with no computer running; contract in `docs/reference/bridge-architecture.md` ("Chat apps")
7. Hosted demo house (`ha-nova try`, "Add to Claude"): `docs/reference/hosted-demo.md`

HA NOVA also contains an opt-in Home Assistant Link (formerly Home Assistant
Cloud) remote transport Beta for Home Assistant OS/Supervised. Release metadata enables it per platform, and a
release binary turns it on only with its signed bundle provenance:

1. The wizard offers `Local + Home Assistant Link`, `Local only`, and
   `Home Assistant Link only`. Service and headless setup stay local-only.
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

Each release runs the three-OS provenance smokes and the per-release Cloud
checklist in `docs/releasing.md`; the contracts live in
`docs/work/2026-07-25-home-assistant-cloud-remote-spec.md`.

## Tech Stack

- **App / Relay:** TypeScript, Node.js >= 22, no HTTP framework
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
