# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/) and [Semantic Versioning](https://semver.org/).

## [0.1.4] - 2026-03-07

### Added
- **Helper CRUD skill** — New `ha-nova:helper` for 9 storage-based helper types (input_boolean, input_number, input_text, input_select, input_datetime, input_button, counter, timer, schedule) via WebSocket commands
- **Helper payload schemas** — `skills/ha-nova/helper-schemas.md` with required/optional fields, types, constraints per helper type
- **H-01..H-08 review checks** — Helper-specific best-practice checks (min/max, restart guards, orphaned helpers, naming consistency)
- **Helper service patterns** — Service call reference for all 9 helper types in service-call skill
- **Multi-arch HA App builds** — `build.yaml` with correct base images for amd64 + aarch64 (Raspberry Pi)
- **Skill architecture docs** — Agent vs inline decision rule, skill section template, new-skill checklist, post-write review standard

### Changed
- **Review agent (SSOT)** — References `review/SKILL.md` instead of duplicating checks; eliminates drift risk
- **Gemini skill discovery** — Dynamic `skills/*/SKILL.md` glob replaces hardcoded skill list in installer
- **Bump script** — Now updates `config.yaml` version (HA Supervisor update detection); portable sed
- **Version sync** — All 5 version-bearing files updated together (version.json, package.json, plugin.json, marketplace.json, config.yaml)
- **Write skill** — Mandatory post-write review with H-check awareness for helper references
- **Inverse scope notes** — Read and write skills explicitly note helper exclusion with redirect

### Fixed
- **HA App version stuck at 0.1.0** — config.yaml now included in bump script
- **CI docs fact-check** — Updated skill directory count from 7 to 8
- **Portability** — `sed -i ''` replaced with temp-file approach (GNU/Linux compat)

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
