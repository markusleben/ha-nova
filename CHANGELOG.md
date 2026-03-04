# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/) and [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-03-04

First public release.

### Added
- **Relay proxy** — WebSocket + REST proxy as HA App (~2K LOC, zero business logic)
- **6 LLM skills** — ha-nova (router), write, read, entity-discovery, service-call, onboarding
- **3-phase safe write flow** — Resolve (read-only) → Preview + Confirm → Apply + Verify
- **Trace debugging** — `trace/list` and `trace/get` for automation and script diagnostics
- **Service calls** — Direct device control with state verification
- **Entity discovery** — Search by name, domain, room, or area (with device-area fallback)
- **Setup wizard** — `npx ha-nova setup` with smart resume, prerequisites check, skill installation
- **Diagnostics** — `npx ha-nova doctor` for connectivity and auth troubleshooting
- **Payload schemas** — Reference examples for automation and script construction
- **Best-practice gate** — Blocks complex writes when HA best-practice snapshot is stale
- **Tokenized delete confirmation** — Destructive operations require `confirm:tok-...` tokens
- **macOS Keychain auth** — Tokens stored securely, never exposed in prompts
- **139 tests** — Contract tests, security tests, onboarding tests, E2E harness
- **CI pipeline** — TypeScript typecheck, Vitest, CodeQL, dependency review
- **Claude Code, Codex CLI, and OpenCode** support via managed skill installation
