#!/usr/bin/env node

import { createHash } from "node:crypto";
import { appendFileSync, readFileSync } from "node:fs";

const eventName = process.env.GITHUB_EVENT_NAME ?? "";
const eventPath = process.env.GITHUB_EVENT_PATH ?? "";
const outputPath = process.env.GITHUB_OUTPUT ?? "";
const policyPath = process.argv[2] ?? "";

function fail(message) {
  console.error(`[resolve-dependabot-auto-merge-trigger] ERROR: ${message}`);
  process.exit(1);
}

function readJSON(path, label) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    fail(`${label} must contain valid JSON`);
  }
}

function requireSHA(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  return value;
}

function writeOutput(name, value) {
  appendFileSync(outputPath, `${name}=${value}\n`, "utf8");
}

if (eventPath.length === 0 || outputPath.length === 0) {
  fail("GitHub Actions event and output paths are required");
}
if (policyPath.length === 0) {
  fail("repository policy path is required");
}

const event = readJSON(eventPath, "GitHub event");
const policyBytes = readFileSync(policyPath);
const policy = readJSON(policyPath, "repository policy");
const policySHA = createHash("sha256").update(policyBytes).digest("hex");
const checkName = policy.cloud_source_gate?.check_name;
const appSlug = policy.cloud_source_gate?.reporter_app_slug;
const appId = policy.cloud_source_gate?.reporter_app_id;
const invalidatorName =
  policy.cloud_source_gate?.synchronous_invalidator_check_name;
const invalidatorSlug =
  policy.cloud_source_gate?.synchronous_invalidator_app_slug;
const invalidatorAppId =
  policy.cloud_source_gate?.synchronous_invalidator_app_id;

if (
  typeof checkName !== "string" ||
  checkName.length === 0 ||
  typeof appSlug !== "string" ||
  appSlug.length === 0 ||
  !Number.isSafeInteger(appId) ||
  appId < 0 ||
  typeof invalidatorName !== "string" ||
  invalidatorName.length === 0 ||
  typeof invalidatorSlug !== "string" ||
  invalidatorSlug.length === 0 ||
  !Number.isSafeInteger(invalidatorAppId) ||
  invalidatorAppId < 0
) {
  fail("Cloud source check policy is invalid");
}

writeOutput("should-process", "false");
writeOutput("policy-sha", policySHA);

if (eventName === "workflow_run") {
  const run = event.workflow_run;
  if (
    event.action !== "completed" ||
    run?.status !== "completed" ||
    run?.conclusion !== "success" ||
    !Number.isSafeInteger(run.id) ||
    run.id <= 0
  ) {
    process.exit(0);
  }
  writeOutput("run-kind", "workflow_run");
  writeOutput("run-id", String(run.id));
  writeOutput("run-sha", requireSHA(run.head_sha, "workflow run head SHA"));
  writeOutput("should-process", "true");
  process.exit(0);
}

if (eventName === "check_run") {
  const check = event.check_run;
  const exactAppCheck =
    (appId > 0 &&
      check?.name === checkName &&
      check?.app?.id === appId &&
      check?.app?.slug === appSlug) ||
    (invalidatorAppId > 0 &&
      check?.name === invalidatorName &&
      check?.app?.id === invalidatorAppId &&
      check?.app?.slug === invalidatorSlug);
  if (
    event.action !== "completed" ||
    check?.status !== "completed" ||
    check?.conclusion !== "success" ||
    !exactAppCheck
  ) {
    process.exit(0);
  }
  writeOutput("run-kind", "check_run");
  writeOutput("run-id", "");
  writeOutput("run-sha", requireSHA(check.head_sha, "check run head SHA"));
  writeOutput("should-process", "true");
  process.exit(0);
}

fail(`unsupported event ${eventName}`);
