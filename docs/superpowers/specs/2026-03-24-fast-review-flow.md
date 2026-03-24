# 2026-03-24 Fast Review Flow

## Summary

Adopt the faster safe review loop for release-bound and other high-risk PRs:
- fix
- targeted local verification
- immediate push
- immediate `@codex`
- subagent review and CI in parallel
- final merge/tag gate only on the exact last clean SHA

## Decision

- Do not serialize Codex review behind manual/subagent review during normal fix iteration.
- For the initial PR SHA and every later relevant SHA, request Codex immediately; if Codex does not produce a real result on that SHA, request it again on the same SHA instead of silently proceeding.
- Keep the strict final gate unchanged:
  - exact latest SHA
  - green required checks
  - real/current Codex result
  - two current clean subagent passes
  - resolved review threads

## Why

The prior flow was safe but slow because it stacked waiting time:
- local fix
- local review
- subagent review
- then Codex
- then CI

The faster safe shape overlaps those waits instead:
- local fix
- targeted verification
- push
- Codex + CI + subagent review in parallel

## Scope

- `AGENTS.md`
- `docs/releasing.md`

## Non-Goals

- No weakening of the final release/merge gate
- No replacement of subagent review with Codex alone
- No workflow-file changes; current GitHub workflow concurrency already exists
