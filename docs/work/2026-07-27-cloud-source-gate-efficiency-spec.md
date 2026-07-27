# Cloud Source Gate Efficiency Fix

Status: locally verified; one monitored remote canary remains pending.

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
- A non-successful upstream CI run, stale pull request, missing merge ref, or
  temporarily absent pull-request merge SHA exits successfully without
  creating an App check. The missing required check remains fail-closed.
- Once an App check exists, any source or policy verification failure completes
  that check as `failure` and exits the broker workflow successfully. One
  security rejection produces one visible failure.
- Failure to authenticate the dedicated App or failure before a check can be
  created remains a broker workflow failure.
- Duplicate deliveries and rerun attempts remain idempotent and attempt-bound.
- Local tests cover completed, cancelled, stale, duplicate, rerun, missing-ref,
  and permission-boundary cases before one monitored remote canary is allowed.
- The commit-association endpoint selects one current PR only. Security fields,
  including `merge_commit_sha`, come from a subsequent full PR response.
  Temporarily omitted or null merge materialization is retried inside the same
  bounded run.

## Actions budget

No push or rerun may be used as a debugging loop. A remote canary is
single-shot, is cancelled at its first unexpected result, and must leave no
active workflows after closure.

## Dependabot trigger containment

The local PR canary exposed an existing failure in the Dependabot safe
auto-merge trigger:

- Downloaded ES modules must retain a `.mjs` filename before Node executes
  them.
- Human branches and unrelated check names must skip the resolver job before
  it downloads policy or script files.
- These prefilters only reject irrelevant events. The trusted default-branch
  resolver remains the authorization boundary for every accepted event.
