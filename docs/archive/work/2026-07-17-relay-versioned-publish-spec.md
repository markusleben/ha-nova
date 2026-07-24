# Spec: Versioned Relay Image Publish

Status: merged — #372
Date: 2026-07-17

## Problem

`.github/workflows/relay-image.yml` tries to publish on every relay-source push to main and fails the run when the version tag from `nova/config.yaml` is already published. A multi-PR train of relay-source changes without a version bump therefore turns the workflow red on main after every merge, and pull requests get no image build or smoke signal at all.

## Decision

- `pull_request` and `workflow_dispatch` runs are always verify-only: build both platforms, smoke test the amd64 image, and never touch registry credentials or tags.
- A push to main publishes only when the version read from `nova/config.yaml` has no published GHCR tag yet (the first run after a version bump). Source-only merges stay green as verify-only runs.
- Recovery of a failed publish is a re-run of that version-bump push run: the decide step re-checks the registry at run time. No other publish path exists.
- `:latest` is promoted before the version tag so an interrupted promotion self-heals on the next publish-path run instead of stranding `:latest` on the old digest.
- The smoke test moves to `scripts/ci/relay-smoke.sh`, shared verbatim by the verify and publish paths, and now removes its container on failure too.

## Acceptance

- Relay-source pull request: build + smoke run, no registry login, no tags pushed.
- Merge without a version bump: green verify-only run on main.
- Merge with a new version in `nova/config.yaml`: candidate push, smoke, promotion to `:latest` and `:<version>`.
- `workflow_dispatch` from any ref: verify-only.
- Re-run of a failed publish run still publishes as long as the version tag is absent.
