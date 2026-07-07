Status: active

# Helper Refactor Recovery

## Scope

- recover the missing repo-dev helper extraction from a dirty Codex worktree into a clean branch
- keep the slice limited to repo-dev install/sync helpers, private desktop validation helpers, and the active docs/tests that now depend on them
- do not pull unrelated workflow or branch-protection policy changes into this branch

## Recovery Rule

- treat `/Users/markus/.codex/worktrees/70b3/ha-nova` as the source of truth for the validated helper files
- verify the recovered branch with targeted onboarding/docs contracts before preparing the PR
