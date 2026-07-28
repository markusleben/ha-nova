const reconcileAttempts = 3;
const reconcileDelayMs = 1_000;

function wait(delay = reconcileDelayMs) {
  return new Promise((resolve) => setTimeout(resolve, delay));
}

export class AmbiguousSourceCheckMutationError extends Error {}

export async function createCheckWithReconciliation({
  cleanupPending,
  create,
  listMatches,
  retainPendingOnTerminalConflict = false,
  waitFor = wait,
}) {
  let creationError;
  try {
    return await create();
  } catch (error) {
    creationError = error;
  }

  const observedMatches = new Map();
  const observedTerminalConclusions = new Set();
  for (let attempt = 1; attempt <= reconcileAttempts; attempt += 1) {
    let matches;
    try {
      matches = await listMatches();
    } catch (error) {
      creationError = error;
      matches = [];
    }
    for (const match of matches) {
      if (match.status === "completed") {
        observedTerminalConclusions.add(match.conclusion);
      }
      const observed = observedMatches.get(match.id);
      if (observed?.status !== "completed" || match.status === "completed") {
        observedMatches.set(match.id, match);
      }
    }
    if (attempt < reconcileAttempts) {
      await waitFor();
    }
  }
  const observed = [...observedMatches.values()];
  const terminals = observed
    .filter((candidate) => candidate.status === "completed")
    .sort((left, right) => left.id - right.id);
  const pending = observed.filter(
    (candidate) => candidate.status !== "completed",
  );
  const terminalConclusions = observedTerminalConclusions;
  if (terminals.length > 0 && terminalConclusions.size === 1) {
    await cleanupPending();
    return terminals.at(-1);
  }
  if (
    retainPendingOnTerminalConflict &&
    terminalConclusions.size > 1 &&
    pending.length === 1
  ) {
    return pending[0];
  }
  if (terminalConclusions.size > 1 || pending.length > 1) {
    throw new AmbiguousSourceCheckMutationError(
      "ambiguous source-check creation produced multiple matching checks",
      { cause: creationError },
    );
  }
  if (pending.length === 1) {
    return pending[0];
  }
  throw new AmbiguousSourceCheckMutationError(
    "source-check creation remained invisible after bounded reconciliation",
    { cause: creationError },
  );
}

export async function retryPendingCleanup(cleanupPending) {
  let finalError;
  for (let attempt = 1; attempt <= reconcileAttempts; attempt += 1) {
    try {
      await cleanupPending();
      finalError = undefined;
    } catch (error) {
      finalError = error;
    }
    if (attempt < reconcileAttempts) {
      await wait();
    }
  }
  if (finalError !== undefined) {
    throw finalError;
  }
}
