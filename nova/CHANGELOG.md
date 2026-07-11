# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

Recent changes are tracked in [GitHub releases](https://github.com/markusleben/ha-nova/releases)
and merged PRs. This changelog will be updated with the next tagged relay version.

## [Relay 0.4.0] - 2026-07-11

### Added
- `POST /files`: generic, opt-in file transport for the Home Assistant configuration directory (`list_dir`, `read_file`, `write_file`, `delete_file`). It exists to unlock YAML-only configuration (template/REST/command-line sensors, packages, themes) that has no API.
- App option `file_access: off | read | readwrite` (default **off**), and `FILE_ACCESS` for the standalone container. Nothing is accessible until it is set, and it stays off when no config directory is mounted rather than reporting a mode it cannot serve.

### Security
- Path containment: logical `/config/...` prefix, iterative percent-decoding, control-character rejection, `..` rejection, and a `realpath` check so a symlink inside the config directory cannot point out of it (verified for writes through a symlinked directory too).
- Always denied, in every mode: `custom_components`, `python_scripts` and `www` (Home Assistant EXECUTES what lives there — file access must not become code execution), `.storage`, `.cloud`, `.ssh`, `.git`, `deps`, `ssl`, `tts`, `backups`, plus prefix matches for `secrets.yaml*` (including `.bak`, `~` and `.old` copies, which hold the same credentials), the recorder database and its `-wal`/`-shm` siblings, `.env*`, and log files. The deny rules are re-applied AFTER symlink resolution, so a link with an innocent name cannot launder a denied target.
- Writes are additionally restricted to configuration formats (`.yaml`, `.yml`, `.conf`, `.json`, `.txt`, `.md`): a `.py` or `.sh` can never be planted, even outside the denied directories.
- Text only (binary is refused), 1 MiB read/write caps, 500 directory entries, no directory deletion.
- Writes are atomic (temp file + rename — a crash cannot leave a half-written configuration) and back up the previous version to `<file>.bak` by default.

## [Relay 0.3.0] - 2026-07-11

### Added
- `collect_events.on_limit: "return"` (window mode): the relay resolves with the events collected so far and marks the response `truncated: true` instead of failing when `max_events`/`timeout_ms` is reached. Enables bounded sniffing of streams that never emit a finish event (MQTT topics, event buses).
- Subscription WS commands are allowed inside a `collect_events` envelope. The collection unsubscribes in its `finally` block and its lifetime is bounded, so nothing leaks; bare subscriptions stay rejected.
- Binary responses on `/core`: non-text/non-JSON upstream bodies (camera frames) are returned base64-encoded with `body_encoding: "base64"` and `content_type`, capped at 8 MiB. They were previously UTF-8 decoded, which silently corrupted every non-ASCII byte.

### Compatibility
- Default behavior is unchanged: without `on_limit` the strict semantics apply, and JSON/text bodies (including the plain-text `/api/error_log`) keep their exact shape.

## [Relay 0.2.6] - 2026-07-08

### Fixed
- **`/health` reports the real Home Assistant connection state** — `ha_ws_connected` previously only reflected whether a connection object existed; because the WebSocket client auto-reconnects, it stayed `true` while HA was down or restarting. The relay now tracks the connection's `ready`/`disconnected` events, so `/health` is truthful between requests without active probing.
- **Bounded REST responses** — the `/core` proxy buffered upstream HA responses without a limit. Responses are now capped at the same 256 MiB ceiling as the WebSocket path, with an actionable error instead of unbounded memory growth.

### Added
- **Relay version response header** — `/ws` and `/core` responses carry `x-ha-nova-relay-version`, so the CLI can warn about an outdated relay during normal skill traffic (throttled), not only on explicit `relay health` runs.
- **Supervisor watchdog** — the add-on config enables `watchdog: tcp://` port liveness so Home Assistant restarts a crashed relay automatically.

### Changed
- **Actionable upstream error text** — network failures toward HA (`UPSTREAM_HTTP_ERROR`) now include a remediation hint instead of only the raw fetch error.

## [Relay 0.2.5] - 2026-07-07

### Fixed
- **Large WS command results no longer drop the connection** — the Node global WebSocket (undici) negotiates permessage-deflate and enforces a max decompressed message size; big responses such as `config/entity_registry/list` on instances with thousands of entities exceeded it and the connection died with an opaque `connection lost`. The relay now connects with the `ws` client (compression disabled, explicit 256 MiB payload ceiling) via a custom authenticated socket.

## [Relay 0.2.4] - 2026-07-07

### Fixed
- **Upstream WS error transparency** — structured Home Assistant command errors (rejected with HA's raw `{code, message}` payload) are no longer collapsed into a generic `WS request failed`. They surface as `UPSTREAM_WS_COMMAND_ERROR` with HA's own error code and text, and no longer tear down the healthy shared WebSocket connection. Numeric transport rejections from the WS library now map to readable messages.

## [Relay 0.2.3] - 2026-06-21

### Changed
- **Generic WS event collection** — event-response WS commands are now collected through an explicit bounded `collect_events` envelope. Skills choose the WS message and stop condition; the Relay only forwards and enforces transport limits.

## [Relay 0.2.2] - 2026-06-21

### Added
- **Finite WS event collection** — the relay now collects `system_health/info` event responses until `finish` and returns them as `data.events`, so Home Status can include real System Health details instead of an empty ack.

## [Relay 0.2.1] - 2026-06-15

### Changed
- **WS proxy hardening** — the relay now rejects subscription and live-update WS commands (`subscribe_*`, `render_template`) at the boundary with a 400. They resolve only on their initial ack and then emit events the relay cannot deliver, forcing the client to auto-unsubscribe — useless over the relay's request/response model, so blocking them avoids needless upstream subscription churn.

## [Relay 0.2.0] - 2026-03-07

### Changed
- **Independent version lines** — Relay (`config.yaml`) and Skills (`version.json`) are now versioned separately; skill-only updates no longer trigger unnecessary HA App rebuilds
- **Bump script** — No longer touches `config.yaml`; only bumps skill version files

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
- **Claude Code, Codex CLI, OpenCode, and Google Antigravity CLI** support via managed skill installation
