# Choices

## 2026-02-25

### User Decisions (Brainstorm Q1-Q10)
- Priorität: Balance (`Demo + API + Skill`).
- DoD: Kombi-DoD mit reduziertem Scope.
- Ziel-Client: client-agnostisch.
- Security-Härte in 1a: dev-first.
- Runtime-Qualität: funktional + sichere Fehler.
- `ha-onboarding.md`: ultra-lean.
- Onboarding auth: user-generated LLT mandatory (`HA_TOKEN`), supervisor token not onboarding credential.
- `ha-safety.md`: core-only.
- `ha-nova.md`: nur aktive 1a/1b Skills.
- Teststrategie: leicht automatisiert (Vitest).
- Ergebnisformat: eine Plan-Datei.

### Planning Defaults (Agent)
- Planstruktur: Vertical-Slice-Ansatz (empfohlen gegenüber infra-first/skill-first).
- Fehlervertrag: einheitlicher JSON-Error-Envelope über alle Endpoints.
- Execution workspace: Worktree-Skill fallback genutzt (Repo ohne Commit/HEAD), daher Feature-Branch `feat/phase-1a-foundation` im aktuellen Workspace.
- GitHub owner for remote operations: canonical API login `markusleben` used (rename from `w0nk1`).
- Branch strategy after repo bootstrap: set `main` as default branch, merge PR #1 into `main`, then apply post-merge wiring fix directly on `main`.
- Phase 1b execution mode: skill-first (REST workflow skills + acceptance doc), no new relay endpoints in this step.
- Branch for 1b: `feat/phase-1b-rest-skills` from `main`.

## 2026-02-26

### Planning Defaults (Agent)
- Upstream token policy: dual auth with deterministic fallback (`HA_LLAT` > addon option `ha_llat` > legacy `HA_TOKEN` > `SUPERVISOR_TOKEN`).
- Compatibility policy: keep `HA_TOKEN` required for existing relay auth behavior, mark `HA_TOKEN` as deprecated only for upstream LLAT resolution.
- Dev ergonomics policy: provide idempotent LLAT seed command (`npm run seed:llat`) that validates options before write.
- Supervisor API write policy: merge existing addon options before posting update to avoid unintended option loss.
- Planning gate policy: no new plan without prior review of current official HA docs (developer + apps/add-ons + user docs).
- Runtime auth split policy: relay endpoint auth token and upstream token selection are separated (`RELAY_AUTH_TOKEN` preferred for relay auth; upstream fallback uses `HA_TOKEN` only when relay auth is explicitly separated).
- Runtime capability policy: if upstream auth is limited/none, relay stays up and returns explicit WS error instead of crashing startup.
- Phase-1a.3 planning policy: addon/app packaging keeps shell runner thin (`addon/run`), with all auth precedence in Node runtime only (KISS + DRY).
- Security baseline for packaging: require explicit `relay_auth_token` in addon options; keep `ha_llat` optional persistent capability token.
- Terminology policy (user): prefer "App" wording in docs/UX communication; keep legacy "addon" only where technical HA interfaces enforce it.
- Architecture policy (user): MVP first, but always modular-by-design to minimize future adaptation cost.
- Skill policy (user): skills remain markdown-only (`*.md`), while relay stays lean and efficient.
- Clean-slate policy (user): no backward-compatibility fallbacks in this non-public phase; remove legacy token paths and aliases instead of preserving them.
- Env contract policy: relay auth requires `RELAY_AUTH_TOKEN`; no `HA_TOKEN` fallback.
- Naming policy: internal runtime/env naming uses `app_*` (`APP_OPTIONS_PATH`, `app_option_ha_llat`) while keeping `/addons/...` only for Supervisor API endpoints.
- Live validation policy: ship one deterministic HA E2E runner (`smoke:app:e2e`) that validates Supervisor options flow and relay `/health` + `/ws` in one command.
- Contributor deploy policy: provide two explicit HA deploy modes — `deploy:app:fast` for normal iteration and `deploy:app:clean` for aggressive cache-busting rebuilds.
- Contributor env policy: keep secrets in untracked `.env.local` (or `.env`), ship committed `.env.example`; script env files are convenience only and must not override explicitly exported shell vars.
- E2E policy refinement: `SUPERVISOR_TOKEN` is optional for runtime-only smoke and mandatory only for Supervisor option preflight / `--apply`.
