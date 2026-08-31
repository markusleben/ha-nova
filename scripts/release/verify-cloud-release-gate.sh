#!/usr/bin/env bash
set -euo pipefail

TARGET_ROOT_MODE=0
if [[ "$#" -gt 0 ]]; then
  TARGET_ROOT_MODE=1
fi
ROOT_DIR="${1:-$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
TRUSTED_REPO_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EVIDENCE_MODE="${2:-require-evidence}"

node - "${ROOT_DIR}" "${TARGET_ROOT_MODE}" "${TRUSTED_REPO_ROOT}" "${EVIDENCE_MODE}" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const [rootDir, targetRootMode, trustedRepoRoot, evidenceMode] =
  process.argv.slice(2);
const allowedPlatforms = new Set(["darwin", "linux", "windows"]);
const requiredChecks = [
  "domains_mfa",
  "lifecycle",
  "parity",
  "redirects_non_disclosure",
  "installed_relay_app",
  "roles",
  "routing",
  "signing_and_update_matrix",
  "stress_10000",
];
const maxEvidenceBytes = 32 * 1024;

if (!["require-evidence", "metadata-only"].includes(evidenceMode)) {
  fail("evidence mode must be require-evidence or metadata-only");
}

function fail(message) {
  console.error(`[verify-cloud-release-gate] ERROR: ${message}`);
  process.exit(1);
}

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function requireExactKeys(value, expected, label) {
  const actual = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (
    actual.length !== wanted.length ||
    actual.some((key, index) => key !== wanted[index])
  ) {
    fail(`${label} must contain exactly the schema-defined fields`);
  }
}

function readVersionMetadata(relativePath) {
  try {
    return JSON.parse(
      fs.readFileSync(path.join(rootDir, relativePath), "utf8"),
    );
  } catch {
    fail(`${relativePath} must contain valid JSON`);
  }
}

function readRelayAppVersion() {
  let raw;
  try {
    raw = fs.readFileSync(path.join(rootDir, "nova/config.yaml"), "utf8");
  } catch {
    fail("nova/config.yaml must exist while Cloud remote is enabled");
  }
  const matches = [...raw.matchAll(/^version:\s*"([^"]+)"\s*$/gm)];
  if (matches.length !== 1 || matches[0][1].length === 0) {
    fail("nova/config.yaml must contain exactly one quoted App version");
  }
  return matches[0][1];
}

function parseReleaseVersion(value, label) {
  if (typeof value !== "string") {
    fail(`${label} must be a strict three-part release version`);
  }
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.exec(value);
  if (match === null) {
    fail(`${label} must be a strict three-part release version`);
  }
  return match.slice(1).map((part) => BigInt(part));
}

function compareVersions(left, right) {
  for (let index = 0; index < 3; index += 1) {
    if (left[index] !== right[index]) {
      return left[index] < right[index] ? -1 : 1;
    }
  }
  return 0;
}

function validateCloudMetadata(version, relativePath) {
  if (!isObject(version)) {
    fail(`${relativePath} must contain a JSON object`);
  }
  if (typeof version.cloud_remote_enabled !== "boolean") {
    fail(`${relativePath} cloud_remote_enabled must be a boolean`);
  }
  if (!Array.isArray(version.cloud_remote_platforms)) {
    fail(`${relativePath} cloud_remote_platforms must be an array`);
  }
}

function requireSHA(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value)) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
}

function readTargetIdentity() {
  const targetCommit = process.env.HA_NOVA_CLOUD_GATE_TARGET_COMMIT ?? "";
  const targetTree = process.env.HA_NOVA_CLOUD_GATE_TARGET_TREE ?? "";
  const trustedPRMode =
    process.env.HA_NOVA_CLOUD_GATE_TRUSTED_PR_MODE === "1";
  if ((targetCommit.length === 0) !== (targetTree.length === 0)) {
    fail("Cloud gate target commit and tree must be provided together");
  }
  if (targetCommit.length > 0) {
    if (!trustedPRMode || targetRootMode !== "1") {
      fail("Cloud gate target override is reserved for trusted PR mode");
    }
    requireSHA(targetCommit, "Cloud gate target commit");
    requireSHA(targetTree, "Cloud gate target tree");
    return { commit: targetCommit, tree: targetTree };
  }

  let commit;
  let tree;
  try {
    commit = execFileSync(
      "git",
      ["rev-parse", "--verify", "HEAD"],
      {
        cwd: rootDir,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      },
    ).trim();
    tree = execFileSync(
      "git",
      ["rev-parse", "--verify", "HEAD^{tree}"],
      {
        cwd: rootDir,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      },
    ).trim();
  } catch {
    fail("cannot resolve the checked-out Git commit and tree");
  }
  requireSHA(commit, "Checked-out Git commit");
  requireSHA(tree, "Checked-out Git tree");
  const workflowCommit = process.env.GITHUB_SHA ?? "";
  if (workflowCommit) {
    requireSHA(workflowCommit, "GITHUB_SHA");
    if (workflowCommit !== commit) {
      fail("checked-out Git commit does not match GITHUB_SHA");
    }
  }
  return { commit, tree };
}

