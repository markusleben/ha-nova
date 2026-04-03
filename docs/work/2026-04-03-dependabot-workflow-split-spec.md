# Spec: split Dependabot safe auto-merge workflow

Date: 2026-04-03

Problem:
- `.github/workflows/dependabot-safe-auto-merge.yml` currently mixes `pull_request_target` and `workflow_run` handling.
- Since commit `1b37854`, GitHub creates 0-job `push` failure runs for this workflow.
- Historical evidence shows prior healthy `workflow_run` runs and new post-#150 `push` failures with no jobs/logs.

Decision:
- Split the workflow into two event-specific files.
- Keep PR-side approval/labeling logic in a `pull_request_target` workflow.
- Keep post-check auto-merge enabling logic in a `workflow_run` workflow.

Implementation:
- Add `.github/workflows/dependabot-safe-lane-prepare.yml` for `pull_request_target`.
- Restrict `.github/workflows/dependabot-safe-auto-merge.yml` to `workflow_run` only.
- Update workflow contract tests to assert the split and preserve policy/behavior guarantees.

Verification:
- Run targeted workflow contract tests.
- Run `npm run test:safe` if the delta stays small enough to rely on pre-push later.
- After push, verify that the old 0-job push failures stop recurring for the split workflows.
