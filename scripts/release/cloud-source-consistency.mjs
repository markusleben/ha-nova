const attempts = 3;
const delayMs = 1_000;

function wait() {
  return new Promise((resolve) => setTimeout(resolve, delayMs));
}

export function createCloudSourceConsistencyResolver({
  currentPullRequest,
  requireSHA,
  resolveRemoteRef,
}) {
  async function resolvePullRequestSource(headSHA) {
    let reason = "workflow run no longer identifies a current pull request";
    for (let attempt = 1; attempt <= attempts; attempt += 1) {
      const pull = await currentPullRequest(headSHA);
      if (pull === undefined) {
        reason = "workflow run no longer identifies a current pull request";
      } else if (pull.merge_commit_sha === null) {
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
        } else {
          return { pull, sourceRef, targetSHA: refSHA };
        }
      }
      if (attempt < attempts) {
        await wait();
      }
    }
    return { reason };
  }

  async function resolveQueueSource(sourceRef, headSHA) {
    let reason = "merge queue ref is no longer current";
    for (let attempt = 1; attempt <= attempts; attempt += 1) {
      const refSHA = resolveRemoteRef(sourceRef);
      if (refSHA === undefined) {
        reason = "merge queue ref is no longer current";
      } else if (refSHA !== headSHA) {
        reason = "merge queue ref moved before source verification";
      } else {
        return { targetSHA: refSHA };
      }
      if (attempt < attempts) {
        await wait();
      }
    }
    return { reason };
  }

  return { resolvePullRequestSource, resolveQueueSource };
}
