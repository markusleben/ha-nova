const queueAttempts = 3;
const delayMs = 3_000;
const pullRequestWindowMs = 90_000;
const pullRequestAttempts = Math.floor(pullRequestWindowMs / delayMs) + 1;

function wait(delay = delayMs) {
  return new Promise((resolve) => setTimeout(resolve, delay));
}

export function createCloudSourceConsistencyResolver({
  currentPullRequest,
  now = () => performance.now(),
  requireSHA,
  resolveRemoteRef,
  waitFor = wait,
}) {
  async function resolvePullRequestSource(headSHA) {
    const deadline = now() + pullRequestWindowMs;
    let reason = "workflow run no longer identifies a current pull request";
    for (let attempt = 1; attempt <= pullRequestAttempts; attempt += 1) {
      if (attempt > 1 && now() >= deadline) {
        break;
      }
      const pull = await currentPullRequest(headSHA);
      if (pull === undefined) {
        reason = "workflow run no longer identifies a current pull request";
      } else if (pull.merge_commit_sha == null) {
        reason = "pull request merge commit is not materialized yet";
      } else {
        const mergeSHA = requireSHA(
          pull.merge_commit_sha,
          "pull request merge commit SHA",
        );
        const sourceRef = `refs/pull/${pull.number}/merge`;
        const refSHA = resolveRemoteRef(sourceRef);
        if (refSHA === undefined) {
          reason = "pull request merge ref is not materialized yet";
        } else if (refSHA !== mergeSHA) {
          reason =
            "pull request API and merge ref are temporarily inconsistent";
        } else if (now() > deadline) {
          reason =
            "pull request source materialized after the bounded deadline";
        } else {
          return { pull, sourceRef, targetSHA: refSHA };
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

  return { resolvePullRequestSource, resolveQueueSource };
}
