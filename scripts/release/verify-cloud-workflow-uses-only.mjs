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
// One-time exception (deep audit 2026-09-10, findings 1/2/11 — round 1 of the
// #578/#579 two-round path): the trust-boundary rewrite pins every mutable
// action tag across seven workflows, adds Go to a fail-closed CodeQL job, and
// fixes the watchdog trigger. ci.yml is Cloud-release-sensitive and the
// rewrite is not a uses:-only delta, so this predicate widens ONLY the
// single-sensitive-workflow lane (#622: refs/pull merge target, canonical
// candidate approval id, exact-target evidence) from one path to exactly this
// set; the Dependabot uses:-only lane and the stale-evidence full-tree carry
// stay closed to it. It pins the ENTIRE transformation: the set of changed
// workflow paths must EQUAL this key set and every path's base and target
// blob must match the pinned pair (after-blobs = the seven files as reviewed
// on the draft pull request of branch ci/trust-boundary; reproduce with
// `git show <commit>:<path> | git hash-object --stdin`). Round 2 must make
// FOUR coordinated edits: (1) apply the seven workflow files byte-exact to
// the `after` blobs, (2) extend verify-cloud-action-pins.mjs and
// verify-cloud-workflow-gate.sh to cover them, (3) remove this block and the
// isOneTimeTrustBoundaryRewrite check below (byte-exact restore of this file),
// and (4) delete tests/onboarding/trust-boundary-handoff-behavior.ts plus its
// side-effect import in cloud-release-gate-behavior.ts.
const oneTimeTrustBoundaryRewrite = new Map([
  [".github/workflows/ci.yml", { before: "28f901994ed08c1c769fe68f64b31d8c9df5e598", after: "dc4f21a91eee6e868fc73f1700f087b50af091bb" }],
  [".github/workflows/codeql.yml", { before: "66135722a4ccd1f044b75a1ee9573e84f8824020", after: "96845f88a35a1350e94375a318990f23b56d2e87" }],
  [".github/workflows/dependency-review.yml", { before: "896a0e56118f53f9c070e5c5070bdffcf05a96dc", after: "6467043e5ab1d440ec62b3521aa8b368d73f48ff" }],
  [".github/workflows/pairing-e2e.yml", { before: "90960cfde9f4d6d351bb63d89072b932b0752d3d", after: "f5b921225bb31a128924ecbd0191acbd651436b7" }],
  [".github/workflows/pr-review-watchdog.yml", { before: "25f04bc82263e640dd56b32d29db1dd07659dd1a", after: "7e38897643ab3c0c4698f5cf0a46f7b97abbb4fb" }],
  [".github/workflows/relay-image.yml", { before: "c9796922081f20da986db990b2278f8fc86767d5", after: "3e492bd838175d4e51023cbb56c2e0d2f235f249" }],
  [".github/workflows/release-pipeline-audit.yml", { before: "526553bb1208bbe3147bac5a3a1addf2bb755ad2", after: "586072ee241d4dda84e04a4cef707440881d22a0" }],
]);

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
if (
  mode !== "full-tree" &&
  mode !== "workflow-tree-only" &&
  mode !== "single-sensitive-workflow"
) {
  fail(
    "mode must be full-tree, workflow-tree-only, or single-sensitive-workflow",
  );
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
const changedWorkflowPaths = [...base]
  .filter(([path, entry]) => target.get(path).blob !== entry.blob)
  .map(([path]) => path);
const isOneTimeTrustBoundaryRewrite =
  mode === "single-sensitive-workflow" &&
  changedWorkflowPaths.length === oneTimeTrustBoundaryRewrite.size &&
  changedWorkflowPaths.every((path) => {
    const pin = oneTimeTrustBoundaryRewrite.get(path);
    return (
      pin !== undefined &&
      base.get(path).blob === pin.before &&
      target.get(path).blob === pin.after
    );
  });
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
  if (isOneTimeTrustBoundaryRewrite) {
    continue; // exact reviewed blobs; set equality is enforced above
  }
  if (mode === "single-sensitive-workflow") {
    if (!sensitive.has(path) || changed > 1) {
      fail("approval may change exactly one existing sensitive workflow");
    }
    continue;
  }
  if (sensitive.has(path)) {
    fail(`${path} is Cloud-release-sensitive`);
  }
  verifyUsesOnly(path, baseCommit, targetCommit);
}
if (changed === 0) {
  fail("workflow tree changed without a workflow file delta");
}

console.log(
  isOneTimeTrustBoundaryRewrite
    ? "[verify-cloud-workflow-uses-only] OK: the one-time approved trust-boundary rewrite"
    : mode === "single-sensitive-workflow"
    ? "[verify-cloud-workflow-uses-only] OK: one approved sensitive workflow content change"
    : `[verify-cloud-workflow-uses-only] OK: ${changed} non-sensitive workflow file(s)`,
);
