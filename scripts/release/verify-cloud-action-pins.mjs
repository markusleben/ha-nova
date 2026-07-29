#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { basename } from "node:path";

const workflowPaths = process.argv.slice(2);
const exactAction =
  /^\s+(?:-\s+)?uses:\s+[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_.-]+)*@[0-9a-f]{40}\s+#\s+v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\s*$/;

function fail(workflowPath, message) {
  console.error(
    `[verify-cloud-action-pins] ERROR: ${basename(workflowPath)} ${message}`,
  );
  process.exit(1);
}

function parseJobs(workflowPath, lines) {
  const jobsLine = lines.findIndex((line) => line === "jobs:");
  if (jobsLine < 0) {
    fail(workflowPath, "must define one canonical jobs key");
  }
  const starts = [];
  for (let index = jobsLine + 1; index < lines.length; index += 1) {
    const match = /^  ([A-Za-z0-9_-]+):\s*$/.exec(lines[index]);
    if (match !== null) {
      starts.push({ id: match[1], start: index });
    }
  }
  if (starts.length === 0) {
    fail(workflowPath, "must define at least one canonical job");
  }
  return starts.map((job, index) => ({
    ...job,
    end: starts[index + 1]?.start ?? lines.length,
  }));
}

function sensitiveJobIDs(workflowPath, jobs) {
  switch (basename(workflowPath)) {
    case "ci.yml":
      return new Set(["ci-gate"]);
    case "cloud-source-gate.yml":
      return new Set(["cloud-source-mode", "cloud-source-gate"]);
    case "e2e-disposable-ha.yml":
      return new Set(["disposable-ha"]);
    case "release.yml":
    case "release-candidate.yml":
    case "cloud-candidate-bundle.yml":
      return new Set(jobs.map((job) => job.id));
    default:
      fail(workflowPath, "is not a recognized Cloud-sensitive workflow");
  }
}

function hasUsesMapping(line) {
  if (line.trimStart().startsWith("#")) {
    return false;
  }
  return /(?:^|[{,\s])["']?uses["']?\s*:/.test(line);
}

function verifyWorkflow(workflowPath) {
  let lines;
  try {
    lines = readFileSync(workflowPath, "utf8").split(/\r?\n/);
  } catch {
    fail(workflowPath, "must exist and be readable");
  }
  const jobs = parseJobs(workflowPath, lines);
  const sensitive = sensitiveJobIDs(workflowPath, jobs);
  for (const jobID of sensitive) {
    if (!jobs.some((job) => job.id === jobID)) {
      fail(workflowPath, `must define Cloud-sensitive job '${jobID}'`);
    }
  }

  let actionCount = 0;
  for (const job of jobs.filter((candidate) => sensitive.has(candidate.id))) {
    for (let index = job.start + 1; index < job.end; index += 1) {
      const line = lines[index];
      if (
        /^\s+-\s*(?:[&*{]|\?)/.test(line) ||
        /^\s+<<\s*:/.test(line)
      ) {
        fail(
          workflowPath,
          `Cloud-sensitive job '${job.id}' uses non-canonical step syntax at line ${index + 1}`,
        );
      }
      if (!hasUsesMapping(line)) {
        continue;
      }
      actionCount += 1;
      if (!exactAction.test(line)) {
        fail(
          workflowPath,
          `Cloud-sensitive job '${job.id}' action at line ${index + 1} must use an immutable full commit SHA with an exact vX.Y.Z comment`,
        );
      }
    }
  }
  if (actionCount === 0) {
    fail(workflowPath, "must contain a pinned Cloud-sensitive action");
  }
  console.log(
    `[verify-cloud-action-pins] OK: ${basename(workflowPath)} pins ${actionCount} Cloud-sensitive action(s)`,
  );
}

if (workflowPaths.length === 0) {
  console.error(
    "[verify-cloud-action-pins] ERROR: at least one workflow path is required",
  );
  process.exit(1);
}
for (const workflowPath of workflowPaths) {
  verifyWorkflow(workflowPath);
}
