# Contributing

Thanks for contributing to `ha-nova`.

## Project Principles

- MVP first.
- KISS.
- Relay stays dumb; Skills stay smart.
- English-only docs, code comments, and commit messages.

## Quick Start

```bash
npm ci
npm run typecheck
npm test
```

## Branch + Commit Style

- Use conventional commit types:
  - `feat`, `fix`, `refactor`, `build`, `ci`, `chore`, `docs`, `style`, `perf`, `test`
- Keep changes focused and reviewable.
- Avoid repo-wide search/replace sweeps.

## Pull Requests

Before opening a PR:
1. Run `npm run typecheck`.
2. Run `npm test`.
3. Update docs if behavior changed.
4. Add or update tests for bug fixes when practical.

PRs should explain:
- problem
- solution
- risk
- verification steps

## Codex PR Automation

- Automatic Codex review comments are disabled to reduce notification noise.
- Optional Codex auto-fix requests are failure-driven and label-gated via `.github/workflows/pr-review-watchdog.yml`.
- For manual Codex review, comment `@codex review` on the PR.
- To enable CI-failure auto-fix requests on a PR, add label: `autofix:enabled`.
- To disable auto-fix requests, remove label: `autofix:enabled`.

## Architecture Guardrails

- No business logic in relay transport handlers.
- Keep skills as plain markdown (`*.md`).
- Prefer small files; split if complexity grows.

## Security

Do not open public issues for vulnerabilities.
Use `SECURITY.md` reporting guidance.
