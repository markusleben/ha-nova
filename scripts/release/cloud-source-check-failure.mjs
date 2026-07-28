export async function handleCloudSourceCheckFailure({
  activeWorkflowRun,
  ambiguousMutation,
  checkId,
  completeCheck,
  deletePendingAttemptChecks,
  deletePendingAttemptChecksEventually,
  deletePendingCheck,
  reportedFailure,
  requireTrustedCI,
  staleAttemptObserved,
  terminalCheckReported,
}) {
  if (reportedFailure) {
    return 0;
  }

  async function cleanupActiveAttempt(context) {
    if (activeWorkflowRun === undefined) {
      return true;
    }
    try {
      await deletePendingAttemptChecks(activeWorkflowRun);
      return true;
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "unexpected pending-check cleanup failure";
      console.error(`[run-cloud-source-check] ERROR: ${context}: ${message}`);
      return false;
    }
  }

  if (staleAttemptObserved) {
    const cleaned = await cleanupActiveAttempt(
      "cannot clean the stale CI attempt",
    );
    return cleaned ? 0 : 1;
  }
  if (ambiguousMutation) {
    if (activeWorkflowRun !== undefined) {
      try {
        await deletePendingAttemptChecksEventually(activeWorkflowRun);
      } catch (error) {
        const message =
          error instanceof Error
            ? error.message
            : "unexpected pending-check cleanup failure";
        console.error(
          `[run-cloud-source-check] ERROR: cannot reconcile pending state after an ambiguous mutation: ${message}`,
        );
      }
    }
    return 1;
  }
  if (checkId === undefined || terminalCheckReported) {
    await cleanupActiveAttempt(
      checkId === undefined
        ? "cannot clean pending state after a pre-check failure"
        : "terminal result reported, but pending sibling cleanup failed",
    );
    return 1;
  }

  let failureCI;
  try {
    failureCI = await requireTrustedCI(activeWorkflowRun);
  } catch (error) {
    const message =
      error instanceof Error
        ? error.message
        : "unexpected CI lifecycle refresh failure";
    console.error(
      `[run-cloud-source-check] ERROR: cannot refresh CI before rejection: ${message}`,
    );
    await cleanupActiveAttempt(
      "cannot clean pending state after CI refresh failure",
    );
    return 1;
  }
  if (failureCI.staleAttempt) {
    const cleaned = await cleanupActiveAttempt(
      "cannot clean the stale CI attempt before rejection",
    );
    return cleaned ? 0 : 1;
  }

  try {
    await completeCheck(
      checkId,
      "failure",
      "Trusted source verification failed. Inspect the linked workflow run.",
    );
  } catch (error) {
    const message =
      error instanceof Error
        ? error.message
        : "unexpected source-check reporting failure";
    console.error(
      `[run-cloud-source-check] ERROR: cannot report rejection: ${message}`,
    );
    try {
      await deletePendingCheck(checkId);
    } catch (cleanupError) {
      const cleanupMessage =
        cleanupError instanceof Error
          ? cleanupError.message
          : "unexpected pending-check cleanup failure";
      console.error(
        `[run-cloud-source-check] ERROR: cannot delete pending rejection: ${cleanupMessage}`,
      );
    }
    return 1;
  }
  try {
    await deletePendingAttemptChecks(activeWorkflowRun, checkId);
  } catch (error) {
    const message =
      error instanceof Error
        ? error.message
        : "unexpected pending-check cleanup failure";
    console.error(
      `[run-cloud-source-check] ERROR: rejection reported, but pending sibling cleanup failed: ${message}`,
    );
    return 1;
  }
  return 0;
}
