const queueAttempts = 3;
const delayMs = 3_000;
const pullRequestWindowMs = 90_000;
const pullRequestAttempts = Math.floor(pullRequestWindowMs / delayMs) + 1;

function wait(delay = delayMs) {
  return new Promise((resolve) => setTimeout(resolve, delay));
}

export function createCloudSourceConsistencyResolver({
  currentPullRequest,
  mergeCommitParents,
  now = () => performance.now(),
  requireSHA,
  resolveRemoteRef,
  waitFor = wait,
}) {
  const unresolvedAPIMismatchIdentities = new Set();

  async function matchesPullRequestSource(pull, headSHA, targetSHA) {
    const identity = `${pull.number}:${requireSHA(
      pull.base?.sha,
      "pull request base SHA",
    )}:${headSHA}`;
    if (pull.merge_commit_sha != null) {
      const matches =
        requireSHA(pull.merge_commit_sha, "pull request merge commit SHA") ===
        targetSHA;
      if (matches) {
        unresolvedAPIMismatchIdentities.delete(identity);
      } else {
        unresolvedAPIMismatchIdentities.add(identity);
      }
      return matches;
    }
    if (unresolvedAPIMismatchIdentities.has(identity)) {
      return "api-mismatch";
    }
    const parents = await mergeCommitParents(targetSHA);
    if (parents === undefined) {
      return undefined;
    }
    return (
      parents.length === 2 &&
      parents[0] === requireSHA(pull.base?.sha, "pull request base SHA") &&
      parents[1] === headSHA
    );
  }

  async function resolvePullRequestSource(headSHA) {
    const deadline = now() + pullRequestWindowMs;
    let reason = "workflow run no longer identifies a current pull request";
    for (let attempt = 1; attempt <= pullRequestAttempts; attempt += 1) {
      if (attempt > 1 && now() > deadline) {
        break;
      }
      const pull = await currentPullRequest(headSHA);
      if (pull === undefined) {
        reason = "workflow run no longer identifies a current pull request";
      } else {
        const sourceRef = `refs/pull/${pull.number}/merge`;
        const refSHA = resolveRemoteRef(sourceRef);
        if (refSHA === undefined) {
          reason = "pull request merge ref is not materialized yet";
        } else {
          const matches = await matchesPullRequestSource(pull, headSHA, refSHA);
          if (matches === undefined) {
            reason = "pull request merge ref commit is not materialized yet";
          } else if (matches === "api-mismatch") {
            reason =
              "pull request API and merge ref are temporarily inconsistent";
          } else if (!matches) {
            reason =
              pull.merge_commit_sha == null
                ? "pull request merge ref does not match the current base and head"
                : "pull request API and merge ref are temporarily inconsistent";
          } else if (now() > deadline) {
            reason =
              "pull request source materialized after the bounded deadline";
          } else {
            return { pull, sourceRef, targetSHA: refSHA };
          }
        }
      }
      const remainingMs = deadline - now();
      if (attempt < pullRequestAttempts && remainingMs > 0) {
        await waitFor(Math.min(delayMs, remainingMs));
      } else {
        break;
      }
    }
    return { kind: "timeout", reason };
  }

  async function resolveQueueSource(sourceRef, headSHA) {
    let reason = "merge queue ref is no longer current";
    for (let attempt = 1; attempt <= queueAttempts; attempt += 1) {
      const refSHA = resolveRemoteRef(sourceRef);
      if (refSHA === undefined) {
        reason = "merge queue ref is no longer current";
      } else if (refSHA !== headSHA) {
        reason = "merge queue ref moved before source verification";
      } else {
        return { targetSHA: refSHA };
      }
      if (attempt < queueAttempts) {
        await wait();
      }
    }
    return { kind: "stale", reason };
  }

  return {
    matchesPullRequestSource,
    resolvePullRequestSource,
    resolveQueueSource,
  };
}
