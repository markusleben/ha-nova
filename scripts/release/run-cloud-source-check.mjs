#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";

import { createCloudSourceCheckReporter } from "./cloud-source-check-reporter.mjs";

const workspace = process.env.GITHUB_WORKSPACE ?? "";
const repository = process.env.GITHUB_REPOSITORY ?? "";
const eventPath = process.env.GITHUB_EVENT_PATH ?? "";
const runId = process.env.GITHUB_RUN_ID ?? "";
const githubToken = process.env.GH_TOKEN ?? "";
const checkToken = process.env.HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN ?? "";
const checkAppId = Number(process.env.HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID ?? "");
const evidence = process.env.HA_NOVA_CLOUD_GATE_EVIDENCE_JSON ?? "";
const apiVersion = "2026-03-10";

function fail(message) {
  throw new Error(message);
}

function requireSHA(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  return value;
}

async function github(endpoint, token, init = {}) {
  const response = await fetch(`https://api.github.com/${endpoint}`, {
    ...init,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      "User-Agent": "ha-nova-cloud-source-gate",
      "X-GitHub-Api-Version": apiVersion,
      ...(init.headers ?? {}),
    },
  });
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
  });
  if (result.error !== undefined || result.status !== 0) {
    fail(`${command} ${args[0]} failed`);
  }
}

function resolveRemoteRef(sourceRef) {
  const result = spawnSync(
    "git",
    ["ls-remote", "--refs", "origin", sourceRef],
    {
      cwd: workspace,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "inherit"],
    },
  );
  if (result.error !== undefined || result.status !== 0) {
    fail(`cannot resolve remote source ref ${sourceRef}`);
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
    fail("workflow run must identify exactly one current pull request");
  }
  return matches[0];
}

function readEvent() {
  let event;
  try {
    event = JSON.parse(readFileSync(eventPath, "utf8"));
  } catch {
    fail("GITHUB_EVENT_PATH must contain valid workflow_run JSON");
  }
  if (
    !["completed", "in_progress", "requested"].includes(event?.action) ||
    event.workflow_run?.name !== "CI" ||
    !["completed", "in_progress", "queued", "requested"].includes(
      event.workflow_run?.status,
    )
  ) {
    fail("only a CI workflow lifecycle event may request this check");
  }
  return { action: event.action, workflowRun: event.workflow_run };
}

async function requireTrustedCI(workflowRun) {
  if (!Number.isSafeInteger(workflowRun.workflow_id)) {
    fail("workflow run must identify its source workflow");
  }
  const trusted = await github(
    `repos/${repository}/actions/workflows/ci.yml`,
    githubToken,
  );
  if (
    !Number.isSafeInteger(trusted.id) ||
    trusted.id !== workflowRun.workflow_id ||
    trusted.path !== ".github/workflows/ci.yml"
  ) {
    fail("workflow run did not originate from the trusted CI workflow");
  }
  const current = await github(
    `repos/${repository}/actions/runs/${workflowRun.id}`,
    githubToken,
  );
  if (
    current.id !== workflowRun.id ||
    current.run_attempt !== workflowRun.run_attempt ||
    current.workflow_id !== workflowRun.workflow_id ||
    current.event !== workflowRun.event ||
    current.head_sha !== workflowRun.head_sha ||
    current.head_branch !== workflowRun.head_branch
  ) {
    fail("workflow run lifecycle identity changed");
  }
  return current;
}

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

const { completeCheck, ensurePendingCheck } = createCloudSourceCheckReporter({
  appId: checkAppId,
  repository,
  runId,
  token: checkToken,
});

let checkId;
try {
  const { action, workflowRun } = readEvent();
  const event = workflowRun.event;
  const headSHA = requireSHA(workflowRun.head_sha, "workflow run head_sha");
  if (event !== "pull_request" && event !== "merge_group") {
    fail(`unsupported triggering event ${String(event)}`);
  }
  const currentWorkflowRun = await requireTrustedCI(workflowRun);

  let sourceEnvironment;
  let sourceRef;
  let verifiedTargetSHA;
  let currentPR;

  if (event === "pull_request") {
    currentPR = await currentPullRequest(headSHA);
    sourceRef = `refs/pull/${currentPR.number}/merge`;
    verifiedTargetSHA = resolveRemoteRef(sourceRef);
    if (
      requireSHA(
        currentPR.merge_commit_sha,
        "pull request merge commit SHA",
      ) !== verifiedTargetSHA
    ) {
      fail("pull request API and merge ref identify different merge commits");
    }
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
    verifiedTargetSHA = headSHA;
    sourceEnvironment = {
      HA_NOVA_CLOUD_GATE_SOURCE_REF: sourceRef,
      HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT: headSHA,
    };
  }

  const { check, terminalSuccess } = await ensurePendingCheck(
    currentWorkflowRun,
    verifiedTargetSHA,
  );
  checkId = check.id;
  if (action !== "completed" || terminalSuccess) {
    process.exit(0);
  }
  if (currentWorkflowRun.status !== "completed") {
    fail("completed workflow event does not identify a completed run");
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
    if (
      latest.state !== "open" ||
      latest.base?.sha !== currentPR.base.sha ||
      latest.head?.sha !== headSHA
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
  if (resolveRemoteRef(sourceRef) !== verifiedTargetSHA) {
    fail("source ref changed while the trusted source gate was running");
  }
  if (event === "pull_request") {
    const finalPR = await currentPullRequest(headSHA);
    if (
      finalPR.number !== currentPR.number ||
      finalPR.base?.sha !== currentPR.base.sha ||
      finalPR.head?.sha !== headSHA ||
      requireSHA(
        finalPR.merge_commit_sha,
        "final pull request merge commit SHA",
      ) !== verifiedTargetSHA
    ) {
      fail("pull request identity changed after final source verification");
    }
  }
  await completeCheck(
    checkId,
    "success",
    "Trusted default-branch code verified the current source state.",
  );
} catch (error) {
  const message =
    error instanceof Error ? error.message : "unexpected source-gate failure";
  console.error(`[run-cloud-source-check] ERROR: ${message}`);
  if (checkId !== undefined) {
    await completeCheck(
      checkId,
      "failure",
      "Trusted source verification failed. Inspect the linked workflow run.",
    );
  }
  process.exit(1);
}
