#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { basename } from "node:path";
import { workflowSyntaxProblem } from "./verify-cloud-workflow-syntax.mjs";

const [workflowPath, resolverPath] = process.argv.slice(2);

function fail(message) {
  console.error(`[verify-cloud-candidate-workflow] ERROR: ${message}`);
  process.exit(1);
}

function read(path) {
  try {
    return readFileSync(path, "utf8");
  } catch {
    fail(`${basename(path ?? "")} must exist and be readable`);
  }
}

function requireText(source, text, label) {
  if (!source.includes(text)) {
    fail(`${label} is missing`);
  }
}

function requireCount(source, text, expected, label) {
  const actual = source.split(text).length - 1;
  if (actual !== expected) {
    fail(`${label} must occur exactly ${expected} time(s), found ${actual}`);
  }
}

function jobBody(source, name, nextName) {
  const start = source.indexOf(`  ${name}:`);
  const end = nextName === undefined
    ? source.length
    : source.indexOf(`  ${nextName}:`, start + 1);
  if (start < 0 || end < 0) {
    fail(`workflow job ${name} is missing`);
  }
  return source.slice(start, end);
}

function runBodies(source) {
  const lines = source.split(/\r?\n/);
  const bodies = [];
  for (let index = 0; index < lines.length; index += 1) {
    const match = /^(\s*)run:\s*(.*)$/.exec(lines[index]);
    if (match === null) continue;
    const indent = match[1].length;
    const body = [match[2]];
    while (index + 1 < lines.length) {
      const next = lines[index + 1];
      if (next.trim() !== "" && next.length - next.trimStart().length <= indent) {
        break;
      }
      body.push(next);
      index += 1;
    }
    bodies.push(body.join("\n"));
  }
  return bodies;
}

if (!workflowPath || !resolverPath) {
  fail("workflow and resolver paths are required");
}
const workflow = read(workflowPath);
const resolver = read(resolverPath);
const syntaxProblem = workflowSyntaxProblem(workflow.split(/\r?\n/));
if (syntaxProblem !== null) {
  fail(`${basename(workflowPath)} ${syntaxProblem}`);
}

requireText(workflow, "name: Cloud Candidate Bundle", "workflow name");
requireText(
  workflow,
  "on:\n  workflow_dispatch:\n    inputs:",
  "manual-only trigger",
);
requireText(workflow, "pull_request:", "pull request input");
requireText(workflow, "version_tag:", "version input");
requireText(workflow, "request_id:", "dispatch recovery input");
requireText(
  workflow,
  "run-name: Cloud candidate PR #${{ inputs.pull_request }} ${{ inputs.version_tag }} (${{ inputs.request_id }})",
  "unique recoverable run name",
);
requireText(
  workflow,
  "group: cloud-candidate-bundle-${{ inputs.pull_request }}",
  "per-pull-request concurrency",
);
requireText(workflow, "cancel-in-progress: true", "duplicate-run cancellation");
requireText(
  workflow,
  "ref: ${{ steps.source.outputs.commit_sha }}",
  "exact resolved candidate checkout",
);
requireText(
  workflow,
  "ref: ${{ needs.resolve-source.outputs.commit_sha }}",
  "downstream exact candidate checkout",
);
requireText(
  workflow,
  "bash trusted/scripts/release/build-rc-binaries.sh",
  "trusted Linux and Windows builder",
);
requireText(
  workflow,
  "bash trusted/scripts/release/build-sign-darwin-binaries.sh",
  "trusted Darwin builder",
);
requireText(
  workflow,
  "bash scripts/release/build-install-bundle.sh",
  "trusted bundle builder",
);
requireText(
  workflow,
  "internal-cloud-release-check",
  "official provenance smoke",
);
requireText(
  workflow,
  "verify-cloud-release-evidence.mjs",
  "public-key verification for every signed archive",
);
requireText(
  workflow,
  "candidate bundle version does not match workflow input",
  "Unix bundle-version assertion",
);
requireText(
  workflow,
  "Raw Windows binary accepted missing Cloud provenance",
  "Windows raw-binary provenance rejection",
);
requireCount(
  workflow,
  'chmod 755 "$binary"',
  2,
  "restored Unix executable mode after artifact transport",
);
requireCount(
  workflow,
  "scripts/release/resolve-cloud-candidate-source.sh",
  4,
  "final complete-state revalidation",
);
requireText(
  workflow,
  "name: cloud-candidate-raw-binaries",
  "raw-binary transport artifact",
);
requireText(
  workflow,
  "name: cloud-candidate-install-bundles",
  "final smoke-tested artifact",
);
requireCount(
  workflow,
  "environment:\n      name: production\n      deployment: false",
  2,
  "non-deploying production environment binding",
);
requireCount(
  workflow,
  "secrets.HA_NOVA_MACOS_CERTIFICATE_P12_BASE64",
  1,
  "macOS certificate binding",
);
requireCount(
  workflow,
  "secrets.HA_NOVA_MACOS_CERTIFICATE_PASSWORD",
  1,
  "macOS certificate password binding",
);
requireCount(
  workflow,
  "secrets.HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM",
  1,
  "Cloud provenance signing-key binding",
);
requireCount(workflow, "retention-days: 1", 2, "one-day raw artifact retention");
requireCount(workflow, "retention-days: 7", 1, "seven-day final artifact retention");
if (workflow.includes("cloud-candidate-install-bundles-staging")) {
  fail("signed candidate bundles must not be uploaded before final validation");
}
for (const [name, nextName] of [
  ["build-signed-darwin", "build-raw-binaries"],
  ["build-raw-binaries", "smoke-darwin-binary"],
  ["finalize-candidate", undefined],
]) {
  if (jobBody(workflow, name, nextName).includes("internal-cloud-release-check")) {
    fail(`artifact producer ${name} must not execute candidate binaries`);
  }
}

