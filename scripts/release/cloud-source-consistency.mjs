const pullRequestAttempts = 21;
const pullRequestAssociationAttempts = 3;
const queueAttempts = 3;
const delayMs = 3_000;
const pullRequestWindowMs = 60_000;

function wait(delay = delayMs) {
  return new Promise((resolve) => setTimeout(resolve, delay));
}

export function createCloudSourceConsistencyResolver({
  currentPullRequest,
  requireSHA,
  resolveRemoteRef,
}) {
  async function resolvePullRequestSource(headSHA) {
    const deadline = Date.now() + pullRequestWindowMs;
    let reason = "workflow run no longer identifies a current pull request";
    let associationMisses = 0;
    for (let attempt = 1; attempt <= pullRequestAttempts; attempt += 1) {
      const pull = await currentPullRequest(headSHA);
      if (pull === undefined) {
        reason = "workflow run no longer identifies a current pull request";
        associationMisses += 1;
        if (associationMisses >= pullRequestAssociationAttempts) {
          return { kind: "stale", reason };
        }
      } else if (pull.merge_commit_sha == null) {
        associationMisses = 0;
        reason = "pull request merge commit is not materialized yet";
      } else {
        associationMisses = 0;
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
        } else {
          return { pull, sourceRef, targetSHA: refSHA };
        }
      }
      const remainingMs = deadline - Date.now();
      if (attempt < pullRequestAttempts && remainingMs > 0) {
        await wait(Math.min(delayMs, remainingMs));
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
