# Choices

## 2026-02-25

### User Decisions (Brainstorm Q1-Q10)
- Priority: balanced (`Demo + API + Skill`).
- DoD: combined DoD with reduced scope.
- Target client: client-agnostic.
- Security strictness in 1a: dev-first.
- Runtime quality: functional + safe error handling.
- `ha-onboarding.md`: ultra-lean.
- Onboarding auth: user-generated LLT mandatory (`HA_TOKEN`), supervisor token not onboarding credential.
- `ha-safety.md`: core-only.
- `ha-nova.md`: only active 1a/1b skills.
- Test strategy: lightweight automation (Vitest).
- Output format: a single plan file.

### Planning Defaults (Agent)
- Plan structure: vertical-slice approach (preferred over infra-first/skill-first).
- Error contract: unified JSON error envelope across all endpoints.
- Execution workspace: worktree-skill fallback used (repo without commit/HEAD), so feature branch `feat/phase-1a-foundation` was executed in the current workspace.
- GitHub owner for remote operations: canonical API login `markusleben` used (rename from `w0nk1`).
- Branch strategy after repo bootstrap: set `main` as default branch, merge PR #1 into `main`, then apply post-merge wiring fix directly on `main`.
- Phase 1b execution mode: skill-first (REST workflow skills + acceptance doc), no new relay endpoints in this step.
- Branch for 1b: `feat/phase-1b-rest-skills` from `main`.

## 2026-02-26

