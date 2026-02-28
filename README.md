# ha-nova

Home Assistant AI integration with a lean Relay and markdown-based Skills.

## What It Is

`ha-nova` replaces a large MCP-heavy architecture with a simpler split:
- Relay (`/health`, `/ws`) as transport only.
- Skills as the intelligence and workflow layer.

Core rule:
- Relay stays dumb.
- Skills stay smart.

## Current MVP Status

Already available:
- App + Relay transport (`/health`, `/ws`)
- macOS onboarding with Keychain-backed relay auth
- Read/discovery flows via Relay (`/ws` first path)
- one-link install flows for Codex and Claude Code
- contributor deployment/e2e tooling

Not yet exposed in end-user flow:
- write control via Relay
- automation CRUD via Relay
- public marketplace packaging
- Windows onboarding

## Quick Start (User)

Codex:
- Follow `.codex/INSTALL.md`
- Install local skill once: `npm run install:codex-skill`

Claude Code:
- Follow `.claude/INSTALL.md`
- Install local skill once: `npm run install:claude-skill`

## Quick Start (Contributor)

Requirements:
- Node.js `>=20`
- npm

Setup:
```bash
npm ci
npm run typecheck
npm test
```

Useful commands:
```bash
npm run deploy:app:fast
npm run deploy:app:clean
npm run smoke:app:e2e
npm run e2e:skill:codex
npm run e2e:skill:codex:scenarios
```

## Repository Layout

- `src/` relay/runtime code
- `skills/` active skill markdown files
- `scripts/` deploy, onboarding, e2e, smoke tooling
- `tests/` contract/unit/integration tests
- `docs/` plans, references, contributor docs
- `app/` Home Assistant App packaging assets

## Contributing and Security

- Contributing guide: `CONTRIBUTING.md`
- Code of conduct: `CODE_OF_CONDUCT.md`
- Security policy: `SECURITY.md`
- Support channels: `SUPPORT.md`
- Changelog format: `CHANGELOG.md`
