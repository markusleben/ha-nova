#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RAW_TAG="${1:-}"
REPO_OVERRIDE="${2:-}"
ALLOW_EXISTING_TAG="${HA_NOVA_ALLOW_EXISTING_RELEASE_TAG:-0}"

node - "${ROOT_DIR}" "${RAW_TAG}" "${REPO_OVERRIDE}" "${ALLOW_EXISTING_TAG}" <<'NODE'
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const [rootDir, rawTag, repoOverride, allowExistingTagRaw] = process.argv.slice(2);

function fail(message) {
  console.error(`[verify-next-release-version] ERROR: ${message}`);
  process.exit(1);
}

function readJSON(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(rootDir, relativePath), "utf8"));
}

function parseRepo(raw) {
  const trimmed = String(raw || "").trim();
  if (!trimmed) {
    return "";
  }
  const sshMatch = trimmed.match(/github\.com[:/]([^/]+\/[^/.]+?)(?:\.git)?$/);
  if (sshMatch) {
    return sshMatch[1];
  }
  return trimmed.replace(/^https:\/\/github\.com\//, "").replace(/\.git$/, "");
}

function parseSemver(raw, label) {
  const match = String(raw || "").match(/^(\d+)\.(\d+)\.(\d+)$/);
  if (!match) {
    fail(`${label} must be semver X.Y.Z, got ${raw}`);
  }
  return match.slice(1).map((part) => Number(part));
}

function compareSemver(a, b) {
  for (let i = 0; i < 3; i += 1) {
    if (a[i] !== b[i]) {
      return a[i] > b[i] ? 1 : -1;
    }
  }
  return 0;
}

const versionJson = readJSON("version.json");
const packageJson = readJSON("package.json");
const expectedVersion = versionJson.skill_version;
if (packageJson.version !== expectedVersion) {
  fail(`package.json version ${packageJson.version} does not match version.json ${expectedVersion}`);
}

let targetVersion = expectedVersion;
let normalizedTag = "";
if (rawTag) {
  if (!/^v\d+\.\d+\.\d+(?:-rc\d+)?$/.test(rawTag)) {
    fail(`tag must match vX.Y.Z or vX.Y.Z-rcN, got ${rawTag}`);
  }
  normalizedTag = rawTag.trim();
  targetVersion = normalizedTag.slice(1).replace(/-rc\d+$/, "");
}

const repo =
  parseRepo(repoOverride) ||
  parseRepo(process.env.GITHUB_REPOSITORY) ||
  parseRepo(packageJson.repository?.url);
if (!repo) {
  fail("cannot determine GitHub repository");
}

let releases;
try {
  // Project down to the three fields this check reads BEFORE the payload
  // crosses the process boundary: full release objects carry the complete
  // notes body per release, and the default spawnSync maxBuffer (1 MiB)
  // killed the v0.14.0 publish with ENOBUFS once the RC's notes pushed the
  // total over it. The raised maxBuffer is the backstop for very long tag
  // histories.
  const ndjson = execFileSync(
    "gh",
    [
      "api",
      "--paginate",
      "--jq",
      ".[] | {tag_name, draft, prerelease}",
      `repos/${repo}/releases?per_page=100`,
    ],
    {
      cwd: rootDir,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      maxBuffer: 64 * 1024 * 1024,
    },
  );
  releases = ndjson
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => JSON.parse(line));
} catch (error) {
  const detail = error.stderr || error.message || String(error);
  fail(`gh api failed for ${repo}: ${detail.trim()}`);
}

if (!Array.isArray(releases)) {
  fail(`unexpected GitHub release payload for ${repo}`);
}

const allowExistingTag = /^(1|true|yes)$/i.test(String(allowExistingTagRaw || "").trim());
const exactRelease = normalizedTag
  ? releases.find((release) => String(release.tag_name || "").trim() === normalizedTag)
  : null;
if (normalizedTag && exactRelease && !allowExistingTag) {
  fail(`tag ${normalizedTag} already exists on GitHub releases`);
}

const latestStable = releases.find((release) => !release.draft && !release.prerelease);
if (!latestStable) {
  console.log(`[verify-next-release-version] OK: ${targetVersion} is the first published stable release for ${repo}`);
  process.exit(0);
}

const latestTag = String(latestStable.tag_name || "").trim();
const latestVersion = latestTag.replace(/^v/, "");
const targetParts = parseSemver(targetVersion, "target version");
const latestParts = parseSemver(latestVersion, "latest stable release");
if (
  compareSemver(targetParts, latestParts) <= 0 &&
  !(allowExistingTag && exactRelease && !exactRelease.draft && !exactRelease.prerelease && latestTag === normalizedTag)
) {
  fail(`target version ${targetVersion} must be newer than latest published stable ${latestTag}`);
}

if (allowExistingTag && exactRelease) {
  console.log(`[verify-next-release-version] OK: rerun allowed for existing ${normalizedTag} in ${repo}`);
  process.exit(0);
}

console.log(`[verify-next-release-version] OK: ${targetVersion} is newer than latest published stable ${latestTag}`);
NODE
