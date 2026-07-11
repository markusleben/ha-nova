# HA NOVA Masterplan 2026

Status: `active`
Scope: program-level plan for releases 0.14 → 1.0. Each release breaks down into its own short spec + PRs before implementation; this doc is the SSOT for sequencing and decisions until superseded.

## Context

Full audit of ha-nova (Relay + CLI + 20 skills) in July 2026, targeting the best future-proof HA AI skillset that outcompetes `homeassistant-ai/ha-mcp` ("The Unofficial and Awesome HA MCP Server", 3.9k stars, v7.12.3, 87 tools) — deliberately without being an MCP server.

**Audit verdict: no fundamental refactor needed.** Relay (1,760 LOC TS, 3 generic endpoints, test ratio 1.46:1) genuinely upholds "dumb relay"; onboarding is strong (1 command + 5-step wizard, ~3 irreducible manual actions); skills are disciplined (shared core, 0% German leakage, 19 contract-test files).

**Real gaps** (instead of a refactor):
1. **Coverage**: runtime domains missing entirely (media, notify, camera, voice/Assist, logs/diagnostics, MQTT, users, YAML-only configs). The fallback catalog claims "any task" but omits these domains.
2. **Safety gates depend on auto-bootstrap**: Write Routing Gate + preview confirmation live only in the context skill → bare agents (Codex/Antigravity/Hermes) lose the guarantees.
3. **Uninstall is server-side blind**: Relay app stays, repository stays, LLAT revocation never mentioned (note only prints when relay reachable, `cli/uninstall_preflight.go:101`).
4. **Update notices Claude-only**; other clients need manual `check-update`.
5. **Platform gate**: add-on requires HA OS/Supervised → ~25% of installs (Container/Core) excluded — exactly ha-mcp's HACS market.
6. **Token weight**: write path transitively ~25k words (~33–46k tokens).

**2026 ecosystem facts** (researched 2026-07): Agent Skills is an open standard (Anthropic Dec 2025, Linux Foundation AAIF since May 2026, ~40 tools incl. Codex/Cursor/Gemini CLI/OpenCode; skills.sh ~90k skills) → the "skills, not MCP" bet is ecosystem-validated. The official HA MCP server stays Assist-scoped (no config/registry threat). ha-mcp has more feature surface today (logs, camera, HACS, beta YAML/filesystem, screenshots) but weaknesses in context cost (87 tools → their own BM25 tool search is the symptom), no voice/media/notify skills, and in-process risk.

**Maintainer decisions**: all coverage domains incl. YAML-only + the InfluxDB edge case; full client parity; template + linter; guided uninstall teardown; `/files` opt-in (default OFF); Docker relay now, HACS component deferred; minimal-credible trust story; comparison page named/dated/fair.

---

## Workstream A — Template v2, Linter, Portability, Token Diet

### A1 Canonical Skill Template v2 (replace `docs/reference/skill-architecture.md` § "Skill Section Template")
- Canonical H2 order for all subskills: `Scope` → `Bootstrap (once per session)` → `Relay Contract` → [domain] → `Flow` → [domain] → `Error Handling` (optional, fixed position) → `Output Format` → `Safety` → `Guardrails` (optional) → `References` (optional).
- `Relay Contract` becomes required in 18 of 19 subskills (add it to write + review where it is missing today); `onboarding` stays a declared exception — the diagnostics skill whose body is remediation commands, consistent with its declared Bootstrap-heading deviation. `Guardrails` becomes officially optional (reality: 9/19); `## Output Rules` abolished — every `## Output Format` section starts exactly with ``Apply `skills/ha-nova/output-rules.md`.``
- fallback gets normalized (Safety Baseline + Safety Guardrails → one `## Safety`; `Agent Flow` → `## Flow`), not allowlisted.
- Bootstrap heading exact; only 2 declared deviations (onboarding, fallback).
- Terminology: prose says "App(s)"; `add-on`/`addon` only as code literals (fix 3 prose spots in `skills/backup/SKILL.md`).
- Context skill gets its own anchor list (router, not an operations skill).

