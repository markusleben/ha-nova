# Cloud Source Materialization Window Follow-up

Status: locally implemented after the single-shot canary; no second canary run.

## Problem

Canary PR #457 reproduced the GitHub materialization race after PR #456. The
completed broker started from trusted `main`, but GitHub did not expose the
current pull request's merge commit and merge ref before the 60-second window
expired. Both appeared immediately after the broker emitted its fail-closed
rejection. The provisional App check was terminally rejected and no pending
check remained.

## Required behavior

- Keep one bounded retry loop inside the existing completed broker run.
- Use one 90-second acceptance deadline for current pull-request association,
  the full pull-request merge commit, and the exact
  `refs/pull/<number>/merge` ref.
- Keep the three-second retry cadence and derive the attempt cap from the
  window so both bounds cannot drift.
- Measure the acceptance deadline with a monotonic clock.
- Revalidate the CI attempt after materialization and immediately before
  every terminal result. An older attempt only cleans up its own pending
  checks, including when cleanup or policy execution fails.
- Reconcile accepted check creation through a short bounded visibility window
  when the GitHub API response is lost; repeat cleanup across that window when
  creation remains ambiguous.
- Preserve the existing fail-closed rejection, terminal immutability,
  provisional cleanup, command/API deadlines, and ten-minute workflow cap.
- Never accept a source that becomes consistent after the acceptance deadline.
- Cover association, merge-commit, absent-ref, inconsistent-ref, deadline, and
  rerun races after the previous 60-second attempt cap.
- Do not rerun the remote canary from this change.
