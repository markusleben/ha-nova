#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";

import {
  AmbiguousSourceCheckMutationError,
  createCloudSourceCheckReporter,
  ReportedSourceCheckError,
} from "./cloud-source-check-reporter.mjs";
import { handleCloudSourceCheckFailure } from "./cloud-source-check-failure.mjs";
import { createCloudSourceConsistencyResolver } from "./cloud-source-consistency.mjs";
import { createCloudSourcePullRequestReader } from "./cloud-source-pull-request.mjs";
import { createTrustedCIResolver } from "./cloud-source-workflow-run.mjs";

const workspace = process.env.GITHUB_WORKSPACE ?? "";
const repository = process.env.GITHUB_REPOSITORY ?? "";
const eventPath = process.env.GITHUB_EVENT_PATH ?? "";
const runId = process.env.GITHUB_RUN_ID ?? "";
const githubToken = process.env.GH_TOKEN ?? "";
const checkToken = process.env.HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN ?? "";
const checkAppId = Number(process.env.HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID ?? "");
const evidence = process.env.HA_NOVA_CLOUD_GATE_EVIDENCE_JSON ?? "";
const apiVersion = "2026-03-10";
const apiTimeoutMs = 10_000;
const commandTimeoutMs = 30_000;

function fail(message) {
  throw new Error(message);
}

function noCheck(message) {
  console.log(`[run-cloud-source-check] NOTICE: ${message}`);
  process.exit(0);
}

function requireSHA(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  return value;
}

async function githubResponse(endpoint, token, init = {}) {
  return fetch(`https://api.github.com/${endpoint}`, {
    ...init,
    signal: init.signal ?? AbortSignal.timeout(apiTimeoutMs),
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      "User-Agent": "ha-nova-cloud-source-gate",
      "X-GitHub-Api-Version": apiVersion,
      ...(init.headers ?? {}),
    },
  });
}

async function github(endpoint, token, init = {}) {
  const response = await githubResponse(endpoint, token, init);
  if (!response.ok) {
    fail(`GitHub API ${endpoint} returned HTTP ${response.status}`);
  }
  return response.json();
}

function run(command, args, env = {}) {
  const result = spawnSync(command, args, {
    cwd: workspace,
    env: { ...process.env, ...env },
    stdio: "inherit",
    timeout: commandTimeoutMs,
    killSignal: "SIGKILL",
  });
  if (result.error !== undefined || result.status !== 0) {
    fail(`${command} ${args[0]} failed`);
  }
}

const { currentPullRequest, mergeCommitParents, resolveRemoteRef } =
  createCloudSourcePullRequestReader({
    apiTimeoutMs,
    fail,
    github,
    githubResponse,
    githubToken,
    repository,
    requireSHA,
    workspace,
  });

function readEvent() {
  let event;
  try {
    event = JSON.parse(readFileSync(eventPath, "utf8"));
  } catch {
    fail("GITHUB_EVENT_PATH must contain valid workflow_run JSON");
  }
  if (
    !["completed", "in_progress"].includes(event?.action) ||
    event.workflow_run?.name !== "CI" ||
    event.workflow_run?.status !== event.action
  ) {
    fail("only an in-progress or completed CI event may request this check");
  }
  return { action: event.action, workflowRun: event.workflow_run };
}

const {
  matchesPullRequestSource,
  resolvePullRequestSource,
  resolveQueueSource,
} = createCloudSourceConsistencyResolver({
  currentPullRequest,
  mergeCommitParents,
  requireSHA,
  resolveRemoteRef,
});
const requireTrustedCI = createTrustedCIResolver({
  fail,
  github: (endpoint) => github(endpoint, githubToken),
  repository,
});

if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) {
  fail("GITHUB_REPOSITORY must identify one repository");
}
if (workspace.length === 0 || eventPath.length === 0 || runId.length === 0) {
  fail("GitHub Actions workflow context is incomplete");
}
if (githubToken.length < 20 || checkToken.length < 20) {
  fail("trusted GitHub and dedicated check tokens are required");
}
if (!Number.isSafeInteger(checkAppId) || checkAppId <= 0) {
  fail("dedicated check App id must be a positive integer");
}

const {
  completeCheck,
  deletePendingCheck,
  deletePendingAttemptChecks,
  deletePendingAttemptChecksEventually,
  deletePendingTargetChecks,
  ensurePendingCheck,
  hasTerminalAttemptResult,
  rejectTargetCheck,
} = createCloudSourceCheckReporter({
  appId: checkAppId,
  repository,
  runId,
  token: checkToken,
});

