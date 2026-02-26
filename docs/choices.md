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
- Phase 1b execution mode: skill-first (REST workflow skills + acceptance doc), no new bridge endpoints in this step.
- Branch for 1b: `feat/phase-1b-rest-skills` from `main`.

## 2026-02-26

### Planning Defaults (Agent)
- Upstream token policy: dual auth with deterministic fallback (`HA_LLAT` > addon option `ha_llat` > legacy `HA_TOKEN` > `SUPERVISOR_TOKEN`).
- Compatibility policy: keep `HA_TOKEN` required for existing bridge auth behavior, mark `HA_TOKEN` as deprecated only for upstream LLAT resolution.
- Dev ergonomics policy: provide idempotent LLAT seed command (`npm run seed:llat`) that validates options before write.
- Supervisor API write policy: merge existing addon options before posting update to avoid unintended option loss.
- Planning gate policy: no new plan without prior review of current official HA docs (developer + apps/add-ons + user docs).
- Runtime auth split policy: bridge endpoint auth token and upstream token selection are separated (`BRIDGE_AUTH_TOKEN` preferred for bridge auth; upstream fallback uses `HA_TOKEN` only when bridge auth is explicitly separated).
- Runtime capability policy: if upstream auth is limited/none, bridge stays up and returns explicit WS error instead of crashing startup.
