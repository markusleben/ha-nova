# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Chat apps can now do what HA NOVA on a computer does: rename devices and entities, move them to rooms, set up helpers, scenes, dashboards, and integrations, edit files (with file access on), read the error log, and look at a camera. Every change shows a preview first, with the same confirmations as on a computer. Only setting up HA NOVA itself (setup, pairing, updates, turning chat apps on or off) stays on a computer.
- NOVA page, Chat apps: under "Add to Claude", what to set so Claude stops asking before every step. With a linked phone: every tool on "Always allow". Without one: every tool but "Run a confirmed change".

### Changed
- Installing or updating a HACS download, and adding or changing a dashboard resource, now take your own confirmation, like a delete, in chat apps and on a computer: that code runs in every signed-in browser.

## [Relay 0.11.5] - 2026-10-24

### Fixed
- NOVA page: the ChatGPT steps quote the labels ChatGPT shows and say to name it HA NOVA. If ChatGPT shows an error right after, choose Connect on the plugin again. (#848)
- Documentation tab: plain words, and Home Assistant Link instead of "Cloud". Troubleshooting is now steps you can follow. (#852)
- Chat apps: an action on a device, like a power strip, also reaches its child devices, like its outlets. Previews of automations and scripts now list them. (#847)
- Chat apps: a new phone question first clears the last result chat apps sent to that phone. On Android, it no longer lands in a group of notifications. (#850)
- Chat apps: previews say "with a backup first", not "backup set to true". (#852)
- Chat apps now run HA NOVA 0.27.3 with these fixes.

### Upgrade notes
- Coming from App 0.11.2 or older? In ChatGPT, open chatgpt.com/plugins, choose HA NOVA, then Refresh. From 0.11.3, no Refresh is needed.
- Computers that are already connected stay connected; no re-pairing.
- Computers on HA NOVA 0.27.3 ask you to update this App; computers on 0.27.0 to 0.27.2 keep working with it.

## [Relay 0.11.4] - 2026-10-23

### Fixed
- Android phones can be linked for confirmations, with HA NOVA 0.27.2 on your computer. Before, the question to link, move, or stop never reached most Android phones. Once a phone is linked on your computer, chat apps ask that phone too. (#842)
- Chat apps: the phone's notification and the "waiting for your tap" message now say how to answer on your phone: press and hold on iPhone, pull down on Android. Before, they said to expand the notification. (#842)
- Chat apps: if a phone question gets no answer in time, the chat now says where to turn on that phone's notifications (on Android, also pop-ups). A phone that never had an HA NOVA notification now shows them as pop-ups. (#844, #845)
- Chat apps now run HA NOVA 0.27.2 with these fixes.

### Upgrade notes
- Coming from App 0.11.2 or older? In ChatGPT, open chatgpt.com/plugins, choose HA NOVA, then Refresh. From 0.11.3, no Refresh is needed.
- Computers that are already connected stay connected; no re-pairing.
- Computers on HA NOVA 0.27.2 ask you to update this App; computers on 0.27.0 and 0.27.1 keep working with it.

## [Relay 0.11.3] - 2026-10-23

### Fixed
- Chat apps: the line that asks for the code now says how long it works, like "Type this code within 15 minutes:". Other notes left the ask, so Claude can't move them below the code. The note "I can see this code too" now comes only in apps that read HA NOVA's instructions; Claude doesn't. (#837, #838)
- Chat apps: phone tips now name only steps that exist: link a phone on your computer, or check that phone's Home Assistant app. The old tip to link a phone on the NOVA page is gone. (#837)
- NOVA page: plain, true wording. Most computers connect by signing in, and a pairing code works once, for 10 minutes. ChatGPT needs a paid plan that lets you add your own plugins (we tested Pro); on a phone, it can only answer. (#835, #836)
- Chat apps now run HA NOVA 0.27.2 with these fixes.

### Upgrade notes
- In ChatGPT, open chatgpt.com/plugins, choose HA NOVA, then Refresh. ChatGPT then gets the new instructions.
- Computers that are already connected stay connected; no re-pairing.
- Computers on HA NOVA 0.27.2 ask you to update this App; computers on 0.27.0 and 0.27.1 keep working with it.

## [Relay 0.11.2] - 2026-10-22

### Fixed
- Chat apps: the code to confirm a change now ends the message in Claude and ChatGPT. How long the code works, and notes like the demo house's hourly reset, come right above it. (#831)
- NOVA page: on a Claude Team or Enterprise plan, an owner adds HA NOVA once for everyone with the new "Add for my organization" link. If Claude opens with no form, the page shows the address to paste. (#830)
- Chat apps: ChatGPT on a phone can't make changes, in its app or in a phone browser. HA NOVA now says to use chatgpt.com on a computer, or the Claude app on the phone. Questions still work on the phone. (#834)
- Chat apps now run HA NOVA 0.27.2.

### Upgrade notes
- In ChatGPT, open chatgpt.com/plugins, choose HA NOVA, then Refresh. ChatGPT then gets the new instructions.
- Computers that are already connected stay connected; no re-pairing.
- Computers on HA NOVA 0.27.2 ask you to update this App; computers on 0.27.0 and 0.27.1 keep working with it.

## [Relay 0.11.1] - 2026-10-22

### Fixed
- Chat apps: ChatGPT's phone app can't make changes through plugins you add yourself. HA NOVA now says so and asks you to use chatgpt.com in a browser. Questions still work on the phone. (#824)
- Chat apps: the code to confirm a change now comes last, on its own line, in bold. (#825)
- Chat apps: if nothing changed, the code you typed now works, even when the safety check was slow. If something did change, HA NOVA says what and asks again. (#827)
- Chat apps now run HA NOVA 0.27.1. App 0.11.0 had an early 0.27.0 build, so this also brings every 0.27.0 fix to chat apps, like hiding lock and alarm codes from the AI. (#801)

### Upgrade notes
- In ChatGPT, open chatgpt.com/plugins, choose HA NOVA, then Refresh. ChatGPT then gets the new instructions.
- Computers that are already connected stay connected; no re-pairing.
- Computers on HA NOVA 0.27.1 ask you to update this App; computers on 0.27.0 keep working with it.

## [Relay 0.11.0] - 2026-10-20

### Added
- Use HA NOVA in Claude and ChatGPT, also on your phone, with no computer running. On the NOVA page, open Chat apps and choose "Turn on chat apps" (or turn them on during `ha-nova setup`). Home Assistant restarts once. Then add HA NOVA in Claude (there is an "Add to Claude" button) or in ChatGPT on the web, and sign in to Home Assistant as the owner. Needs Home Assistant 2026.10 or newer and Home Assistant Link with remote access (a subscription from the Home Assistant team) or your own public HTTPS address. Off by default; only the owner can turn it on and use it. Guide: [Use HA NOVA in Claude and ChatGPT](https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md). (#756, #758, #760, #762)
- On the NOVA page you can also let chat apps only read, or turn off codes in the chat so risky changes need your phone. (#756)
- In Claude, HA NOVA's tools have plain names, and Claude can stop asking before every step: set the read-only tools and "Control a device" to "Always allow", and keep "Run a confirmed change" on "Needs approval". Answers use your names as in Home Assistant, with no ids. Guide: [Stop Claude from asking before every step](https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md#stop-claude-from-asking-before-every-step). (#784)
- Setting up Home Assistant Link as the owner connects a computer with that one sign-in, without the six-digit code. Other users still get the code. (#757)
- When you uninstall HA NOVA from the last computer that uses this App, `ha-nova uninstall` can remove the App too, after the owner signs in. The App now tells a computer whether other devices still use it. (#754)

### Upgrade notes
- **Turn chat apps off before you remove this App or restore an older version** (NOVA page → Chat apps → "Turn off chat apps"). Turning them off removes the part HA NOVA added to Home Assistant (`custom_components/ha_nova`) and signs out its own access.
- Went back to an older App with chat apps still on? Update this App again, then run `ha-nova chat-apps off`. The App also cleans up what is left in Home Assistant when it starts; if the NOVA page says chat apps are not fully off, choose "Finish turning off". (#776)
- Turning chat apps off also removes an empty `custom_components` folder if this App made it. (#793)
- Chat apps help on the NOVA page: if ChatGPT says the OAuth callback data is missing right after you create the plugin, choose Connect on it again. (#792)
- Computers that are already connected stay connected; no re-pairing.
- Computers on the matching HA NOVA release ask you to update this App; computers on an older release keep working with it.
- Still on the old shared access (Relay Auth Token)? HA NOVA 0.27 is the last release that accepts it. Run `ha-nova setup` once on each computer to switch (a `--service` install: `ha-nova pair --credential-store=file`). When every computer has switched, choose "Revoke legacy access" on the NOVA page.

## [Relay 0.10.0] - 2026-10-17

### Added
- Connect a computer by signing in with Home Assistant: the HA NOVA setup on that computer opens your Home Assistant login, you sign in as the owner, and the computer is connected without typing a code. The six-digit code from the NOVA page keeps working as before. Requires the matching HA NOVA release on the computer; older versions keep using the code. (#701)
- While a computer is connecting this way, the NOVA page shows "A computer is connecting through Home Assistant sign-in…" with a Cancel button. (#701)

### Changed
- Home Assistant Cloud is now called Home Assistant Link on the NOVA page and in the documentation, following Nabu Casa's rename. Nothing to change on your side. (#722)

### Upgrade notes
- Computers that are already connected stay connected; no re-pairing.

## [Relay 0.9.1] - 2026-10-15

### Fixed
- A Home Assistant that rejects the Relay's token no longer causes an endless reconnect loop. (#651)
- The Home Assistant WebSocket authentication handshake has a deadline; an abandoned connection is closed instead of wedging later calls. (#627)
- Malformed core REST and Supervisor replies (invalid JSON or UTF-8) fail as `502 / UPSTREAM_HTTP_ERROR` instead of a successful empty result. (#630)
- `RELAY_PORT` must be a plain integer; values such as `8791oops` are rejected at startup. (#630)
- Private `/data` files readable by other users are repaired to owner-only; files owned by another user stop the App with a `chown` hint. Persistence failures are logged with their real cause. (#628)
- Concurrent configuration-snapshot saves can no longer exceed the store's hard caps. (#633)

### Changed
- Published as prebuilt multi-arch images (`ghcr.io/markusleben/{arch}-ha-nova-relay`): Supervisor downloads the App instead of building it on the device. (#674)
- Runtime dependency updates: `ws` 8.22, `home-assistant-js-websocket` 9.7, `@peculiar/x509` 2.1. (#649)

## [Relay 0.9.0] - 2026-08-04

- NOVA page device list: two-step arm/confirm for revoke, legacy revoke, and
  registry reset (device-bound CSRF tokens; typed RESET for the strongest
  gate); per-device added/last-used timestamps; cloud badge and bound HA user
  on the confirm screen.
- Device registry: throttled last-used tracking that survives a broken disk;
  atomic writes clean up their temp files on failure.

## [Relay 0.8.0] - 2026-07-29

### Added
- Optional Home Assistant Cloud ingress for the existing Relay routes, with user-bound device authentication and separately revocable pairings.
- Persistent Relay instance identity so local and Cloud routes cannot silently bind to different App installations.

### Security
- Functional Cloud routes require the Supervisor ingress peer, one Home Assistant user identity, the matching active device, and the current Relay instance.
- Credential-bearing Home Assistant and Supervisor HTTP clients reject redirects.
- Disabled builds expose only Cloud capability discovery and self-revocation; setup and functional Cloud routes remain unavailable.

## [Relay 0.7.1] - 2026-07-23

### Changed
- Migrated the certificate runtime to `@peculiar/x509` 2.0 while preserving existing TLS identities and SPKI pins byte-for-byte. Partial identity recovery now remains retryable after an interrupted write. (#421)
- Updated `ws` to 8.21.1 for corrected fragment accounting and lower defensive fragment/chunk ceilings. (#424)

### Fixed
- Invalid UTF-8, UTF-16, and ambiguous byte-order marks are rejected before Relay route dispatch instead of being decoded with replacement characters. Valid Unicode, including umlauts, remains unchanged. (#423)

## [Relay 0.7.0] - 2026-07-20

### Added
- Secure device pairing: OPAQUE (RFC 9807) handshake over an SPKI-pinned TLS 1.3 device listener; every device gets its own credential and can be revoked individually. (#374)
- NOVA console via Supervisor ingress: generate one-time six-digit pairing codes ("Connect a device"), list paired devices, revoke any one, and manage migrated legacy access. (#374)
- Supervisor-token upstream in App mode — no `HA_LLAT` in the App anymore; standalone Container/Core keeps its server-side token. (#374)
- Automatic legacy migration: the pre-pairing shared relay token is imported as a digest on first start and the stored plaintext is cleared; legacy access keeps working until revoked. (#374)
- The App enables its NOVA sidebar entry once on first start (also right after this update); hiding it afterwards is always respected. (#384)

### Fixed
- Fresh App installs could fail to start because a required option had no default. (#376)

## [Relay 0.6.0] - 2026-07-15

### Added
- Home Base: an admin-only Home Assistant sidebar page with Relay status, the current pairing code, and install guidance.
- `POST /pair`: exchange a short-lived, single-use six-digit code for the Relay token during local setup.
- App-managed persistent Relay tokens when the advanced `relay_auth_token` option is left empty. Existing configured tokens remain compatible.

### Changed
- `/health` now reports the Home Assistant WebSocket disconnect reason (`auth`, `network`, or `never_connected`) and snapshot-store status.
- `LOG_LEVEL` is applied at runtime; rejected auth requests and unexpected request failures now produce useful logs without exposing secrets.
- HTTP request, header, keep-alive, and upstream response limits are explicit and configurable where appropriate.

### Security
- Pairing is peer- and globally rate-limited, returns generic failures, rotates codes after success or expiry, and marks every response `no-store`.
- Home Base requires the real Supervisor ingress peer plus authenticated user headers; direct-port header spoofing is rejected.
- Secret comparisons no longer reveal length through an early return.

## [Relay 0.5.0] - 2026-07-14

### Added
- `POST /backups`: a bearer-authenticated, generic gzip JSON store for named and automatic config snapshots, with bounded save/load/list/delete/prune actions and no Home Assistant business logic.
- Snapshot storage persists in the App data directory and can use a mounted `SNAPSHOT_DIR` in the standalone container.

## [Relay 0.4.1] - 2026-07-12

- The App info page now explains what the NOVA Relay is, what it deliberately does not do, and where the security model lives — instead of a one-line stub. No functional changes.

## [Relay 0.4.0] - 2026-07-11

### Added
- `POST /files`: generic, opt-in file transport for the Home Assistant configuration directory (`list_dir`, `read_file`, `write_file`, `delete_file`). It exists to unlock YAML-only configuration (template/REST/command-line sensors, packages, themes) that has no API.
- App option `file_access: off | read | readwrite` (default **off**), and `FILE_ACCESS` for the standalone container. Nothing is accessible until it is set, and it stays off when no config directory is mounted rather than reporting a mode it cannot serve.

### Security
- Path containment: logical `/config/...` prefix, iterative percent-decoding, control-character rejection, `..` rejection, and a `realpath` check so a symlink inside the config directory cannot point out of it (verified for writes through a symlinked directory too).
- Always denied, in every mode: `custom_components`, `python_scripts` and `www` (Home Assistant EXECUTES what lives there — file access must not become code execution), `.storage`, `.cloud`, `.ssh`, `.git`, `deps`, `ssl`, `tts`, `backups`, plus prefix matches for `secrets.yaml*` (including `.bak`, `~` and `.old` copies, which hold the same credentials), the recorder database and its `-wal`/`-shm` siblings, `.env*`, and log files. The deny rules are re-applied AFTER symlink resolution, so a link with an innocent name cannot launder a denied target.
- Writes are additionally restricted to configuration formats (`.yaml`, `.yml`, `.conf`, `.json`, `.txt`, `.md`): a `.py` or `.sh` can never be planted, even outside the denied directories.
- Text only (binary is refused), 1 MiB read cap and 768 KiB write cap (the write must fit the JSON request body together with its envelope and escaping — an exact 1 MiB limit would surface as an opaque 413), 500 directory entries, no directory deletion.
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