let checkId;
let activeWorkflowRun;
let staleAttemptObserved = false;
let terminalCheckReported = false;

async function discardStaleAttempt(workflowRun) {
  staleAttemptObserved = true;
  await deletePendingAttemptChecks(workflowRun);
  noCheck("workflow lifecycle delivery belongs to an older CI attempt");
}

try {
  const { action, workflowRun } = readEvent();
  const event = workflowRun.event;
  const headSHA = requireSHA(workflowRun.head_sha, "workflow run head_sha");
  if (event !== "pull_request" && event !== "merge_group") {
    fail(`unsupported triggering event ${String(event)}`);
  }
  activeWorkflowRun = workflowRun;
  const trustedCI = await requireTrustedCI(workflowRun);
  if (trustedCI.staleAttempt) {
    await discardStaleAttempt(workflowRun);
  }
  const currentWorkflowRun = trustedCI.current;
  activeWorkflowRun = currentWorkflowRun;
  async function revalidateCurrentAttempt() {
    const mutationCI = await requireTrustedCI(workflowRun);
    if (mutationCI.staleAttempt) {
      await discardStaleAttempt(currentWorkflowRun);
    }
  }
  if (action === "in_progress") {
    if (currentWorkflowRun.status === "completed") {
      noCheck("upstream CI already completed; provisional check not emitted");
    }
    if (
      await hasTerminalAttemptResult(
        currentWorkflowRun,
        revalidateCurrentAttempt,
      )
    ) {
      await deletePendingTargetChecks(currentWorkflowRun, headSHA);
      noCheck("source check attempt already has a terminal result");
    }
    const { check, terminalResult } = await ensurePendingCheck(
      currentWorkflowRun,
      headSHA,
      revalidateCurrentAttempt,
    );
    if (terminalResult) {
      process.exit(0);
    }
    const refreshedCI = await requireTrustedCI(workflowRun);
    if (refreshedCI.staleAttempt) {
      await discardStaleAttempt(workflowRun);
    }
    const refreshedWorkflowRun = refreshedCI.current;
    if (refreshedWorkflowRun.status === "completed") {
      if (event === "pull_request") {
        await deletePendingTargetChecks(refreshedWorkflowRun, headSHA);
      } else if (
        await hasTerminalAttemptResult(
          refreshedWorkflowRun,
          revalidateCurrentAttempt,
        )
      ) {
        await deletePendingAttemptChecks(refreshedWorkflowRun);
      }
    }
    noCheck("provisional source check recorded for active CI");
  }
  if (
    currentWorkflowRun.status !== "completed" ||
    currentWorkflowRun.conclusion !== "success"
  ) {
    await deletePendingAttemptChecks(currentWorkflowRun);
    noCheck(
      "upstream CI did not complete successfully; no source check emitted",
    );
  }
  if (
    await hasTerminalAttemptResult(currentWorkflowRun, revalidateCurrentAttempt)
  ) {
    await deletePendingAttemptChecks(currentWorkflowRun);
    noCheck("source check attempt already has a terminal result");
  }

  let sourceEnvironment;
  let sourceRef;
  let verifiedTargetSHA;
  let currentPR;

  if (event === "pull_request") {
    const resolved = await resolvePullRequestSource(headSHA);
    const refreshedSourceCI = await requireTrustedCI(workflowRun);
    if (refreshedSourceCI.staleAttempt) {
      await discardStaleAttempt(workflowRun);
    }
    if ("reason" in resolved) {
      if (resolved.kind === "stale") {
        await deletePendingAttemptChecks(currentWorkflowRun);
      } else {
        await rejectTargetCheck(
          currentWorkflowRun,
          headSHA,
          "GitHub did not materialize the current pull-request source before the bounded deadline. Re-run CI once; the Cloud Source Gate will follow automatically.",
          revalidateCurrentAttempt,
        );
      }
      noCheck(resolved.reason);
    }
    currentPR = resolved.pull;
    sourceRef = resolved.sourceRef;
    verifiedTargetSHA = resolved.targetSHA;
    sourceEnvironment = {
      HA_NOVA_CLOUD_GATE_PR_NUMBER: String(currentPR.number),
      HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT: verifiedTargetSHA,
      HA_NOVA_CLOUD_GATE_EXPECTED_HEAD_COMMIT: headSHA,
      HA_NOVA_CLOUD_GATE_EXPECTED_BASE_COMMIT: requireSHA(
        currentPR.base.sha,
        "pull request base SHA",
      ),
    };
  } else if (event === "merge_group") {
    if (
      typeof workflowRun.head_branch !== "string" ||
      !/^gh-readonly-queue\/main\/[A-Za-z0-9._/-]+$/.test(
        workflowRun.head_branch,
      )
    ) {
      fail("merge_group workflow run has an invalid queue branch");
    }
    sourceRef = `refs/heads/${workflowRun.head_branch}`;
    const resolved = await resolveQueueSource(sourceRef, headSHA);
    if ("reason" in resolved) {
      await deletePendingAttemptChecks(currentWorkflowRun);
      noCheck(resolved.reason);
    }
    verifiedTargetSHA = resolved.targetSHA;
    sourceEnvironment = {
      HA_NOVA_CLOUD_GATE_SOURCE_REF: sourceRef,
      HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT: headSHA,
    };
  }

  const { check, terminalResult } = await ensurePendingCheck(
    currentWorkflowRun,
    verifiedTargetSHA,
    revalidateCurrentAttempt,
  );
  checkId = check.id;
  await deletePendingAttemptChecks(currentWorkflowRun, checkId);
  if (terminalResult) {
    process.exit(0);
  }

  run("bash", ["scripts/release/verify-github-production-environment.sh"]);
  if (event === "pull_request") {
    run("bash", ["scripts/release/verify-cloud-pr-source-gate.sh"], {
      ...sourceEnvironment,
      HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: evidence,
    });
    const latest = await github(
      `repos/${repository}/pulls/${currentPR.number}`,
      githubToken,
    );
    const latestSourceMatches =
      latest.merge_commit_sha == null ||
      (await matchesPullRequestSource(latest, headSHA, verifiedTargetSHA)) ===
        true;
    if (
      latest.state !== "open" ||
      latest.base?.sha !== currentPR.base.sha ||
      latest.head?.sha !== headSHA ||
      !latestSourceMatches
    ) {
      fail("pull request changed while the trusted source gate was running");
    }
  } else {
    run("bash", ["scripts/release/verify-cloud-target-source-gate.sh"], {
      ...sourceEnvironment,
      HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: evidence,
    });
  }
  run(
    "bash",
    ["scripts/release/verify-github-main-protection.sh", repository, "main"],
    { GH_TOKEN: checkToken },
  );
  // Disabled workflows deliver no events, so their required checks silently
  // never appear and PRs hang BLOCKED with no red anywhere. Executed here from
  // the trusted default branch because enabled Cloud freezes workflow files
  // to uses:-only PR deltas, so the guard cannot live as a workflow step.
  run(
    "bash",
    ["scripts/release/verify-required-workflows-active.sh", repository],
    { GH_TOKEN: githubToken },
  );
  if (resolveRemoteRef(sourceRef) !== verifiedTargetSHA) {
    fail("source ref changed while the trusted source gate was running");
  }
  if (event === "pull_request") {
    const finalPR = await currentPullRequest(headSHA);
    if (
      finalPR === undefined ||
      finalPR.number !== currentPR.number ||
      finalPR.base?.sha !== currentPR.base.sha ||
      finalPR.head?.sha !== headSHA ||
      (await matchesPullRequestSource(finalPR, headSHA, verifiedTargetSHA)) !==
        true
    ) {
      fail("pull request identity changed after final source verification");
    }
  }
  const finalTrustedCI = await requireTrustedCI(workflowRun);
  if (finalTrustedCI.staleAttempt) {
    await discardStaleAttempt(currentWorkflowRun);
  }
  if (resolveRemoteRef(sourceRef) !== verifiedTargetSHA) {
    fail("source ref changed immediately before terminal reporting");
  }
  const terminalTrustedCI = await requireTrustedCI(workflowRun);
  if (terminalTrustedCI.staleAttempt) {
    await discardStaleAttempt(currentWorkflowRun);
  }
  await completeCheck(
    checkId,
    "success",
    "Trusted default-branch code verified the current source state.",
  );
  terminalCheckReported = true;
  await deletePendingAttemptChecks(currentWorkflowRun, checkId);
} catch (error) {
  const message =
    error instanceof Error ? error.message : "unexpected source-gate failure";
  console.error(`[run-cloud-source-check] ERROR: ${message}`);
  const exitCode = await handleCloudSourceCheckFailure({
    activeWorkflowRun,
    ambiguousMutation: error instanceof AmbiguousSourceCheckMutationError,
    checkId,
    completeCheck,
    deletePendingAttemptChecks,
    deletePendingAttemptChecksEventually,
    deletePendingCheck,
    reportedFailure: error instanceof ReportedSourceCheckError,
    requireTrustedCI,
    staleAttemptObserved,
    terminalCheckReported,
  });
  process.exit(exitCode);
}