function validateEvidenceIdentity(evidence, target) {
  requireSHA(
    evidence.commit_sha,
    "Home Assistant Cloud gate evidence commit_sha",
  );
  requireSHA(
    evidence.tree_sha,
    "Home Assistant Cloud gate evidence tree_sha",
  );
  let evidenceCommitTree;
  let targetCommitTree;
  try {
    evidenceCommitTree = execFileSync(
      "git",
      ["rev-parse", "--verify", `${evidence.commit_sha}^{tree}`],
      {
        cwd: trustedRepoRoot,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      },
    ).trim();
  } catch {
    try {
      execFileSync(
        "git",
        [
          "fetch",
          "--no-tags",
          "--depth=1",
          "origin",
          evidence.commit_sha,
        ],
        {
          cwd: trustedRepoRoot,
          stdio: ["ignore", "ignore", "ignore"],
        },
      );
      evidenceCommitTree = execFileSync(
        "git",
        ["rev-parse", "--verify", `${evidence.commit_sha}^{tree}`],
        {
          cwd: trustedRepoRoot,
          encoding: "utf8",
          stdio: ["ignore", "pipe", "ignore"],
        },
      ).trim();
    } catch {
      fail("Home Assistant Cloud evidence commit must exist in the repository");
    }
  }
  try {
    targetCommitTree = execFileSync(
      "git",
      ["rev-parse", "--verify", `${target.commit}^{tree}`],
      {
        cwd: trustedRepoRoot,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      },
    ).trim();
  } catch {
    fail("Home Assistant Cloud target commit must exist locally");
  }
  if (evidenceCommitTree !== evidence.tree_sha) {
    fail(
      "Home Assistant Cloud gate evidence tree_sha must exactly match its evidence commit",
    );
  }
  if (targetCommitTree !== target.tree) {
    fail("Home Assistant Cloud gate target tree must match its target commit");
  }
  if (
    process.env.HA_NOVA_CLOUD_GATE_REQUIRE_EXACT_EVIDENCE === "1" &&
    (evidence.commit_sha !== target.commit || evidence.tree_sha !== target.tree)
  ) {
    fail(
      "approved sensitive workflow changes require evidence for the exact target commit and tree",
    );
  }
  if (evidence.tree_sha === target.tree) {
    return;
  }
  try {
    execFileSync(
      "node",
      [
        path.join(
          trustedRepoRoot,
          "scripts/release/verify-cloud-workflow-uses-only.mjs",
        ),
        trustedRepoRoot,
        evidence.commit_sha,
        target.commit,
      ],
      { stdio: ["ignore", "ignore", "pipe"] },
    );
  } catch {
    try {
      execFileSync(
        "node",
        [
          path.join(
            trustedRepoRoot,
            "scripts/release/verify-cloud-nonsensitive-source.mjs",
          ),
          trustedRepoRoot,
          evidence.commit_sha,
          target.commit,
        ],
        { stdio: ["ignore", "ignore", "pipe"] },
      );
    } catch {
      fail(
        "stale Home Assistant Cloud evidence may cover only ancestor-to-target existing non-sensitive uses: version bumps or a delta confined to docs/, skills/, and root Markdown without install-command changes",
      );
    }
  }
}

const version = readVersionMetadata("version.json");
const appVersion = readVersionMetadata("nova/version.json");
validateCloudMetadata(version, "version.json");
validateCloudMetadata(appVersion, "nova/version.json");

if (
  version.cloud_remote_enabled !== appVersion.cloud_remote_enabled ||
  JSON.stringify(version.cloud_remote_platforms) !==
    JSON.stringify(appVersion.cloud_remote_platforms) ||
  version.min_relay_version !== appVersion.min_relay_version
) {
  fail(
    "version.json and nova/version.json Cloud release metadata must match exactly",
  );
}

const platforms = version.cloud_remote_platforms;
if (!version.cloud_remote_enabled) {
  if (platforms.length !== 0) {
    fail(
      "version.json cloud_remote_platforms must be empty while Cloud remote is disabled",
    );
  }
  console.log(
    "[verify-cloud-release-gate] OK: Cloud remote disabled; no external evidence required",
  );
  process.exit(0);
}

