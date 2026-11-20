# Spec: Public Winget Launch Prep + Active Contract Cleanup

Date: 2026-03-22

## Problem

Private Windows QA is now good enough to prove:

- local `winget --manifest` install
- first-run `setup`
- `doctor`
- uninstall

But the repo still lacks two things:

1. A clean maintainer handoff from release artifacts to a real `winget-pkgs` submission flow.
2. A few active QA/docs contracts still bias Windows testing toward the raw bundle/bootstrap path and make local winget QA easier to mis-run than it should be.

## Goal

Prepare the repo for the real public `winget` launch without prematurely switching public install docs.

## Decisions

1. Keep public Windows install docs on `install.ps1` until the community package is actually live.
2. Add a maintainer-facing release helper that stages the exact generated manifest payload for `winget-pkgs` submission.
3. Make the local validation harness rebuild and serve a local-manifest ZIP that points at the live local bundle URL automatically.
4. Print explicit Windows local-manifest QA commands from the harness so maintainers do not have to reconstruct them by hand.
5. Update active release/docs contracts to mention the new public handoff path and the local-manifest QA path.

## Non-Goals

- Do not switch README/client install docs to `winget install` yet.
- Do not pretend local-manifest QA proves final published-source `winget upgrade`.

## Expected Outcome

- Maintainers can take a tagged release and stage the exact `winget-pkgs` submission payload from the repo.
- Local Windows QA no longer drifts back to a GitHub-targeting manifest by accident.
- The active product contract stays honest: `install.ps1` remains public until the package is published and proven on a fresh Windows VM.
