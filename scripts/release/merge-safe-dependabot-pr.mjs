#!/usr/bin/env node

import {
  apiVersion,
  fail,
  github,
  githubPages,
  policyAt,
  requireSHA,
} from "./dependabot-direct-merge-github.mjs";
import {
  ownedMarker,
  requiredPolicy,
  requireChecks,
} from "./dependabot-direct-merge-policy.mjs";

const repository = process.env.GITHUB_REPOSITORY ?? "";
const token = process.env.GH_TOKEN ?? "";
const runKind = process.env.RUN_KIND ?? "";
const runId = process.env.RUN_ID ?? "";
const runSHA = process.env.RUN_SHA ?? "";
const policyRef = process.env.AUTHENTICATED_POLICY_REF ?? "";
const expectedPolicySHA = process.env.EXPECTED_POLICY_SHA ?? "";
const defaultBranch = process.env.DEFAULT_BRANCH ?? "";

async function currentPolicy() {
  const branch = await github(`repos/${repository}/branches/${defaultBranch}`);
  const currentRef = requireSHA(
    branch?.commit?.sha,
    "current default branch SHA",
  );
  const current = await policyAt(currentRef);
  return { ...current, ref: currentRef };
}

async function comments(prNumber) {
  return githubPages(`repos/${repository}/issues/${prNumber}/comments`);
}

async function timeline(prNumber) {
  return githubPages(`repos/${repository}/issues/${prNumber}/timeline`);
}

async function disableAutoMerge(pr) {
  if (pr.auto_merge === null || pr.auto_merge === undefined) {
    return;
  }
  await github("graphql", {
    method: "POST",
    body: JSON.stringify({
      query:
        "mutation($id:ID!){disablePullRequestAutoMerge(input:{pullRequestId:$id}){pullRequest{number}}}",
      variables: { id: pr.node_id },
    }),
  });
}

