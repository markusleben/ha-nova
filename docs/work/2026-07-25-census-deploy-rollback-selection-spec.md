# Census Deploy Rollback Selection

Date: 2026-07-25
Status: active

## Goal

Ensure a failed Census Worker deployment rolls back to the deployment that was
serving immediately before the attempted deploy.

## Scope

- Read the active deployment through Wrangler's dedicated
  `deployments status --json` command instead of inferring it from list order.
- Use the same current-state command to verify the restored deployment.
- Require exactly one status object with one safe 100-percent version ID.
- Keep deployment-state parsing in a focused helper so the release wrapper
  remains below the repository's approximate 400-line limit.
- Skip rollback when production never changed, and refuse rollback when a
  different deployment became active after this process deployed.
- Add a regression fixture matching Wrangler 4.113.0's oldest-first list
  order.

## Acceptance

- The rollback path does not use the oldest-first deployment list.
- The current 100-percent version comes from `deployments status --json`.
- A post-deploy verification failure rolls back to that version.
- The restored deployment must be the newest 100-percent deployment and match
  the selected rollback version.
- A failed deploy that leaves production unchanged does not create a rollback
  deployment.
- A deploy that changes production but loses or malforms local output still
  restores the baseline under the required single-writer invariant.
- When Wrangler identifies this run's deployed version, cleanup does not
  overwrite a different active version.
- Existing release-gate behavior remains green.

## Non-goals

- No Worker runtime or Census data changes.
- No Wrangler version change.
- No production deployment.
- No distributed deployment lock; correctness for indistinguishable
  concurrent writes depends on the documented single-writer operation.
