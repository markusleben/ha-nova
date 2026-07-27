#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const [rootDir, baseCommit, targetCommit, mode = "full-tree"] =
  process.argv.slice(2);
const workflowPrefix = ".github/workflows/";
const policy = JSON.parse(
  readFileSync(join(rootDir, ".github/policy/repo-policy.json"), "utf8"),
);
const sensitive = new Set(policy.cloud_source_gate?.sensitive_workflows ?? []);
const resolvedTags = new Map();

function fail(message) {
  console.error(`[verify-cloud-workflow-uses-only] ERROR: ${message}`);
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

function workflowEntries(ref) {
  const raw = git(
    ["ls-tree", "-r", "-z", ref, "--", ".github/workflows"],
    null,
  );
  const entries = new Map();
  for (const record of raw.toString("utf8").split("\0").filter(Boolean)) {
    const match = /^([0-7]{6}) (blob) ([0-9a-f]{40})\t(.+)$/.exec(record);
    if (match === null) {
      fail("workflow tree contains an unsupported entry");
    }
    const [, mode, type, blob, path] = match;
    if (
      type !== "blob" ||
      mode !== "100644" ||
      !path.startsWith(workflowPrefix) ||
      (!path.endsWith(".yml") && !path.endsWith(".yaml"))
    ) {
      fail(`workflow tree contains unsupported path ${path}`);
    }
    entries.set(path, { mode, blob });
  }
  return entries;
}

function requireCommit(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  git(["rev-parse", "--verify", `${value}^{commit}`]);
}

function actionReference(line) {
  const match =
    /^(\s*(?:-\s+)?uses:\s+)([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_.-]+)*)@([0-9a-f]{40})\s+#\s+(v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*))\s*$/.exec(
      line,
    );
  if (match === null) {
    return null;
  }
  const [, prefix, identity, ref, tag, major, minor, patch] = match;
  return {
    prefix,
    identity,
    ref,
    tag,
    version: [BigInt(major), BigInt(minor), BigInt(patch)],
  };
}

function compareVersions(left, right) {
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return left[index] < right[index] ? -1 : 1;
    }
  }
  return 0;
}

function ghJSON(endpoint) {
  try {
    return JSON.parse(
      execFileSync(
        "gh",
        [
          "api",
          "--header",
          "X-GitHub-Api-Version: 2026-03-10",
          endpoint,
        ],
        {
          encoding: "utf8",
          stdio: ["ignore", "pipe", "ignore"],
        },
      ),
    );
  } catch {
    fail(`cannot resolve immutable action release ${endpoint}`);
  }
}

function resolvedActionTag(identity, tag) {
  const repository = identity.split("/").slice(0, 2).join("/");
  const cacheKey = `${repository}@${tag}`;
  if (resolvedTags.has(cacheKey)) {
    return resolvedTags.get(cacheKey);
  }
  const ref = ghJSON(
    `repos/${repository}/git/ref/tags/${encodeURIComponent(tag)}`,
  );
  let object = ref?.object;
  for (let depth = 0; depth < 4 && object?.type === "tag"; depth += 1) {
    object = ghJSON(`repos/${repository}/git/tags/${object.sha}`)?.object;
  }
  if (object?.type !== "commit" || !/^[0-9a-f]{40}$/.test(object.sha ?? "")) {
    fail(`${repository}@${tag} must resolve to one immutable commit`);
  }
  resolvedTags.set(cacheKey, object.sha);
  return object.sha;
}

function verifyUsesOnly(path, baseRef, targetRef) {
  const before = git(["show", `${baseRef}:${path}`]).split(/\r?\n/);
  const after = git(["show", `${targetRef}:${path}`]).split(/\r?\n/);
  if (before.length !== after.length) {
    fail(`${path} may change only existing uses: references`);
  }
  let changes = 0;
  for (let index = 0; index < before.length; index += 1) {
    if (before[index] === after[index]) {
      continue;
    }
    changes += 1;
    const beforeAction = actionReference(before[index]);
    const afterAction = actionReference(after[index]);
    if (
      beforeAction === null ||
      afterAction === null ||
      beforeAction.prefix !== afterAction.prefix ||
      beforeAction.identity !== afterAction.identity ||
      beforeAction.ref === afterAction.ref ||
      beforeAction.version[0] !== afterAction.version[0] ||
      compareVersions(beforeAction.version, afterAction.version) >= 0
    ) {
      fail(
        `${path}:${index + 1} must be a forward minor/patch release update on an unchanged action`,
      );
    }
    if (
      resolvedActionTag(beforeAction.identity, beforeAction.tag) !==
        beforeAction.ref ||
      resolvedActionTag(afterAction.identity, afterAction.tag) !==
        afterAction.ref
    ) {
      fail(
        `${path}:${index + 1} action SHAs must match their canonical release tags`,
      );
    }
  }
  if (changes === 0) {
    fail(`${path} blob changed without a reviewable uses: delta`);
  }
}

requireCommit(baseCommit, "base commit");
requireCommit(targetCommit, "target commit");
if (mode !== "full-tree" && mode !== "workflow-tree-only") {
  fail("mode must be full-tree or workflow-tree-only");
}
if (
  !Array.isArray(policy.cloud_source_gate?.sensitive_workflows) ||
  sensitive.size !== policy.cloud_source_gate.sensitive_workflows.length
) {
  fail("sensitive workflow policy must be a duplicate-free array");
}
try {
  execFileSync(
    "git",
    ["-C", rootDir, "merge-base", "--is-ancestor", baseCommit, targetCommit],
    { stdio: "ignore" },
  );
} catch {
  fail("base commit must be an ancestor of the target commit");
}

if (mode === "full-tree") {
  const changedPaths = git([
    "diff",
    "--name-only",
    "-z",
    baseCommit,
    targetCommit,
  ])
    .split("\0")
    .filter(Boolean);
  if (
    changedPaths.length === 0 ||
    changedPaths.some((path) => !path.startsWith(workflowPrefix))
  ) {
    fail(
      "the complete evidence-to-target delta must contain only workflow files",
    );
  }
}

const base = workflowEntries(baseCommit);
const target = workflowEntries(targetCommit);
if (
  base.size !== target.size ||
  [...base.keys()].some((path) => !target.has(path))
) {
  fail("enabled Cloud source may not add, delete, or rename workflows");
}

let changed = 0;
for (const [path, baseEntry] of base) {
  const targetEntry = target.get(path);
  if (targetEntry.mode !== baseEntry.mode) {
    fail(`${path} file mode changed`);
  }
  if (targetEntry.blob === baseEntry.blob) {
    continue;
  }
  changed += 1;
  if (sensitive.has(path)) {
    fail(`${path} is Cloud-release-sensitive`);
  }
  verifyUsesOnly(path, baseCommit, targetCommit);
}
if (changed === 0) {
  fail("workflow tree changed without a workflow file delta");
}

console.log(
  `[verify-cloud-workflow-uses-only] OK: ${changed} non-sensitive workflow file(s)`,
);