if (platforms.length === 0) {
  fail(
    "version.json cloud_remote_platforms must list at least one enabled platform",
  );
}
if (
  platforms.some(
    (platform) =>
      typeof platform !== "string" || !allowedPlatforms.has(platform),
  )
) {
  fail(
    "version.json cloud_remote_platforms may contain only darwin, linux, and windows",
  );
}
if (new Set(platforms).size !== platforms.length) {
  fail("version.json cloud_remote_platforms must not contain duplicates");
}
const relayAppVersion = readRelayAppVersion();
if (
  typeof version.min_relay_version !== "string" ||
  version.min_relay_version !== relayAppVersion
) {
  fail(
    "version.json min_relay_version must exactly match nova/config.yaml while Cloud remote is enabled",
  );
}
if (
  compareVersions(
    parseReleaseVersion(relayAppVersion, "nova/config.yaml version"),
    parseReleaseVersion("0.7.1", "pre-Cloud Relay App version"),
  ) <= 0
) {
  fail(
    "Cloud remote requires a Relay App version newer than the pre-Cloud 0.7.1 release",
  );
}

if (evidenceMode === "metadata-only") {
  if (
    targetRootMode !== "1" ||
    process.env.HA_NOVA_CLOUD_GATE_TRUSTED_PR_MODE !== "1"
  ) {
    fail("metadata-only verification is reserved for trusted candidate source checks");
  }
  console.log(
    `[verify-cloud-release-gate] OK: enabled Cloud metadata verified for ${platforms.length} platform(s); external evidence intentionally pending`,
  );
  process.exit(0);
}

const rawEvidence =
  process.env.HA_NOVA_CLOUD_GATE_EVIDENCE_JSON ?? "";
if (rawEvidence.length === 0) {
  fail(
    "HA_NOVA_CLOUD_GATE_EVIDENCE_JSON is required while Cloud remote is enabled",
  );
}
if (Buffer.byteLength(rawEvidence, "utf8") > maxEvidenceBytes) {
  fail("Home Assistant Cloud gate evidence exceeds the 32 KiB limit");
}

let evidence;
try {
  evidence = JSON.parse(rawEvidence);
} catch {
  fail("Home Assistant Cloud gate evidence must be valid JSON");
}
if (!isObject(evidence)) {
  fail("Home Assistant Cloud gate evidence must be a JSON object");
}
requireExactKeys(
  evidence,
  ["schema", "commit_sha", "tree_sha", "checks", "relay_app"],
  "evidence",
);
if (evidence.schema !== 2) {
  fail("Home Assistant Cloud gate evidence schema must equal 2");
}

const target = readTargetIdentity();
validateEvidenceIdentity(evidence, target);

if (!isObject(evidence.relay_app)) {
  fail("Home Assistant Cloud relay_app evidence must be a JSON object");
}
requireExactKeys(
  evidence.relay_app,
  ["version", "source_commit", "source_tree_sha"],
  "evidence relay_app",
);
if (evidence.relay_app.version !== relayAppVersion) {
  fail(
    "Home Assistant Cloud relay_app evidence version does not match nova/config.yaml",
  );
}
if (evidence.relay_app.source_commit !== evidence.commit_sha) {
  fail("Home Assistant Cloud relay_app source_commit does not match evidence commit_sha");
}
if (evidence.relay_app.source_tree_sha !== evidence.tree_sha) {
  fail("Home Assistant Cloud relay_app source_tree_sha does not match evidence tree_sha");
}

if (!isObject(evidence.checks)) {
  fail("Home Assistant Cloud gate evidence checks must be a JSON object");
}
requireExactKeys(
  evidence.checks,
  [...requiredChecks, "keyrings"],
  "evidence checks",
);
for (const check of requiredChecks) {
  if (evidence.checks[check] !== true) {
    fail(`Home Assistant Cloud gate check ${check} must equal true`);
  }
}

const keyrings = evidence.checks.keyrings;
if (!isObject(keyrings)) {
  fail("Home Assistant Cloud gate keyrings must be a JSON object");
}
requireExactKeys(keyrings, platforms, "evidence keyrings");
for (const platform of platforms) {
  if (keyrings[platform] !== true) {
    fail(
      `Home Assistant Cloud gate keyring check for ${platform} must equal true`,
    );
  }
}

console.log(
  `[verify-cloud-release-gate] OK: full-tree-bound Cloud evidence verified for ${platforms.length} platform(s)`,
);
NODE
