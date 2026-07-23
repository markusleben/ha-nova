# Worktree Bootstrap and Git Hook Isolation

Date: 2026-07-23
Status: implementation

## Problem

Fresh Codex worktrees do not contain ignored dependency directories, so release
verification repeatedly starts with a missing `tsc` binary.

Git also exports repository-local `GIT_*` variables to hooks. The pre-push hook
passes those variables into tests that create temporary repositories, allowing
their Git commands to target the source repository instead.

## Contract

- Every new Codex worktree runs both lockfile-backed dependency installs before
  work starts.
- Bootstrap rejects symlinked, identical, or structurally invalid source and
  worktree roots before invoking npm.
- Codex's managed `.worktreeinclude` mechanism carries `.env` and `.env.local`
  into local managed worktrees without following source symlinks or
  overwriting existing destinations.
- The pre-push hook clears Git's repository-local environment variables before
  invoking project commands.
- A behavioral regression test proves child commands cannot observe the
  inherited repository-local Git environment.
- A behavioral regression test proves bootstrap path validation, install
  ordering, fail-fast behavior, and a clean rerun after partial failure.

## Non-goals

- No release or runtime behavior changes.
- No global machine setup.
- No dependency or package-manager changes.

## Source

- [Codex local environments and managed worktrees](https://learn.chatgpt.com/docs/environments/git-worktrees#copy-ignored-local-files-into-managed-worktrees)