### Planning Defaults (Agent)
- Upstream token policy (superseded): dual auth with deterministic fallback (`HA_LLAT` > app option `ha_llat` > legacy `HA_TOKEN` > `SUPERVISOR_TOKEN`).
- Compatibility policy (superseded): keep `HA_TOKEN` required for existing relay auth behavior, mark `HA_TOKEN` as deprecated only for upstream LLAT resolution.
- Dev ergonomics policy: provide idempotent LLAT seed command (`npm run seed:llat`) that validates options before write.
- Supervisor API write policy: merge existing app options before posting update to avoid unintended option loss.
- Planning gate policy: no new plan without prior review of current official HA docs (developer docs + app docs + user docs).
- Runtime auth split policy (superseded): relay endpoint auth token and upstream token selection are separated (`RELAY_AUTH_TOKEN` preferred for relay auth; upstream fallback uses `HA_TOKEN` only when relay auth is explicitly separated).
- Runtime capability policy (superseded): if upstream auth is limited/none, relay stays up and returns explicit WS error instead of crashing startup.
- Phase-1a.3 planning policy: App packaging keeps shell runner thin (`app/run`), with all auth precedence in Node runtime only (KISS + DRY).
- Security baseline for packaging (superseded): require explicit `relay_auth_token` in app options; keep `ha_llat` optional persistent capability token.
- Terminology policy (user): prefer "App" wording in docs/UX communication; keep legacy "addon" only where technical HA interfaces enforce it.
- Architecture policy (user): MVP first, but always modular-by-design to minimize future adaptation cost.
- Skill policy (user): skills remain markdown-only (`*.md`), while relay stays lean and efficient.
- Clean-slate policy (user): no backward-compatibility fallbacks in this non-public phase; remove legacy token paths and aliases instead of preserving them.
- Env contract policy: relay auth requires `RELAY_AUTH_TOKEN`; no `HA_TOKEN` fallback.
- Naming policy (superseded): internal runtime/env naming uses `app_*` (`APP_OPTIONS_PATH`, `app_option_ha_llat`) while keeping `/addons/...` only for Supervisor API endpoints.
- Live validation policy: ship one deterministic HA E2E runner (`smoke:app:e2e`) that validates Supervisor options flow and relay `/health` + `/ws` in one command.
- Contributor deploy policy: provide two explicit HA deploy modes — `deploy:app:fast` for normal iteration and `deploy:app:clean` for aggressive cache-busting rebuilds.
- Contributor env policy: keep secrets in untracked `.env.local` (or `.env`), ship committed `.env.example`; script env files are convenience only and must not override explicitly exported shell vars.
- E2E policy refinement (superseded): `SUPERVISOR_TOKEN` is optional for runtime-only smoke and mandatory only for Supervisor option preflight / `--apply`.
- Language policy enforcement: keep all project documentation English-only, including reference and migration docs.
- Supervisor-first runtime policy (superseded): maximize operation without LLAT; treat `HA_LLAT` as optional fallback for non-App contexts.
- App options policy refinement (superseded): optional password fields must avoid `null` defaults/writes (use empty string or omit key) to prevent Supervisor validation/start failures.
- Capability wording policy (superseded): describe supervisor fallback as "limited WebSocket scope" (not generic limited API scope).
- Contributor bootstrap policy: provide a single bootstrap deploy command that handles sync, first install, Supervisor option validation/write, and app restart.
- Option write normalization policy: write `ha_llat` as string (possibly empty) during bootstrap option apply to overwrite stale `null` values in Supervisor options.
- Audience policy refinement (user): primary target is end-user onboarding simplicity; developer bootstrap tooling must remain clearly separated from normal user flows.
- Naming policy refinement (dev tools): keep developer-only scripts under `dev:*` npm namespace and separate script path (`scripts/dev/*`).
- Secret storage policy (user + research): macOS onboarding is Keychain-first (`security`), while `.env.local` remains contributor-only convenience.
- Onboarding UX policy (macOS): provide one interactive setup command with Keychain-backed secrets and explicit diagnostics.
- Separation policy refinement (user): end-user onboarding script must not include SSH/bootstrap; contributor bootstrap remains isolated under `scripts/dev/*`.
- Onboarding validation policy (user): setup must validate HA instance host and probe Relay health; on Relay failure, show explicit install/start guidance for the App.
- Onboarding resilience policy: if HA validation fails, allow explicit user override (`Continue with unverified host`) instead of hard-blocking setup.
- Dev deploy cache policy: auto-detect Supervisor metadata drift (`ingress`/`ports`) and auto-reinstall the App during `deploy:app:*`.
- Onboarding token UX policy refinement: on repeated macOS setup, empty relay token input reuses existing Keychain token before any auto-generation.
- Onboarding diagnostics policy refinement (superseded): classify Relay health failures by status (`401/403`, `404`, `000`) and surface `ha_ws_connected=false` as a non-blocking warning.
- Onboarding UX pattern policy: provide a public raw instruction entrypoint (`.codex/ONBOARDING.md`) so Codex can guide setup from one link/prompt.
- Onboarding UX pattern refinement: canonical one-link Codex entrypoint is `.codex/INSTALL.md`; `.codex/ONBOARDING.md` is alias-only.
- Onboarding command-surface policy: keep onboarding runtime commands minimal (`setup`, `doctor`, `env`) and remove client launch commands.
- Onboarding first-time policy: canonical `.codex/INSTALL.md` must include clone/install bootstrap for users without a local repo.
- Skill-install parity policy: Codex path must support one-time global skill install (`~/.agents/skills`) so daily startup uses plain `codex` without a custom launcher.
- Multi-client installer policy: one local installer must support Codex, Claude Code, and OpenCode skill-link targets (`install:skills`) with idempotent backups.
- Install-flow safety policy: canonical `.codex/INSTALL.md` must keep onboarding as setup+diagnostics only (no nested client launch flow).
- Skill portability policy: installed local skills are rendered as managed `SKILL.md` files with concrete repo root substitution instead of symlink-only paths.
- Skill contract policy (superseded): active skill docs use `RELAY_BASE_URL`/`RELAY_AUTH_TOKEN` and optional `HA_LLAT`; legacy `HA_TOKEN`/`RELAY_URL` terms are removed.
- Onboarding cleanup policy refinement: obsolete launcher flow is removed (`start`, `codex`, `claude`), keeping only `setup`, `doctor`, and `env`.
- Installer simplification policy: remove dedicated wrapper script and call `install-local-skills.sh` directly from npm shortcuts.
- Skill orchestration policy refinement: `ha-nova` is a strict router with mandatory `doctor` gate before any HA operation.
- Capability policy refinement for automation CRUD: prefer App-context Supervisor path when available; fallback to direct REST with `HA_LLAT`; otherwise stop and route to onboarding.
- Install UX standardization policy: Codex and Claude install docs must share the same section structure (`Quick Install`, `Verify`, `Troubleshooting`, `Security`).
- MVP validation policy: provide one contributor command (`smoke:app:mvp`) that enforces onboarding health gate and runs deterministic automation CRUD smoke with cleanup.
- Fresh-session readiness policy: add `onboarding:macos:quick` as default fast gate (doctor + installed skill marker) before heavy contributor checks.
- Quick-gate parsing policy: normalize installed skill marker parsing to strip HTML comment suffix (`-->`) before repo-root comparison.
- Live skill E2E policy: validate Codex behavior in black-box mode via `codex exec --json` instead of only shell smoke scripts.
- Live skill E2E realism policy: reject helper-smoke-script command execution and subagent delegation in this harness to match single-agent user behavior.
- Capability-aware E2E expectation policy (superseded): `e2e:skill:codex` used `E2E_EXPECT=auto` before mandatory-LLAT simplification.
- Live E2E delegation policy: subagent usage is allowed by default (`E2E_SUBAGENT_POLICY=allow`) to reflect realistic user sessions; strict single-agent mode remains opt-in (`deny`).
- Fast-path UX policy (user): the skill must always prefer the fastest viable capability path first.
- Session-check policy refinement: replace per-operation visible doctor flow with cached `ready --quiet` gate; show diagnostics only on failure.
- Entity discovery capability policy: `ha-entities` now prefers Relay `/ws` `get_states` and uses direct REST + `HA_LLAT` only as fallback.
- Auth policy override (user): `HA_LLAT` is mandatory baseline for MVP runtime and onboarding; no no-LLAT user path.
- Runtime policy override: remove `SUPERVISOR_TOKEN` fallback from upstream token resolution; runtime startup fails fast without LLAT.
- Onboarding policy override: `setup`, `doctor`, `ready`, and `env` must fail when LLAT is missing/invalid or Relay reports `ha_ws_connected=false`.
- Config-surface reduction policy (user): remove `ws_allowlist_append` from App/user settings to keep MVP configuration minimal.

## 2026-02-27

### Planning Defaults (Agent)
- E2E capability-proof policy: keep `e2e:skill:codex` assertions path-neutral (`/config/automation/config/{id}`, `/services/automation/reload`) so both App-context Supervisor and direct REST traces satisfy the same CRUD contract.
- Onboarding UX clarity policy: setup text must always make LLAT mandatory semantics explicit (`required`, `must match`) and avoid optional-sounding degraded-WS wording.
