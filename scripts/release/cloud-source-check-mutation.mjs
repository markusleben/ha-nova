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
}) {
  let creationError;
  try {
    return await create();
  } catch (error) {
    creationError = error;
  }

  let reconciledPending;
  let reconciledTerminal;
  for (let attempt = 1; attempt <= reconcileAttempts; attempt += 1) {
    let matches;
    try {
      matches = await listMatches();
    } catch (error) {
      creationError = error;
      matches = [];
    }
    if (matches.length === 1) {
      const [match] = matches;
      if (match.status === "completed") {
        reconciledTerminal = match;
      } else {
        reconciledPending = match;
      }
    }
    if (matches.length > 1) {
      const terminals = matches.filter(
        (candidate) => candidate.status === "completed",
      );
      if (terminals.length === 1) {
        await cleanupPending();
        return terminals[0];
      }
      throw new AmbiguousSourceCheckMutationError(
        "ambiguous source-check creation produced multiple matching checks",
        { cause: creationError },
      );
    }
    if (attempt < reconcileAttempts) {
      await wait();
    }
  }
  if (reconciledTerminal !== undefined) {
    await cleanupPending();
    return reconciledTerminal;
  }
  if (reconciledPending !== undefined) {
    return reconciledPending;
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
