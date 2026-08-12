#!/usr/bin/env node

import { execFileSync } from "node:child_process";

const [rootDir, baseCommit, targetCommit] = process.argv.slice(2);

// A stale evidence envelope stays valid when the complete ancestor-to-target
// delta is confined to Markdown under docs/ or skills/, or root-level
// Markdown. Everything else (tests, scripts, workflows, runtime code,
// manifests, version metadata, installers, assets, dotfiles) needs fresh
// evidence. tests/ is deliberately excluded: privileged release workflows
// execute repository tests with production-environment secrets, so test
// content must stay attested. AGENTS.md is deliberately excluded: it is the
// executable policy of agents operating with maintainer credentials.
const allowedPath = /^(docs|skills)\/.+\.md$|^[^/]+\.md$/;
// Agent-policy basenames are denied at every depth and case-folded: agents
// load AGENTS.md/CLAUDE.md/GEMINI.md per subtree, and on case-insensitive
// checkouts an added lowercase alias materializes as the policy file.
const deniedBasenames = new Set(["agents.md", "claude.md", "gemini.md"]);

// Best-effort guard for the copy-paste surface users and agents execute
// blindly: any changed line that touches a download/install command or a
// raw-script source falls back to the full evidence path. This is a denylist
// and documented as best-effort; PR review remains the semantic control.
const installCommand =
  /(curl|wget|\biwr\b|\birm\b|invoke-webrequest|invoke-restmethod|invoke-expression|downloadstring|downloadfile|webclient|start-bitstransfer|\|\s*(bash|sh|iex)\b|\b(ba)?sh\s+-c\b|\b(npm|pnpm|yarn|bun)\s+(install|i|add|exec|dlx|create)\b|\bpipx?3?\s+install\b|\bgit\s+clone\b|\bgh\s+release\s+download\b|\bbrew\s+install\b|\bdocker\s+(run|pull|create)\b|install\.(sh|ps1)\b|raw\.githubusercontent\.com|cdn\.jsdelivr\.net|statically\.io|githack)/i;

const allowedModes = new Set(["000000", "100644"]);

function fail(message) {
  console.error(`[verify-cloud-nonsensitive-source] ERROR: ${message}`);
  process.exit(1);
}

function git(args, encoding = "utf8") {
  try {
    return execFileSync("git", ["-C", rootDir, ...args], {
      encoding,
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {
    fail(`git ${args[0]} failed`);
  }
}

function requireCommit(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  git(["rev-parse", "--verify", `${value}^{commit}`]);
}

requireCommit(baseCommit, "base commit");
requireCommit(targetCommit, "target commit");
try {
  execFileSync(
    "git",
    ["-C", rootDir, "merge-base", "--is-ancestor", baseCommit, targetCommit],
    { stdio: "ignore" },
  );
} catch {
  fail("base commit must be an ancestor of the target commit");
}

const records = git([
  "diff",
  "--raw",
  "--no-renames",
  "-z",
  baseCommit,
  targetCommit,
])
  .split("\0")
  .filter(Boolean);

// git diff --raw -z --no-renames alternates metadata and path fields:
// ":<oldmode> <newmode> <oldblob> <newblob> <status>", then the path.
const changedPaths = [];
for (let index = 0; index < records.length; index += 2) {
  const meta =
    /^:([0-7]{6}) ([0-7]{6}) [0-9a-f]+ [0-9a-f]+ ([A-Z])$/.exec(
      records[index],
    );
  const filePath = records[index + 1];
  if (meta === null || filePath === undefined) {
    fail("evidence-to-target delta contains an unsupported diff record");
  }
  const [, oldMode, newMode, status] = meta;
  if (!/^[\x20-\x7e]+$/.test(filePath)) {
    fail("evidence-to-target delta contains a non-ASCII path");
  }
  if (!["A", "M", "D"].includes(status)) {
    fail(`${filePath} uses unsupported change status ${status}`);
  }
  if (!allowedModes.has(oldMode) || !allowedModes.has(newMode)) {
    fail(`${filePath} must stay a regular non-executable file`);
  }
  const baseName = filePath.split("/").pop().toLowerCase();
  if (!allowedPath.test(filePath) || deniedBasenames.has(baseName)) {
    fail(`${filePath} is outside the non-sensitive source scope`);
  }
  changedPaths.push(filePath);
}

if (changedPaths.length === 0) {
  fail("evidence-to-target delta must not be empty");
}

for (const filePath of changedPaths) {
  // --text overrides in-tree diff attributes and binary heuristics so the
  // guard can never be blinded; :(literal) disables pathspec magic.
  const diffLines = git([
    "diff",
    "--no-renames",
    "--text",
    "-U0",
    baseCommit,
    targetCommit,
    "--",
    `:(literal)${filePath}`,
  ]).split(/\r?\n/);
  // Skip only the structural header block before the first hunk; after a
  // "@@" marker every +/- line is content, so header-shaped content lines
  // ("+++ b/...") can never dodge the scan.
  let sawHunk = false;
  for (const line of diffLines) {
    if (line.startsWith("@@")) {
      sawHunk = true;
      continue;
    }
    if (!sawHunk || !/^[+-]/.test(line)) {
      continue;
    }
    if (installCommand.test(line) || /\\$/.test(line)) {
      fail(
        `${filePath} changes an install-command or continuation line; full evidence required`,
      );
    }
  }
  if (!sawHunk) {
    fail(
      `${filePath} produced no scannable textual delta; full evidence required`,
    );
  }
}

console.log(
  `[verify-cloud-nonsensitive-source] OK: ${changedPaths.length} non-sensitive file(s)`,
);
