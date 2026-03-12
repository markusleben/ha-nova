# Spec: Codex Review Signal Flow

Date: 2026-03-12

## Problem

The PR merge flow already checks GitHub checks, bot reactions, and inline PR review comments, but Codex can also report a clean result through PR discussion comments like `Codex Review: Didn't find any major issues.` If that channel is ignored, a human can miss the final clean signal or misread a pending gate as unresolved review work.

## Decision

Treat Codex review as a multi-signal check:

- workflow status via `gh pr checks`
- bot reactions via `issues/<nr>/reactions`
- inline findings via `pulls/<nr>/comments`
- summary / clean comments via `issues/<nr>/comments` or `gh pr view --comments`

## Scope

- `AGENTS.md`
- local project notes (`docs/choices.md`, `docs/breadcrumbs.md`)

## Non-Goals

- no automation script yet
- no change to branch protection itself