async function removeLabel(prNumber, safeLabel, labels) {
  if (!labels.some((label) => label.name === safeLabel)) {
    return;
  }
  const response = await fetch(
    `https://api.github.com/repos/${repository}/issues/${prNumber}/labels/${encodeURIComponent(safeLabel)}`,
    {
      method: "DELETE",
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${token}`,
        "User-Agent": "ha-nova-dependabot-direct-merge",
        "X-GitHub-Api-Version": apiVersion,
      },
    },
  );
  if (!response.ok && response.status !== 404) {
    fail(`failed to remove safe label from PR #${prNumber}`);
  }
}

async function cleanupOwnedState(pr, comment, safeLabel) {
  await disableAutoMerge(pr);
  await removeLabel(pr.number, safeLabel, pr.labels ?? []);
  if (comment !== undefined) {
    await github(`repos/${repository}/issues/comments/${comment.id}`, {
      method: "DELETE",
    });
  }
}

async function triggerPRNumbers() {
  if (runKind === "workflow_run") {
    const run = await github(`repos/${repository}/actions/runs/${runId}`);
    return (run.pull_requests ?? []).map((pull) => pull.number);
  }
  if (runKind === "check_run") {
    const pulls = await github(
      `repos/${repository}/commits/${runSHA}/pulls?per_page=100`,
    );
    return pulls
      .filter(
        (pull) =>
          pull.state === "open" &&
          pull.base?.ref === defaultBranch &&
          pull.base?.repo?.full_name === repository &&
          pull.head?.sha === runSHA,
      )
      .map((pull) => pull.number);
  }
  fail("unrecognized trusted trigger kind");
}

async function pullRequest(prNumber) {
  return github(`repos/${repository}/pulls/${prNumber}`);
}

async function requireLiveProtection(policy) {
  const expected = requiredPolicy(policy, true);
  const live = await github(
    `repos/${repository}/branches/${defaultBranch}/protection`,
  );
  if (live.required_status_checks?.strict !== true) {
    fail("live required status checks are not strict");
  }
  const liveChecks = live.required_status_checks?.checks ?? [];
  const liveContexts = [
    ...new Set(liveChecks.map((check) => check.context)),
  ].sort();
  if (
    JSON.stringify(liveContexts) !==
    JSON.stringify([...expected.required].sort())
  ) {
    fail("live required status checks differ from authenticated policy");
  }
  for (const [name, appId] of Object.entries(expected.apps)) {
    if (
      !liveChecks.some(
        (check) => check.context === name && check.app_id === appId,
      )
    ) {
      fail(`live required check ${name} is not bound to its exact App`);
    }
  }
  return expected;
}

async function requireOwnedLabel(pr, safeLabel) {
  if (!(pr.labels ?? []).some((label) => label.name === safeLabel)) {
    fail("safe Dependabot label is absent");
  }
  const events = await timeline(pr.number);
  const lastEvent = events
    .filter(
      (event) =>
        ["labeled", "unlabeled"].includes(event.event) &&
        event.label?.name === safeLabel,
    )
    .at(-1);
  if (
    lastEvent?.event !== "labeled" ||
    lastEvent.actor?.login !== "github-actions[bot]"
  ) {
    fail("safe Dependabot label is not currently automation-owned");
  }
}

async function checkRuns(headSHA) {
  const response = await github(
    `repos/${repository}/commits/${headSHA}/check-runs?filter=all&per_page=100`,
  );
  const checks = response.check_runs ?? [];
  if (response.total_count > checks.length) {
    fail("more than 100 check runs exist for the candidate commit");
  }
  return checks;
}

async function requireNoActiveCI(headSHA) {
  const active = new Set([
    "pending",
    "queued",
    "requested",
    "in_progress",
    "waiting",
  ]);
  let seen = 0;
  for (let page = 1; page <= 10; page += 1) {
    const response = await github(
      `repos/${repository}/actions/workflows/ci.yml/runs?head_sha=${headSHA}&per_page=100&page=${page}`,
    );
    if (
      !Number.isSafeInteger(response.total_count) ||
      response.total_count < 0 ||
      !Array.isArray(response.workflow_runs)
    ) {
      fail("CI workflow-run response is invalid");
    }
    if (
      response.workflow_runs.some(
        (run) =>
          run.head_sha === headSHA &&
          run.head_repository?.full_name === repository &&
          active.has(run.status),
      )
    ) {
      fail("a CI run is queued or in progress for the candidate head");
    }
    seen += response.workflow_runs.length;
    if (seen >= response.total_count) {
      return;
    }
    if (response.workflow_runs.length !== 100) {
      fail("CI workflow-run pagination ended before total_count");
    }
  }
  fail("more than 1,000 CI runs exist for the candidate head");
}

async function validateCandidate(prNumber, expectedHead, policySHA) {
  const current = await currentPolicy();
  if (current.ref !== policyRef || current.sha256 !== policySHA) {
    fail("repository policy changed during direct-merge validation");
  }
  const policyContract = await requireLiveProtection(current.policy);
  const pr = await pullRequest(prNumber);
  if (
    pr.state !== "open" ||
    pr.draft === true ||
    pr.user?.login !== "dependabot[bot]" ||
    pr.head?.sha !== expectedHead ||
    pr.base?.ref !== defaultBranch ||
    pr.base?.repo?.full_name !== repository ||
    pr.base?.sha !== current.ref
  ) {
    fail("current pull request identity is not mergeable under strict policy");
  }
  const mergeSHA = requireSHA(
    pr.merge_commit_sha,
    "pull request merge commit SHA",
  );
  const mergeRef = await github(
    `repos/${repository}/git/ref/pull/${pr.number}/merge`,
  );
  if (
    requireSHA(mergeRef?.object?.sha, "pull request merge ref SHA") !== mergeSHA
  ) {
    fail("pull request API and merge ref identify different merge commits");
  }
  const markerComment = ownedMarker(
    await comments(pr.number),
    policyContract.safeLabel,
    policySHA,
  );
  if (markerComment === undefined) {
    fail("current automation-owned policy marker is absent");
  }
  await requireOwnedLabel(pr, policyContract.safeLabel);
  requireChecks(
    await checkRuns(expectedHead),
    current.policy,
    mergeSHA,
    pr.number,
  );
  await requireNoActiveCI(expectedHead);
  return { headSHA: expectedHead, mergeSHA };
}

async function main() {
  if (
    !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository) ||
    token.length < 20 ||
    defaultBranch !== "main" ||
    !/^[1-9][0-9]*$/.test(runKind === "workflow_run" ? runId : "1")
  ) {
    fail("trusted GitHub Actions context is incomplete");
  }
  requireSHA(runSHA, "trigger commit SHA");
  requireSHA(policyRef, "authenticated policy ref");
  if (!/^[0-9a-f]{64}$/.test(expectedPolicySHA)) {
    fail("authenticated policy fingerprint must be SHA-256");
  }

  const authenticated = await policyAt(policyRef);
  if (authenticated.sha256 !== expectedPolicySHA) {
    fail("authenticated repository policy fingerprint mismatch");
  }
  const oldContract = requiredPolicy(authenticated.policy);
  const initialCurrent = await currentPolicy();
  const policyDrift =
    initialCurrent.ref !== policyRef ||
    initialCurrent.sha256 !== expectedPolicySHA;

  for (const prNumber of await triggerPRNumbers()) {
    const pr = await pullRequest(prNumber);
    if (pr.head?.sha !== runSHA) {
      continue;
    }
    const markerComment = ownedMarker(
      await comments(pr.number),
      oldContract.safeLabel,
      expectedPolicySHA,
    );
    let owned =
      pr.user?.login === "dependabot[bot]" && markerComment !== undefined;
    if (
      !owned &&
      pr.user?.login === "dependabot[bot]" &&
      pr.auto_merge?.enabled_by?.login === "github-actions[bot]"
    ) {
      owned = (await timeline(pr.number)).some(
        (event) =>
          event.event === "labeled" &&
          event.label?.name === oldContract.safeLabel &&
          event.actor?.login === "github-actions[bot]",
      );
    }
    if (policyDrift) {
      if (owned) {
        await cleanupOwnedState(pr, markerComment, oldContract.safeLabel);
      }
      continue;
    }
    if (!owned || pr.draft === true) {
      continue;
    }
    await disableAutoMerge(pr);
    await validateCandidate(pr.number, runSHA, expectedPolicySHA);
    const final = await validateCandidate(pr.number, runSHA, expectedPolicySHA);
    const merged = await github(
      `repos/${repository}/pulls/${pr.number}/merge`,
      {
        method: "PUT",
        body: JSON.stringify({
          merge_method: "squash",
          sha: final.headSHA,
        }),
      },
    );
    if (merged?.merged !== true) {
      fail(`GitHub refused the exact-head squash merge for PR #${pr.number}`);
    }
  }
}

main().catch((error) => {
  const message =
    error instanceof Error ? error.message : "unexpected direct-merge failure";
  console.error(`[merge-safe-dependabot-pr] ERROR: ${message}`);
  process.exit(1);
});
