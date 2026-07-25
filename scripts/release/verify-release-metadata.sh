#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TAG_NAME="${1:-}"

node - "${ROOT_DIR}" "${TAG_NAME}" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");

const [rootDir, rawTag] = process.argv.slice(2);

function fail(message) {
  console.error(`[verify-release-metadata] ERROR: ${message}`);
  process.exit(1);
}

function readJSON(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(rootDir, relativePath), "utf8"));
}

function assertEqual(label, actual, expected) {
  if (actual !== expected) {
    fail(`${label} expected ${expected} but found ${actual}`);
  }
}

const versionJson = readJSON("version.json");
const packageJson = readJSON("package.json");
const packageLock = readJSON("package-lock.json");
const pluginJson = readJSON(".claude-plugin/plugin.json");
const marketplaceJson = readJSON(".claude-plugin/marketplace.json");

const expectedVersion = versionJson.skill_version;
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(expectedVersion)) {
  fail(`version.json skill_version must be semver, got ${expectedVersion}`);
}

assertEqual("package.json version", packageJson.version, expectedVersion);
assertEqual("package-lock.json version", packageLock.version, expectedVersion);
assertEqual("package-lock.json packages[\"\"] version", packageLock.packages?.[""]?.version, expectedVersion);
assertEqual(".claude-plugin/plugin.json version", pluginJson.version, expectedVersion);

if (!marketplaceJson.metadata || typeof marketplaceJson.metadata !== "object") {
  fail(".claude-plugin/marketplace.json metadata block missing");
}
if (!marketplaceJson.metadata.description) {
  fail(".claude-plugin/marketplace.json metadata.description missing");
}
assertEqual(".claude-plugin/marketplace.json metadata.version", marketplaceJson.metadata.version, expectedVersion);

if (!Array.isArray(marketplaceJson.plugins) || marketplaceJson.plugins.length !== 1) {
  fail(".claude-plugin/marketplace.json must define exactly one plugin entry");
}

const pluginEntry = marketplaceJson.plugins[0];
assertEqual(".claude-plugin/marketplace.json plugins[0].version", pluginEntry.version, expectedVersion);
assertEqual(".claude-plugin/marketplace.json plugins[0].source", pluginEntry.source, "./");

if (rawTag) {
  if (!/^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc[1-9]\d*)?$/.test(rawTag)) {
    fail(`tag must match vX.Y.Z or vX.Y.Z-rcN, got ${rawTag}`);
  }
  const tagBaseVersion = rawTag.slice(1).replace(/-rc[1-9]\d*$/, "");
  assertEqual("tag/version.json base version", tagBaseVersion, expectedVersion);
}

console.log(`[verify-release-metadata] OK: ${expectedVersion}`);
NODE
