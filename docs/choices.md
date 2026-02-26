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
