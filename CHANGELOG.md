# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/) and [Semantic Versioning](https://semver.org/).

## [0.1.3] - 2026-03-05

### Added
- **curl|bash installer** — `curl -fsSL .../install.sh | bash` one-liner setup
- **Non-interactive setup** — `--host` and `--token` CLI flags
- **`ha-nova update`** — Subcommand for git-based updates
- **App documentation** — `DOCS.md` for HA App UI Documentation tab
- **App icons & logos** — PNG assets (icon, logo, @2x variants)
- **Translations** — `translations/en.yaml` with Config UI labels
- **Social preview** — Redesigned horizontal hero layout
- **49 new tests** — 141→190 (fixture-based, 11 onboarding scenarios)

### Changed
- **Deploy script** — Always clean deploy, options save/restore on reinstall, translations sync
- **Config parsing** — Secure key-value parsing instead of `source` (security)

### Fixed
- **Session-start hook** — `\b`/`\f` JSON escaping
- **Deploy: Options** — Base64 encoding for safe option transfer via SSH
- **Deploy: Translations** — Removed duplicate config.yaml in app/ (root cause)
- **CLI: `ha-nova update`** — Path calculation corrected (3 instead of 2 levels)

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
- **Claude Code, Codex CLI, OpenCode, and Gemini CLI** support via managed skill installation