### A2 Skill linter: `tests/skills/skill-template-contract.test.ts` (NEW, ~350 lines, vitest)
- Checks: frontmatter spec (name=dirname, lowercase-hyphen, ≤56 chars because of the `ha-nova-` prefix; description ≤1024; only allowed optional fields), section presence + order per skill class (MUTATION = 13 skills / READ_ONLY = 6), forbidden heading variants, App terminology (prose only, fences/inline code stripped), check-code leak (`/\b[SRPMFH]-\d{2}\b/` only in allowlisted files), Safety Core verbatim embedding, word budgets (dynamic globbing instead of hardcoded lists).
- Migrates the existing English-only and word-budget tests out of `tests/skills/ha-nova-contract.test.ts` (delete hardcoded lists).
- Lands with a temporary `KNOWN_DRIFT` skip map, burned down to empty.

### A3 Bootstrap-independent safety: embedded Safety Core
- Canonical 8-bullet block (~130 words), byte-identical at the top of `## Safety` in all 13 mutation skills, linter-enforced. SSOT: fenced block in skill-architecture.md.
- Content: preview-before-write; confirmation binds to the shown preview + expires on change; pre-preview phrases authorize drafting only; delete = typed `confirm:<token>`; never guess IDs; relay access only via `ha-nova relay`; **for writes this skill does not cover, STOP → `ha-nova:fallback` first**.
- READ_ONLY skills: 2-line read-only core (never mutating calls; write intent → owning skill/fallback).
- Verified: both installers (`cli/client_antigravity.go` rewriteFlatMarkdown, `scripts/onboarding/install-local-skills.sh`) rewrite embedded text correctly → flat installs keep the gates.
- fallback frontmatter sentence "Must be invoked before any raw relay write operation" gets linter-pinned (discovery-time gate).

### A4 Single-thread path for agent templates
- `skills/ha-nova/agents/resolve-agent.md` + `apply-agent.md`: new `## Execution Mode` section (~7 lines): dispatched vs INLINE (structured-text sections = internal checklist, never user output; "main thread" = you; Hard Scope binds in both modes).

### A5 Token diet (write path −31%: 24,815 → ~17,100 words)
- **M1**: make `review/checks.md` self-contained (move the application preamble from review/SKILL.md Step 1 there); write + helper reference only checks.md instead of the 4,566-word review/SKILL.md. Biggest lever: −6k tokens.
- **M2**: split write/helper `References` into "Always" (relay-api, payload-schemas, write-safety, best-practices) and "On demand" with triggers (automation-patterns only for branching/timing logic; template-guidelines only for Jinja; safe-refactoring only for rename/migrate). −3k tokens situational.
- **M3**: `skills/ha-nova/update-revert.md` (NEW) — extract revert execution (~900 words) from write-safety.md, loaded only on `revert`; snapshot capture stays hot.
- **M4**: rewrite the relay-api.md auth section: opens with "transport/auth handled internally by `ha-nova relay` (OS credential store); never set RELAY_AUTH_TOKEN/RELAY_BASE_URL" → kills env-var confusion for foreign agents.
- Rejected (KISS): payload-schemas split, checks.md family split.

