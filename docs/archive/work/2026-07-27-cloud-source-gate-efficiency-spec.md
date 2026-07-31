# Cloud Source Gate Efficiency Fix

Status: merged — follow-up canary completed on 2026-07-28

## Problem

The trusted broker ran for `requested`, `in_progress`, and `completed` CI
lifecycle events. GitHub can deliver those events before a pull-request merge
ref exists and can finish stale events after a pull request changes or closes.
Those safe, expected races became red workflow runs and generated duplicate
notifications.

## Required behavior

- Trigger the broker for `in_progress` and `completed` CI deliveries. Never use
  `requested`, because GitHub does not emit it consistently for reruns.
- `in_progress` creates only an attempt-bound provisional pending App check on
  the CI head. It does not resolve a PR, fetch a merge ref, or run policy code.
- `completed` creates the exact target-bound pending check before deleting the
  provisional check. It repeats provisional cleanup after terminal success so
  a delayed early delivery cannot strand state.
- A non-successful upstream CI run or stale pull request exits successfully
  without creating an App check and retires only its pending provisional
  state. Persistent source materialization failure produces one App rejection.
- Once an App check exists, any source or policy verification failure completes
  that check as `failure` and exits the broker workflow successfully. One
  security rejection produces one visible failure.
- Failure to authenticate the dedicated App or failure before a check can be
  created remains a broker workflow failure.
- Duplicate deliveries and rerun attempts remain idempotent and attempt-bound.
- A terminal App result is immutable for one upstream run id and attempt.
  Retrying requires a new CI attempt. Broker deliveries for the same run and
  attempt are serialized; stale deliveries from an older attempt only clean up
  that older attempt.
- Local tests cover completed, cancelled, stale, duplicate, rerun, missing-ref,
  and permission-boundary cases before one monitored remote canary is allowed.
- The commit-association endpoint selects one current PR only. Security fields,
  including `merge_commit_sha`, come from a subsequent full PR response.
  Temporarily omitted or null merge materialization is retried inside the same
  bounded run.
- The completed broker allows one bounded materialization window long enough
  for an observed post-CI GitHub merge-ref delay. If the source is still not
  materialized, it completes the attempt's provisional check as a rejection,
  or creates one fail-safe rejection if no provisional check exists. The
  broker itself remains successful and the App check remains fail-closed.
- A delayed `in_progress` delivery observed after its CI run is already
  complete never creates, deletes, or recreates a pending check. The completed
  delivery exclusively owns finalization and provisional cleanup.

## Actions budget

No push or rerun may be used as a debugging loop. A remote canary is
single-shot, is cancelled at its first unexpected result, and must leave no
active workflows after closure.

The first materialization canary reached the bounded retry limit after CI and
left its provisional App check pending while the broker exited successfully.
The workflow was disabled immediately, the canary was closed without merge,
and no rerun was used for debugging.

GitHub API calls, remote-ref resolution, and policy subprocesses each have a
finite deadline. The mode job has a three-minute limit; the reporter has a
ten-minute limit with cleanup margin. A failed fail-safe completion deletes
the pending check before surfacing infrastructure failure.

## Dependabot trigger containment

The local PR canary exposed an existing failure in the Dependabot safe
auto-merge trigger:

- Downloaded ES modules must retain a `.mjs` filename before Node executes
  them.
- Human branches and unrelated check names must skip the resolver job before
  it downloads policy or script files.
- These prefilters only reject irrelevant events. The trusted default-branch
  resolver remains the authorization boundary for every accepted event.