if (runBodies(workflow).some((body) => body.includes("${{ inputs."))) {
  fail("workflow_dispatch inputs must reach run scripts only through env");
}

for (const forbidden of [
  /\bgh\s+release\b/i,
  /\bgit\s+tag\b/i,
  /\brelease\.yml\b/,
  /HA_NOVA_CLOUD_GATE_EVIDENCE_JSON/,
  /(?:bash|node|\.)\s+["']?(?:\$GITHUB_WORKSPACE\/)?target\//,
]) {
  if (forbidden.test(workflow)) {
    fail(
      `workflow contains forbidden publication or target-script path: ${forbidden}`,
    );
  }
}

for (const marker of [
  '[[ "${GITHUB_EVENT_NAME:-}" == "workflow_dispatch" ]]',
  '[[ "${GITHUB_REF:-}" == "refs/heads/main" ]]',
  '[[ "${REPO}" == "markusleben/ha-nova" ]]',
  '[[ "${GITHUB_ACTOR:-}" == "markusleben"',
  '"${GITHUB_ACTOR_ID:-}" == "6522814"',
  '[[ "${GITHUB_TRIGGERING_ACTOR:-}" == "markusleben"',
  '"${GITHUB_RUN_ATTEMPT:-}" == "1"',
  '[[ "${trusted_head}" == "${GITHUB_SHA}" ]]',
  ".draft == false",
  '.base.ref == "main"',
  '.head.repo.full_name == "markusleben/ha-nova"',
  '[[ "${base_sha}" == "${GITHUB_SHA}" ]]',
  '[[ "${remote_main_sha}" == "${GITHUB_SHA}" ]]',
  'source_ref="refs/pull/${PR_NUMBER}/merge"',
  '[[ "${remote_sha}" == "${merge_sha}" ]]',
  ".parents[0].sha == $base",
  ".parents[1].sha == $head",
  '.required_status_checks - ["cloud-source-gate"]',
  "current pull request head lacks a real clean Codex bot result",
  '.user.login == "chatgpt-codex-connector[bot]"',
  ".user.id == 199175422",
  '.user.type == "Bot"',
  '(.user.type == "User" or .user.type == "Bot")',
  "pull request has requested changes or unresolved review threads",
  "a later Codex inline finding supersedes the clean result",
  '**Reviewed commit:** `" + $prefix + "`',
  '.name == "cloud-source-gate" and .app.id == $app_id',
  '.conclusion == "failure"',
  "bash scripts/release/verify-cloud-target-source-gate.sh candidate",
  "pull request changed while candidate source was resolved",
  "final pull request merge ref",
]) {
  requireText(resolver, marker, `resolver guard ${marker}`);
}

console.log(
  "[verify-cloud-candidate-workflow] OK: exact reviewed source -> native raw-binary smoke -> final signed bundles -> complete-state check; no publication",
);