### A6 Agent Skills standard alignment
- `license: MIT` + one-line `compatibility` field ("Requires the ha-nova CLI…") in all 20 frontmatters; deliberately NOT `metadata`/`allowed-tools` (version-sync burden, experimental).
- skills.sh listing only AFTER A1–A5 (don't distribute unhardened trees); bootstrap sections first get a "CLI not installed → install one-liner" line (today circular: "run ha-nova setup" without the CLI).

**Migration order A**: Template v2 (SSOT) → linter with KNOWN_DRIFT → heading normalization → Safety Core embedding (+ extend `ha-safety-contract.test.ts` mutationDocs to the full list) → Execution Mode → A5 restructuring (with `npm run e2e:skill:codex:promoted:smoke`) → frontmatter fields → cleanup + version.json bump. `npm run test:safe` green after every step.

---

## Workstream B — Coverage Expansion (8 new skills + 3 relay extensions)

Verified ground truths: `/api/error_log` works today (rest-client returns non-JSON as string); binary is corrupted today (`Buffer.concat(...).toString("utf8")`); the collect_events envelope canNOT sniff (ws-proxy.ts:60,80 blocks subscription types even inside the envelope; timeout/max_events rejects instead of partial return); `POST /files` is already spec'd as Phase 3 in `docs/reference/bridge-architecture.md:195-214` (with stale `ha_mcp` naming); `/api/hassio/*` is reachable via /core today (hassio_api: true, dormant); `ha-nova trace` exists (`cli/trace.go`).

### Priority roadmap

| # | Domain | Decision | Core APIs | Relay change | Prio |
|---|--------|----------|-----------|--------------|------|
| B11 | Complete fallback catalog | add rows (below) | — | — | **P1** |
| B4 | Logs & diagnostics | **NEW `diagnose`** (~170 ln): "why did X fail" workflow | `GET /api/error_log`, WS `system_log/list`, `trace/list\|get` + `ha-nova trace`, logbook/history, `POST /api/template`, `diagnostics/*`, `logger.set_level` (mutating, with reset reminder) | none | **P1** |
| B1 | Media | **NEW `media`** (~150 ln) | WS `media_player/browse_media`, `media_source/browse_media\|resolve_media`, `media_player/search_media` (feature-detect ≥2025.2), services play/volume/source/join, `tts.speak` + `tts/engine/list` | none | **P1** |
| B2 | Notify | **NEW `notify`** (~110 ln) | `GET /api/services` → notify targets, `notify.mobile_app_*`, `persistent_notification.*` + WS get | none (action callbacks honestly documented as limitation: helper+automation workaround pattern, roadmap row) | **P1** |
| B5 | Voice/Assist | **NEW `assist`** (~140 ln): flagship = utterance testing | WS `assist_pipeline/pipeline/*`, `POST /api/conversation/process`, `conversation/agent/list`, `homeassistant/expose_entity\|/list`, `tts\|stt/engine/list`, `wake_word/info`; `assist_pipeline/run` stays blocked (streaming) | none | P2 |
| B6 | MQTT | **NEW `mqtt`** (~130 ln) | `mqtt.publish` (retained = tokenized!), bounded sniffing via envelope v2 on `mqtt/subscribe`, `mqtt/device/debug_info` | **envelope v2** | P2 |
| B3 | Camera | **NEW `camera`** (~120 ln) | `GET /api/camera_proxy/{id}` (binary), WS `camera/stream` (HLS URL), `camera.snapshot\|record` (host file, allowlist_external_dirs warning) | **binary support** | P2 |
| B7 | Persons/Zones/Users/Tags | **NEW `admin`** (~160 ln; do NOT overload organize) | WS `person/*`, `zone/*`, `tag/*`, `config/auth/*` (v1 without password ops); refuse owner delete, LLAT-owner warning | none | P2 |
| B8 | YAML-only configs | **Relay `/files` (opt-in) + NEW `yaml-config`** (~190 ln) | see below | **`/files` + map** | P2 |
| B9 | InfluxDB/external | **NEW `external-sources`** (~140 ln) | see below | none | P3 |
| B10 | Weather | fallback row + 4-line example in `service-call` only (`weather.get_forecasts?return_response` already documented in relay-api.md:328) | — | — | P3 |

### Relay extensions (all generic transport, zero HA-domain logic)
1. **Envelope v2** (`nova/src/http/handlers/ws-proxy.ts` + `nova/src/ha/ws-client.ts`, ~40 lines + tests): allow subscription types INSIDE the `collect_events` envelope (finally-unsubscribe already exists; lifetime bounded); new `on_limit: "error"|"return"` (default `"error"` = today's semantics; `"return"` yields partial + `truncated: true`). Bare subscriptions stay blocked. Caps stay 100 events / 10 s.
2. **Binary on /core** (`nova/src/ha/rest-client.ts`, ~40 + 35 CLI lines): non-JSON/non-text → `{"body": "<base64>", "body_encoding": "base64", "content_type": ...}`, dedicated 8 MiB cap; CLI `ha-nova relay core --out-binary <file>` decodes.
3. **`POST /files`** (NEW `nova/src/http/handlers/files.ts`; `nova/config.yaml`: `map: [homeassistant_config:rw]` — verify mount point on live Supervisor (/homeassistant vs /config; endpoint accepts logical `/config/...` prefix, translates internally) + option `file_access: off|read|readwrite`, **default off**): actions `list_dir|read_file|write_file|delete_file`; realpath containment (reuse core-proxy.ts technique); always-deny: `.storage/`, `.cloud/`, `.ssh/`, `.git/`, `deps/`, `ssl/`, `secrets.yaml`, `home-assistant_v2.db*`, `*.log*`; caps 1 MiB, UTF-8 only, no dir delete/rename; `backup: true` (default) writes `.bak`. No YAML parsing in the relay. CLI: `files` in the endpoint switch (`cli/relay.go`, ~5 lines). Naming fix: convention dir becomes `/config/ha_nova/` (docs still say `ha_mcp`).
4. `yaml-config` skill flow: read → diff preview → confirm → write (auto-.bak) → `POST /api/config/core/check_config` → targeted reload (`template.reload` etc.) → verify entity in `/api/states` → on failure `.bak` restore. With `file_access: off`: graceful degradation to guided manual editing (works today via /core check_config + reload).

### B9 InfluxDB / external sources
- Relay proxying foreign hosts **rejected** (SSRF, charter violation).
- **Primary**: `external-sources` skill — client-side direct queries against InfluxDB's own HTTP API (version detect: `/ping`→1.x InfluxQL, `/health`→2.x Flux, 3.x SQL); read-only by contract; credentials v1 via env vars (`HANOVA_INFLUXDB_URL/_TOKEN`), v2 optional `ha-nova secret set` (keyring generalization, does not block v1). Generic pattern (Prometheus/Grafana/z2m as later sections).
- Secondary: recurring metrics → `influxdb` sensor via `yaml-config` (B8) → normal history/energy skills.
- Detects `influxdb` in `GET /api/components`, corrects the premise first (HA only writes out, never reads back).

### Fallback capability map: rows to add/change
Media, Notify (+ roadmap row actionable callbacks), Camera, Logs/Diagnostics, Voice/Assist, MQTT, Persons/Zones/Tags (replaces current row), User management, Weather (Covered via service-call), YAML configs (Roadmap→Covered), External sources, Frontend themes (Roadmap via yaml-config), **Apps/Supervisor: External → Relay-Ready** (via `/api/hassio/*`), HACS (External, sharpened), ESPHome (External), Zigbee2MQTT (External + mqtt-skill note), Network (External). Event-subscriptions row: bounded ships with envelope v2, true streaming stays Phase 1c.

**Shipping order B**: B11 → B4 → B1 → B2 (pure skill wave, no relay release) → **Relay 0.3.0** (envelope v2 + binary) → B6 → B3 → B5 → B7 → **Relay 0.4.0** (`/files`) → B8 → B9 → B10.

Dispatch sharpness at 28 skills: `diagnose` = root-cause a concrete failure, `health` = current status, `history` = timelines. Every new skill: dispatch row + examples in `skills/ha-nova/SKILL.md`, row flip in fallback, Template-v2-conformant (linter enforces), doc updates (`ha-api-matrix.md`, `bridge-architecture.md`, `relay-api.md`).

---

## Workstream C — Lifecycle, Distribution, Trust

### C1 Guided uninstall teardown
- **Order: server-side FIRST** (HA URL/token needed for deep links + verify; purge deletes them; teardown touches no local files → cancellable/idempotent; Windows handoff comes after).
- Flow (TTY): preflight + mode prompt (unchanged) → teardown offer (only when config with HAURL exists AND HA reachable; default yes) → **1/3 uninstall app** (opens `haRelayAppPageURL`; verify: `fetchRelayHealth` 3× → "relay no longer answers" or warning, never block) → **2/3 remove repository** (opens NEW `haAppStoreURL` = `_my_redirect/supervisor_store`; note: HA allows repo removal only after app uninstall) → **3/3 revoke LLAT** (opens `haProfileSecurityURL`; deliberately unverifiable — CLI never held the LLAT) → local removal (existing).
- Non-interactive/`--yes`/HA unreachable: **always** print the full 3-item HA checklist in the report (URLs captured from `loadJSONConfig` before purge; else generic paths). Replaces today's conditional single note.
- Reuse: `promptWizardLineFromReader`/`promptWizardYesNoFromReader` (`cli/setup_wizard_navigation.go`), `renderSetupStep`/`renderSetupLink` (`cli/setup_ui.go`), `openAnnouncedBrowserURL` (`cli/setup_urls.go`), `fetchRelayHealth` (`cli/command_doctor.go`). Important: use `loadJSONConfig` directly, not `loadConfig` (fails on partial configs).
- Files: NEW `cli/uninstall_teardown.go` (~180 LOC) + `cli/uninstall_teardown_test.go`; `cli/setup_urls.go` (+`haAppStoreURL`); `cli/command_uninstall.go` (wiring); `cli/uninstall_preflight.go` (unconditional checklist).

### C2 Update nudge for all clients
- NEW `cli/update_nudge.go` (~90 LOC) + test: `maybeNudgeSkillUpdate(paths)` in `runRelayProxy` (ws + core) after the relay-version check; replace `runHealth`'s inline `checkForUpdate` with the helper (fixes hidden 15 s stall).
- Mechanics: opt-out `HA_NOVA_NO_UPDATE_NUDGE=1`; dev guard; **cache-only compare, never network in the hot path** (`inspectCachedRelease` + `compareReleaseVersions`); 24 h throttle marker in CacheDir (pattern of `shouldWarnRelayOutdated`); background refresh via detached `check-update --quiet --json` whose stdio must be detached from the parent's streams (Go: leave `exec.Cmd` Stdout/Stderr nil → /dev/null) so the child's JSON can never interleave with relay JSON that skills parse; the nudge itself prints on **stderr** via `printHumanNotice` (stdout stays pure JSON → skill parsing safe).
- Wording agent-phrased: `"HA NOVA update available: vX -> vY. Inform the user: run 'ha-nova update', then start a new session."`
- `skills/ha-nova/SKILL.md` Self-Update: +1 line (surface notice to user once); extend existing pins in `tests/skills/ha-nova-contract.test.ts` only.

### C3 Distribution
- **Docker relay (option B)**: same TS code, GHCR publish job (`ghcr.io/markusleben/ha-nova-relay`, lockstep with relay version 0.2.x), wizard branch (~150 LOC: prints `docker run` with generated token; LLAT as env var), docs `--restart unless-stopped`. Relay is already fully env-driven (`nova/src/config/env.ts`) → pure packaging. Unlocks ~25% market (Container/Core). + "run anywhere" footnote (`node nova/dist/...`).
- **HACS custom component: deferred** with explicit revisit trigger ("when onboarding/support data shows the token paste measurably blocks Container users"). Rationale: second implementation = false-parity bug class; benefit shrinks to one saved paste under NOVA's security model. Positioning bonus: process isolation (a crash can't take HA down; one scoped token instead of the in-process `hass` object).
- **Listings**: skills.sh (topics `agent-skills`, `home-assistant`, `claude`, `codex`) AFTER partial-install hardening; anthropics/skills PR as an attempt; skip MCP directories (wrong shelf). Highest ROI: HA Community forum, awesome-home-assistant PR, r/homeassistant — timed to the 0.16 "runs everywhere" release.

### C4 Trust story (minimal credible)
- Use what exists: ~3.1k LOC live eval harnesses (`scripts/e2e/codex-ha-nova-*.sh/py` + scenario JSONs), `scripts/dev-seed-ha-llat.mjs`.
- NEW `scripts/e2e/disposable-ha/`: docker compose (`homeassistant/home-assistant:stable` + pre-baked config with demo entities), relay as `node nova/dist/index.js` (simultaneously validates the Docker distribution), one entry script: boot → seed → scenario suite → teardown.
- NEW `docs/safety.md`: guarantee table — claim | enforced by (file/skill section) | verified by (test/scenario) | date. Rows: preview-before-write, read-back verification, tokenized deletes, revert stack (N=5), LLAT server-side only, OS keyring, response caps, no telemetry.
- Cadence: deterministic parts as weekly non-blocking GitHub Action; codex agent evals manually per release with dated report in `docs/safety-evidence/`; 1 revert-stack demo (webp) in `assets/`.

### C5 Comparison page
- NEW `docs/comparison.md` ("Why 20 skills instead of 87 tools"), linked from README via release-prep PR (README gate!). ha-mcp named, all claims dated "as of 2026-07": context cost (ha-mcp's own BM25 tool search as evidence), safety model table, token model, process isolation, **honesty column** (what ha-mcp does better today — shrinks with workstream B). Do NOT reuse the historical 9/13-no-verify claim.

---

## Release sequencing

**Maintainer decision (2026-07-11): all phases land on `main` first; ONE release ships at the very end.** The former 0.14–0.17 staging becomes internal waves — same content, same order, no intermediate tags. `main` may run ahead of the stable tag for the duration of the program; the README stays stable-truth the whole time (release-bound claims collect in the release-body draft per the README gate).

| Wave | Theme | Contents | Status |
|------|-------|----------|--------|
| **1** | Lifecycle & parity | C1 guided teardown + C2 update nudge | **DONE** — #288, #289 |
| **2** | Template & portability | Template v2 + structural linter, bootstrap-independent Safety Core, Execution Mode, token diet (write path −~9k tokens), Agent-Skills frontmatter | **DONE** — #290, #291, #292, #293, #294 |
| **3** | Coverage wave 1 | `diagnose`, `media`, `notify`, `camera`, `mqtt` + fallback catalog; Relay 0.3.0 (envelope v2 window mode + binary responses) | **DONE** — #295, #296, #297, #298 |
| **4** | Runs everywhere | Standalone container relay (HA Container/Core), GHCR publish with smoke-then-promote, one-codebase guard | **DONE** — #299 |
| **5** | Trust & compare | `docs/reference/safety.md` (guarantee → enforced by → verified by), `docs/reference/comparison.md` (named, dated, honest both ways), live disposable-HA e2e (weekly, non-blocking) | **DONE** — #301 |
| **6** | Coverage wave 2 | Relay 0.4.0 `/files` (opt-in, default off) **DONE** — #302. Skills `yaml-config`, `assist`, `admin`, `external-sources` + weather row: **branch `feat/wave6-skills` (ae3dcda), pushed, NOT yet a PR** | **IN PROGRESS** |
| **Final release gate** | | See "Where to pick up" below | open |

## Where to pick up (2026-07-12)

**Everything through wave 5 is on `main`.** 17 PRs merged; `main` is green (typecheck, 74 test files / 721 tests, Go suite, docs fact-check, live e2e against a real Home Assistant).

**Immediate next step — finish wave 6:**
1. `git checkout feat/wave6-skills && git merge main` (the branch predates the relay-0.4.0 merge; it carries `min_relay_version: 0.4.0`, which only becomes valid once #302 is in — it now is).
2. `npm run typecheck && npm run test:safe && bash scripts/check-docs.sh` — the skill-directory count in `scripts/check-docs.sh` is already set to 29 on that branch; the linter classes, dispatch table, fallback map, Hermes readiness list and architecture doc are wired.
3. Add the code-execution boundary to `skills/yaml-config/SKILL.md`: the relay now refuses `custom_components`, `python_scripts`, `www` and only writes `.yaml/.yml/.conf/.json/.txt/.md` — the skill must say so instead of discovering it as an error.
4. PR → `@codex` → fix findings → merge (standard cycle).

**Then the release (the only thing left):**
- Release-prep PR (README is stable-truth and may ONLY change here, together with a `version.json` bump — that is what `readme-release-gate` enforces):
  - Version bump via `npm run bump -- <version>` (0.14.0 is the working assumption). `scripts/bump-version.sh` updates every version-bearing file — `version.json`, `package.json`, `package-lock.json`, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`; a `version.json`-only edit is not enough. The bump touches manifests → add `manifest-review:approved` per the PR checklist.
  - `README.md`: "19 task skills" → 28; relay size "~1.5K lines" → ~2.7K; add the platform claim (App **or** standalone container → runs on every HA install type); link `docs/reference/safety.md` and `docs/reference/comparison.md`.
  - Release notes: refresh `docs/work/0.14.0-release-body.md` against the shipped wave-6 state before use — the wave-6 PR moves the relay floor to 0.4.0 and adds the wave-6 skill bullets; verify the draft carries them rather than trusting "already written". Keep it short and user-centric per the release-notes rule.
- Then the tag flow per `docs/releasing.md`: strict-mode `verify-release-pipeline.sh` → push `vX.Y.Z-rc1` on the reviewed commit → verify the public install → tag the final `vX.Y.Z` on that same commit.

**Known open items (not release blockers):**
- Windows gold test (carried over from before this program).
- The setup wizard has no guided "Docker" branch yet; Container/Core users follow `docs/reference/relay-container.md` (non-interactive `ha-nova setup --non-interactive --ha-url ... --relay-url ... --relay-token ...`). Documented as planned.
- `skills/helper/SKILL.md` is 489 lines (over the ~400 guidance) — split candidate, deliberately deferred.

Every wave follows repo rules: one topic one branch, PR merge checklist, Codex review cycle; the single release at the end follows the full release rehearsal gate.

## Opinionated defaults (documented instead of asked)
- Teardown in standard mode: config kept with a hint note ("points at nothing; `--purge` or keep for reinstall") — no auto-purge.
- `--yes` checklist with concrete URLs even on `--purge` (captured before deletion).
- Nudge wording agent-phrased (pattern: existing relay-outdated warning).
- Docker image lockstep with relay version; `helper/SKILL.md` split (489 ln → family files) as a wave-B candidate, not a blocker.
- Coverage wave 1 before Relay 0.3.0 (pure skill releases first).

## Known risks
- Safety-Core verbatim: wording changes touch 13 files — accepted deliberately, the linter turns it into one mechanical PR.
- M1 regression: if review/SKILL.md nuances were silently load-bearing → live smoke in step 6 catches it.
- `map` mount point (B8) and `media_player/search_media` availability: verify on live Supervisor before freezing specs.
- `mqtt.publish` retained + user delete (B7): strictest confirmation tiers, owner/LLAT protection.
- 28 skills → dispatch sharpness becomes critical; linter + context-skill examples push back.
- `compatibility` field rendering in Antigravity/Hermes unverified → check after `dev:install`; fallback: `license` only.
