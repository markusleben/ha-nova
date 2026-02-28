# PR Review Watchdog Design

Date: 2026-02-28

## Goal

Automate Codex PR review and optional Codex auto-fix requests with minimal workflow complexity.

## Scope (MVP)

1. Trigger Codex review comment automatically for each new PR head SHA.
2. Trigger Codex auto-fix request automatically when required CI workflow fails.
3. Keep auto-fix opt-in via PR label gate.

## Safety

- No repository checkout required.
- No code execution from PR content.
- Use `pull_request_target` for comment automation only.
- Auto-fix requests only when label `autofix:enabled` is present.
- De-duplicate comments via HTML marker tags.

## Out of Scope

- No autonomous merge.
- No broad issue triage automation.
- No multi-agent orchestration.

## Deliverables

- `.github/workflows/pr-review-watchdog.yml`
- `CONTRIBUTING.md` section documenting label-gated auto-fix
- `docs/choices.md` + `docs/breadcrumbs.md` updates

## Verification

1. Workflow YAML exists and is syntactically valid.
2. Local checks: `npm run typecheck`, `npm test`.
3. PR behavior:
   - comment `@codex review` for new head SHA
   - comment `@codex fix ...` on CI failure only when `autofix:enabled` label exists
