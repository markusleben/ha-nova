import { spawnSync } from "node:child_process";

export function createCloudSourcePullRequestReader({
  apiTimeoutMs,
  fail,
  github,
  githubResponse,
  githubToken,
  repository,
  requireSHA,
  workspace,
}) {
  function resolveRemoteRef(sourceRef) {
    const result = spawnSync(
      "git",
      ["ls-remote", "--refs", "origin", sourceRef],
      {
        cwd: workspace,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "inherit"],
        timeout: apiTimeoutMs,
      },
    );
    if (result.error !== undefined || result.status !== 0) {
      fail(`cannot resolve remote source ref ${sourceRef}`);
    }
    if (result.stdout.trim().length === 0) {
      return undefined;
    }
    const fields = result.stdout.trim().split(/\s+/);
    if (
      fields.length !== 2 ||
      fields[1] !== sourceRef ||
      !/^[0-9a-f]{40}$/.test(fields[0])
    ) {
      fail(`remote source ref ${sourceRef} did not resolve exactly once`);
    }
    return fields[0];
  }

  async function currentPullRequest(headSHA) {
    const candidates = await github(
      `repos/${repository}/commits/${headSHA}/pulls`,
      githubToken,
    );
    const matches = candidates.filter(
      (pull) =>
        pull.state === "open" &&
        pull.base?.ref === "main" &&
        pull.base?.repo?.full_name === repository &&
        pull.head?.sha === headSHA,
    );
    if (matches.length !== 1) {
      if (matches.length === 0) {
        return undefined;
      }
      fail("workflow run must identify exactly one current pull request");
    }
    const association = matches[0];
    if (!Number.isSafeInteger(association.number) || association.number <= 0) {
      fail("associated pull request number must be a positive integer");
    }
    const pull = await github(
      `repos/${repository}/pulls/${association.number}`,
      githubToken,
    );
    if (
      pull.number !== association.number ||
      pull.state !== "open" ||
      pull.base?.ref !== "main" ||
      pull.base?.repo?.full_name !== repository ||
      pull.head?.sha !== headSHA
    ) {
      return undefined;
    }
    return pull;
  }

  async function mergeCommitParents(mergeSHA) {
    const endpoint = `repos/${repository}/git/commits/${mergeSHA}`;
    const response = await githubResponse(endpoint, githubToken);
    if (response.status === 404) {
      return undefined;
    }
    if (!response.ok) {
      fail(`GitHub API ${endpoint} returned HTTP ${response.status}`);
    }
    const commit = await response.json();
    if (
      requireSHA(commit.sha, "pull request merge ref commit SHA") !==
        mergeSHA ||
      !Array.isArray(commit.parents)
    ) {
      fail("pull request merge ref commit response is invalid");
    }
    return commit.parents.map((parent) =>
      requireSHA(parent?.sha, "pull request merge ref parent SHA"),
    );
  }

  return { currentPullRequest, mergeCommitParents, resolveRemoteRef };
}
