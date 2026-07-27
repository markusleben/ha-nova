import fs from "node:fs";
import path from "node:path";
import { workflowSyntaxProblem } from "./verify-cloud-workflow-syntax.mjs";
const workflowPaths = process.argv.slice(2);
const metadataStepName = "Verify release metadata";
const gateStepName = "Verify Home Assistant Cloud release gate";
const environmentStepName = "Verify production environment policy";
const mainProtectionStepName = "Verify live main protection";
const metadataCommand =
  'run: bash scripts/release/verify-release-metadata.sh "${VERSION_TAG}"';
const gateCommand = "run: bash scripts/release/verify-cloud-release-gate.sh";
const environmentCommand =
  "run: bash scripts/release/verify-github-production-environment.sh";
const mainProtectionCommand =
  "run: bash scripts/release/verify-cloud-publication-main-protection.sh";
const githubTokenBinding = "GH_TOKEN: ${{ github.token }}";
const sourceAppIdBinding =
  "HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID: ${{ secrets.HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID }}";
const sourceAppKeyBinding =
  "HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY: ${{ secrets.HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY }}";
const evidenceBinding =
  "HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: ${{ secrets.HA_NOVA_CLOUD_GATE_EVIDENCE_JSON }}";
function fail(workflowPath, message) {
  console.error(
    `[verify-cloud-workflow-gate] ERROR: ${path.basename(workflowPath)} ${message}`,
  );
  process.exit(1);
}
function stepEnd(lines, start) {
  const indent = lines[start].match(/^\s*/)?.[0] ?? "";
  for (let index = start + 1; index < lines.length; index += 1) {
    if (lines[index].startsWith(`${indent}- `)) {
      return index;
    }
  }
  return lines.length;
}
function assertRequiredStep(workflowPath, lines, name) {
  const marker = `- name: ${name}`;
  const matches = lines.flatMap((line, index) =>
    line.trim() === marker ? [index] : [],
  );
  if (matches.length !== 1) {
    fail(workflowPath, `must contain exactly one '${name}' step`);
  }
  const start = matches[0];
  const end = stepEnd(lines, start);
  return { start, end, block: lines.slice(start, end) };
}
function namedStepsForJob(lines, job) {
  const steps = [];
  for (let index = job.start + 1; index < job.end; index += 1) {
    const match = lines[index].match(/^      - name:\s+(.+?)\s*$/);
    if (match) {
      steps.push({
        name: match[1],
        start: index,
        end: Math.min(stepEnd(lines, index), job.end),
      });
    }
  }
  return steps;
}
function meaningfulLines(block) {
  return block.map((line) => line.trim()).filter(Boolean);
}
function assertPureGateJob(
  workflowPath,
  lines,
  gateJob,
  mainProtection,
  metadata,
  gate,
) {
  const steps = namedStepsForJob(lines, gateJob);
  const allStepStarts = lines
    .slice(gateJob.start + 1, gateJob.end)
    .filter((line) => /^      -\s+(?:name|uses|run):/.test(line));
  const expectedSteps = [
    "Checkout",
    "Setup Node",
    environmentStepName,
    mainProtectionStepName,
    metadataStepName,
    gateStepName,
  ];
  if (
    steps.length !== expectedSteps.length ||
    allStepStarts.length !== expectedSteps.length ||
    steps.some((step, index) => step.name !== expectedSteps[index])
  ) {
    fail(
      workflowPath,
      `Cloud gate job '${gateJob.id}' may contain only Checkout, Setup Node, production environment policy, live main protection, release metadata, and the Cloud gate`,
    );
  }
  const isFinalRelease = path.basename(workflowPath) === "release.yml";
  const expectedJobName = isFinalRelease
    ? "name: Verify release publication gates"
    : "name: Verify RC publication gates";
  const expectedVersionBinding = isFinalRelease
    ? "VERSION_TAG: ${{ github.ref_name }}"
    : "VERSION_TAG: ${{ inputs.version_tag }}";
  const relativeStepsStart = lines
    .slice(gateJob.start + 1, gateJob.end)
    .findIndex((line) => line === "    steps:");
  if (relativeStepsStart < 0) {
    fail(workflowPath, `Cloud gate job '${gateJob.id}' must define steps`);
  }
  const jobHeader = meaningfulLines(
    lines.slice(gateJob.start + 1, gateJob.start + relativeStepsStart + 2),
  );
  const expectedJobHeader = [
    expectedJobName,
    "runs-on: ubuntu-latest",
    "permissions:",
    "contents: read",
    "deployments: read",
    "environment:",
    "name: production",
    "steps:",
  ];
  if (
    jobHeader.length !== expectedJobHeader.length ||
    jobHeader.some((line, index) => line !== expectedJobHeader[index])
  ) {
    fail(
      workflowPath,
      `Cloud gate job '${gateJob.id}' must use the exact read-only isolated job contract`,
    );
  }
  const checkout = meaningfulLines(lines.slice(steps[0].start, steps[0].end));
  if (
    checkout.length !== 4 ||
    checkout[0] !== "- name: Checkout" ||
    !/^uses: actions\/checkout@/.test(checkout[1]) ||
    checkout[2] !== "with:" ||
    checkout[3] !== "fetch-depth: 0"
  ) {
    fail(
      workflowPath,
      "Cloud gate checkout step must use only actions/checkout with full history",
    );
  }
  const setup = meaningfulLines(lines.slice(steps[1].start, steps[1].end));
  if (
    setup.length !== 4 ||
    setup[0] !== "- name: Setup Node" ||
    !/^uses: actions\/setup-node@/.test(setup[1]) ||
    setup[2] !== "with:" ||
    setup[3] !== 'node-version: "20"'
  ) {
    fail(
      workflowPath,
      "Cloud gate setup step must use only actions/setup-node with Node 20",
    );
  }
  const environment = steps[2];
  const environmentLines = meaningfulLines(
    lines.slice(environment.start, environment.end),
  );
  if (
    environmentLines.length !== 4 ||
    environmentLines[0] !== `- name: ${environmentStepName}` ||
    environmentLines[1] !== "env:" ||
    environmentLines[2] !== githubTokenBinding ||
    environmentLines[3] !== environmentCommand
  ) {
    fail(
      workflowPath,
      "production environment step must run only the exact live policy verifier with github.token",
    );
  }
  const mainProtectionLines = meaningfulLines(mainProtection.block);
  if (
    mainProtectionLines.length !== 5 ||
    mainProtectionLines[0] !== `- name: ${mainProtectionStepName}` ||
    mainProtectionLines[1] !== "env:" ||
    mainProtectionLines[2] !== sourceAppIdBinding ||
    mainProtectionLines[3] !== sourceAppKeyBinding ||
    mainProtectionLines[4] !== mainProtectionCommand
  ) {
    fail(
      workflowPath,
      "main protection step must mint and use only the exact Cloud source App administration-read token",
    );
  }
  const metadataLines = meaningfulLines(metadata.block);
  if (
    metadataLines.length !== 4 ||
    metadataLines[0] !== `- name: ${metadataStepName}` ||
    metadataLines[1] !== "env:" ||
    metadataLines[2] !== expectedVersionBinding ||
    metadataLines[3] !== metadataCommand
  ) {
    fail(
      workflowPath,
      "release metadata step must run only the exact metadata verifier with one VERSION_TAG binding",
    );
  }
  const gateLines = meaningfulLines(gate.block);
  if (
    gateLines.length !== 5 ||
    gateLines[0] !== `- name: ${gateStepName}` ||
    gateLines[1] !== "env:" ||
    gateLines[2] !== githubTokenBinding ||
    gateLines[3] !== evidenceBinding ||
    gateLines[4] !== gateCommand
  ) {
    fail(
      workflowPath,
      "Cloud gate step must run only the exact fail-closed gate command",
    );
  }
}
function artifactProducerLines(lines) {
  const patterns = [
    /^\s*-\s+name:\s+.*\b(?:build|upload|publish)\b/i,
    /^\s*uses:\s*(?:goreleaser\/goreleaser-action|actions\/upload-artifact)@/i,
    /\bgh\s+release\s+(?:create|edit|upload)\b/i,
    /\bscripts\/release\/build-install-bundle\.sh\b/,
    /\bgo\s+build\b/,
    /\bgoreleaser\s+(?:build|release)\b/i,
  ];
  return lines.flatMap((line, index) => {
    if (line.trimStart().startsWith("#")) {
      return [];
    }
    return patterns.some((pattern) => pattern.test(line)) ? [index] : [];
  });
}
function parseJobs(workflowPath, lines) {
  const jobsLine = lines.findIndex((line) => line.trim() === "jobs:");
  if (jobsLine < 0) {
    fail(workflowPath, "must define jobs");
  }
  const starts = [];
  for (let index = jobsLine + 1; index < lines.length; index += 1) {
    const match = lines[index].match(/^  ([A-Za-z0-9_-]+):\s*$/);
    if (match) {
      starts.push({ id: match[1], start: index });
    } else if (
      /^  \S/.test(lines[index]) &&
      !lines[index].trimStart().startsWith("#")
    ) {
      fail(
        workflowPath,
        `contains unsupported jobs syntax at line ${index + 1}`,
      );
    }
  }
  if (starts.length === 0) {
    fail(workflowPath, "must define at least one job");
  }
  return starts.map((job, index) => ({
    ...job,
    end: starts[index + 1]?.start ?? lines.length,
  }));
}
function parseJobNeeds(lines, job) {
  for (let index = job.start + 1; index < job.end; index += 1) {
    const match = lines[index].match(/^    needs:\s*(.*?)\s*$/);
    if (!match) {
      continue;
    }
    const value = match[1].trim();
    if (value.startsWith("[") && value.endsWith("]")) {
      return value
        .slice(1, -1)
        .split(",")
        .map((need) => need.trim().replace(/^["']|["']$/g, ""))
        .filter(Boolean);
    }
    if (value !== "") {
      return [value.replace(/^["']|["']$/g, "")];
    }
    const needs = [];
    for (let nested = index + 1; nested < job.end; nested += 1) {
      const item = lines[nested].match(/^      -\s+([A-Za-z0-9_-]+)\s*$/);
      if (item) {
        needs.push(item[1]);
      } else if (/^    \S/.test(lines[nested])) {
        break;
      }
    }
    return needs;
  }
  return [];
}
function jobForLine(jobs, line) {
  return jobs.find((job) => line > job.start && line < job.end);
}
function assertNoStatusBypass(workflowPath, lines, job) {
  const unsafe = lines
    .slice(job.start + 1, job.end)
    .find((line) => /^    (?:if|continue-on-error):/.test(line));
  if (unsafe !== undefined) {
    fail(
      workflowPath,
      `job '${job.id}' must not bypass dependency failure status`,
    );
  }
}
function assertArtifactStepStatus(workflowPath, lines, job, artifactLine) {
  let stepStart = -1;
  for (let index = artifactLine; index > job.start; index -= 1) {
    if (/^      -\s+(?:name|uses):/.test(lines[index])) {
      stepStart = index;
      break;
    }
  }
  if (stepStart < 0) {
    fail(
      workflowPath,
      `artifact producer at line ${artifactLine + 1} is outside a workflow step`,
    );
  }
  const block = lines.slice(
    stepStart,
    Math.min(stepEnd(lines, stepStart), job.end),
  );
  if (block.some((line) => /^\s*continue-on-error:/.test(line))) {
    fail(
      workflowPath,
      `artifact producer step at line ${stepStart + 1} must not use continue-on-error`,
    );
  }
  if (
    block.some(
      (line) =>
        /^\s*if:/.test(line) &&
        /\b(?:always|failure|cancelled)\s*\(/.test(line),
    )
  ) {
    fail(
      workflowPath,
      `artifact producer step at line ${stepStart + 1} must not bypass a failed Cloud gate`,
    );
  }
}
function verifyWorkflow(workflowPath) {
  let lines;
  try {
    lines = fs.readFileSync(workflowPath, "utf8").split(/\r?\n/);
  } catch {
    fail(workflowPath, "must exist and be readable");
  }
  const syntaxProblem = workflowSyntaxProblem(lines);
  if (syntaxProblem !== null) {
    fail(workflowPath, syntaxProblem);
  }
  const metadata = assertRequiredStep(workflowPath, lines, metadataStepName);
  const mainProtection = assertRequiredStep(
    workflowPath,
    lines,
    mainProtectionStepName,
  );
  const gate = assertRequiredStep(workflowPath, lines, gateStepName);
  const jobs = parseJobs(workflowPath, lines);
  const metadataJob = jobForLine(jobs, metadata.start);
  const mainProtectionJob = jobForLine(jobs, mainProtection.start);
  const gateJob = jobForLine(jobs, gate.start);
  if (
    !metadataJob ||
    !mainProtectionJob ||
    !gateJob ||
    metadataJob.id !== gateJob.id ||
    mainProtectionJob.id !== gateJob.id
  ) {
    fail(
      workflowPath,
      "must run live main protection, release metadata, and the Cloud gate sequentially in one job",
    );
  }
  metadata.end = Math.min(metadata.end, metadataJob.end);
  metadata.block = lines.slice(metadata.start, metadata.end);
  mainProtection.end = Math.min(mainProtection.end, metadataJob.end);
  mainProtection.block = lines.slice(
    mainProtection.start,
    mainProtection.end,
  );
  gate.end = Math.min(gate.end, gateJob.end);
  gate.block = lines.slice(gate.start, gate.end);
  if (
    mainProtection.end !== metadata.start ||
    metadata.end !== gate.start
  ) {
    fail(
      workflowPath,
      "must run live main protection, release metadata, and the Cloud gate consecutively",
    );
  }
  assertPureGateJob(
    workflowPath,
    lines,
    gateJob,
    mainProtection,
    metadata,
    gate,
  );
  const artifactLines = artifactProducerLines(lines);
  if (artifactLines.length === 0) {
    fail(workflowPath, "must contain at least one artifact producer");
  }
  const jobsByID = new Map(jobs.map((job) => [job.id, job]));
  const dependencies = new Map(
    jobs.map((job) => [job.id, parseJobNeeds(lines, job)]),
  );
  for (const [jobID, needs] of dependencies) {
    for (const dependency of needs) {
      if (!jobsByID.has(dependency)) {
        fail(workflowPath, `references unknown dependency job '${dependency}'`);
      }
    }
    if (jobID === gateJob.id && needs.length !== 0) {
      fail(
        workflowPath,
        `Cloud gate job '${gateJob.id}' must not depend on another job`,
      );
    }
  }
  const reachesGate = (jobID, visiting = new Set()) => {
    if (jobID === gateJob.id) {
      return true;
    }
    if (visiting.has(jobID)) {
      return false;
    }
    const next = new Set(visiting);
    next.add(jobID);
    return (dependencies.get(jobID) ?? []).some((dependency) =>
      reachesGate(dependency, next),
    );
  };
  for (const job of jobs) {
    if (job.id !== gateJob.id && !reachesGate(job.id)) {
      fail(
        workflowPath,
        `job '${job.id}' must depend on Cloud gate job '${gateJob.id}'`,
      );
    }
    assertNoStatusBypass(workflowPath, lines, job);
  }
  for (const artifactLine of artifactLines) {
    const artifactJob = jobForLine(jobs, artifactLine);
    if (!artifactJob) {
      fail(
        workflowPath,
        `artifact producer at line ${artifactLine + 1} is outside a job`,
      );
    }
    assertArtifactStepStatus(workflowPath, lines, artifactJob, artifactLine);
    if (artifactJob.id === gateJob.id) {
      fail(
        workflowPath,
        `Cloud gate job '${gateJob.id}' must not build, upload, or publish artifacts`,
      );
    }
  }
  console.log(
    `[verify-cloud-workflow-gate] OK: ${path.basename(workflowPath)} metadata -> Cloud gate -> ${artifactLines.length} artifact producer marker(s)`,
  );
}
if (workflowPaths.length === 0) {
  console.error(
    "[verify-cloud-workflow-gate] ERROR: at least one workflow path is required",
  );
  process.exit(1);
}
for (const workflowPath of workflowPaths) {
  verifyWorkflow(workflowPath);
}
