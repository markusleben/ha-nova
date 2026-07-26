#!/usr/bin/env node

import { appendFileSync, readFileSync } from "node:fs";

const outputPath = process.env.GITHUB_OUTPUT ?? "";

function fail(message) {
  console.error(`[resolve-cloud-source-gate-mode] ERROR: ${message}`);
  process.exit(1);
}

function readJSON(path, label) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    fail(`${label} must contain valid JSON`);
  }
}

function appID(value, label) {
  if (!Number.isSafeInteger(value) || value < 0) {
    fail(`${label} must be zero or a positive integer`);
  }
  return value;
}

if (outputPath.length === 0) {
  fail("GITHUB_OUTPUT is required");
}

const version = readJSON("version.json", "version.json");
const policy = readJSON(
  ".github/policy/repo-policy.json",
  "repository policy",
);
if (typeof version.cloud_remote_enabled !== "boolean") {
  fail("version.json cloud_remote_enabled must be a boolean");
}

const sourceGate = policy.cloud_source_gate;
if (sourceGate === null || typeof sourceGate !== "object") {
  fail("repository policy cloud_source_gate must be an object");
}
const reporterID = appID(sourceGate.reporter_app_id, "reporter App ID");
const invalidatorID = appID(
  sourceGate.synchronous_invalidator_app_id,
  "synchronous invalidator App ID",
);
const provisioned = reporterID > 0 && invalidatorID > 0;

if (version.cloud_remote_enabled && !provisioned) {
  fail("enabled Cloud Remote requires both source-gate App IDs");
}

const shouldRun = version.cloud_remote_enabled || provisioned;
appendFileSync(outputPath, `should-run=${shouldRun}\n`, {
  encoding: "utf8",
  mode: 0o600,
});
console.log(
  shouldRun
    ? "[resolve-cloud-source-gate-mode] Source gate enabled."
    : "[resolve-cloud-source-gate-mode] Cloud Remote and source-gate Apps are unprovisioned; skipping.",
);
