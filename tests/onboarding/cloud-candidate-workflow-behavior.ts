import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { describe, expect, it } from "vitest";

type FixtureChange =
  | "draft"
  | "wrong-actor"
  | "wrong-trigger"
  | "rerun"
  | "stale-base"
  | "wrong-head-repo"
  | "wrong-parent"
  | "failed-check"
  | "older-failed-check"
  | "spoofed-check"
  | "same-app-spoofed-check"
  | "wrong-check-event"
  | "wrong-workflow-pr"
  | "later-pending-workflow"
  | "large-workflow-history"
  | "stale-check-base"
  | "wrong-cloud-target"
  | "later-pending-check"
  | "failed-commit-status"
  | "successful-commit-status"
  | "passing-conclusions"
  | "stale-codex"
  | "spoofed-codex"
  | "later-codex-finding"
  | "later-codex-reaction"
  | "later-codex-bot-reaction"
  | "unresolved-thread"
  | "changes-requested"
  | "moved-late"
  | "moved-during-final-checks"
  | "source-rejected"
  | "identity-mismatch"
  | "cloud-success";

function git(cwd: string, args: string[]): string {
  return execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
}

function writeExecutable(path: string, body: string): void {
  writeFileSync(path, body, "utf8");
  chmodSync(path, 0o755);
}

function runResolver(
  change?: FixtureChange,
  pullRequest = "42",
): ReturnType<typeof spawnSync> {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-cloud-candidate-"));
  const remote = mkdtempSync(join(tmpdir(), "ha-nova-cloud-candidate-remote-"));
  const fakeBin = join(root, "fake-bin");
  mkdirSync(fakeBin);
  mkdirSync(join(root, "scripts", "release"), { recursive: true });

  git(root, ["init", "-q", "-b", "main"]);
  git(root, ["config", "user.name", "Candidate Test"]);
  git(root, ["config", "user.email", "candidate@example.invalid"]);
  writeFileSync(join(root, "source.txt"), "base\n");
  git(root, ["add", "source.txt"]);
  git(root, ["commit", "-qm", "base"]);
  const base = git(root, ["rev-parse", "HEAD"]);
  git(root, ["switch", "-qc", "candidate"]);
  writeFileSync(join(root, "source.txt"), "candidate\n");
  git(root, ["commit", "-qam", "candidate"]);
  const head = git(root, ["rev-parse", "HEAD"]);
  git(root, ["switch", "-q", "main"]);
  git(root, ["switch", "-qc", "merge-source"]);
  git(root, ["merge", "--no-ff", "-qm", "merge", "candidate"]);
  const merge = git(root, ["rev-parse", "HEAD"]);
  const tree = git(root, ["rev-parse", "HEAD^{tree}"]);
  git(root, ["switch", "-q", "main"]);
  git(remote, ["init", "--bare", "-q"]);
  git(root, ["remote", "add", "origin", remote]);
  git(root, ["push", "-q", "origin", `${base}:refs/heads/main`]);
  git(root, ["push", "-q", "origin", `${merge}:refs/pull/42/merge`]);

  const policy = {
    main_branch_protection: {
      required_status_checks: ["analyze", "ci-gate", "cloud-source-gate"],
      required_status_check_apps: { "cloud-source-gate": 4400145 },
      advisory_checks: ["codex-review-gate"],
    },
  };
  const policyPath = join(root, "repo-policy.json");
  writeFileSync(policyPath, JSON.stringify(policy));

  const pr = {
    number: 42,
    state: "open",
    draft: change === "draft",
    base: {
      ref: "main",
      sha: base,
      repo: { full_name: "markusleben/ha-nova" },
    },
    head: {
      sha: head,
      repo: {
        full_name:
          change === "wrong-head-repo" ? "someone/fork" : "markusleben/ha-nova",
      },
    },
    merge_commit_sha: merge,
  };
  const commit = {
    sha: merge,
    tree: { sha: tree },
    parents: [{ sha: change === "wrong-parent" ? head : base }, { sha: head }],
  };
  const checkBinding = {
    head_sha: head,
    pull_requests: [
      {
        number: 42,
        base: {
          sha: change === "stale-check-base" ? "f".repeat(40) : base,
        },
        head: { sha: head },
      },
    ],
  };
  const workflowBinding = {
    ...checkBinding,
    pull_requests: checkBinding.pull_requests.map((pullRequest) => ({
      ...pullRequest,
      number: change === "wrong-workflow-pr" ? 43 : pullRequest.number,
    })),
  };
  const checks = [
    ...(change === "older-failed-check"
      ? [
          {
            id: 0,
            name: "cloud-source-gate",
            status: "completed",
            conclusion: "success",
            app: { id: 4400145 },
            details_url:
              "https://github.com/markusleben/ha-nova/actions/runs/0/job/0",
            external_id: `workflow-run:1:attempt:1:target:${merge}`,
            ...checkBinding,
          },
          {
            id: 1,
            name: "ci-gate",
            status: "completed",
            conclusion: "failure",
            app: { id: 15368 },
            details_url:
              "https://github.com/markusleben/ha-nova/actions/runs/1/job/1",
            ...checkBinding,
          },
        ]
      : []),
    {
      id: 2,
      name: "analyze",
      status: "completed",
      conclusion: change === "passing-conclusions" ? "neutral" : "success",
      app: { id: 15368 },
      details_url:
        "https://github.com/markusleben/ha-nova/actions/runs/2/job/2",
      ...checkBinding,
    },
    {
      id: 3,
      name: "ci-gate",
      status: "completed",
      conclusion:
        change === "failed-check" || change === "spoofed-check"
          ? "failure"
          : change === "passing-conclusions"
            ? "skipped"
            : "success",
      app: { id: 15368 },
      details_url:
        "https://github.com/markusleben/ha-nova/actions/runs/3/job/3",
      ...checkBinding,
    },
    {
      id: 4,
      name: "codex-review-gate",
      status: "completed",
      conclusion: "success",
      app: { id: 15368 },
      details_url:
        "https://github.com/markusleben/ha-nova/actions/runs/4/job/4",
      ...checkBinding,
    },
    {
      id: 5,
      name: "cloud-source-gate",
      status: "completed",
      conclusion: change === "cloud-success" ? "success" : "failure",
      app: { id: 4400145 },
      details_url:
        "https://github.com/markusleben/ha-nova/actions/runs/5/job/5",
      external_id: `workflow-run:1:attempt:1:target:${
        change === "wrong-cloud-target" ? "e".repeat(40) : merge
      }`,
      ...checkBinding,
    },
    ...(change === "spoofed-check"
      ? [
          {
            id: 99,
            name: "ci-gate",
            status: "completed",
            conclusion: "success",
            app: { id: 999 },
            details_url:
              "https://github.com/markusleben/ha-nova/actions/runs/99/job/99",
            ...checkBinding,
          },
        ]
      : []),
    ...(change === "same-app-spoofed-check"
      ? [
          {
            id: 99,
            name: "ci-gate",
            status: "completed",
            conclusion: "success",
            app: { id: 15368 },
            details_url:
              "https://github.com/markusleben/ha-nova/actions/runs/99/job/99",
            ...checkBinding,
          },
        ]
      : []),
  ];
  const workflowRuns = [
    ...(change === "large-workflow-history"
      ? [
          {
            id: 0,
            path: ".github/workflows/history.yml",
            padding: "x".repeat(3 * 1024 * 1024),
          },
        ]
      : []),
    {
      id: 1,
      path: ".github/workflows/ci.yml",
      event: "pull_request",
      status: "completed",
      conclusion: "failure",
      ...workflowBinding,
    },
    {
      id: 2,
      path: ".github/workflows/codeql.yml",
      event: "pull_request",
      status: "completed",
      conclusion: "success",
      ...workflowBinding,
    },
    {
      id: 3,
      path: ".github/workflows/ci.yml",
      event: change === "wrong-check-event" ? "push" : "pull_request",
      status: "completed",
      conclusion: "success",
      ...workflowBinding,
    },
    {
      id: 4,
      path: ".github/workflows/codex-review-gate.yml",
      event: "pull_request",
      status: "completed",
      conclusion: "success",
      ...workflowBinding,
    },
    ...(change === "same-app-spoofed-check"
      ? [
          {
            id: 99,
            path: ".github/workflows/spoof.yml",
            event: "pull_request",
            status: "completed",
            conclusion: "success",
            ...workflowBinding,
          },
        ]
      : []),
    ...(change === "later-pending-workflow"
      ? [
          {
            id: 100,
            path: ".github/workflows/ci.yml",
            event: "pull_request",
            status: "in_progress",
            conclusion: null,
            ...workflowBinding,
          },
        ]
      : []),
  ];
  const prPath = join(root, "pr.json");
  const commitPath = join(root, "commit.json");
  const checksPath = join(root, "checks.json");
  const laterChecksPath = join(root, "later-checks.json");
  const statusesPath = join(root, "statuses.json");
  const workflowRunsPath = join(root, "workflow-runs.json");
  const commentsPath = join(root, "comments.json");
  const reviewsPath = join(root, "reviews.json");
  const inlineCommentsPath = join(root, "inline-comments.json");
  const reactionsPath = join(root, "reactions.json");
  const threadsPath = join(root, "threads.json");
  const prCallsPath = join(root, "pr-calls");
  const checkCallsPath = join(root, "check-calls");
  writeFileSync(prPath, JSON.stringify(pr));
  writeFileSync(commitPath, JSON.stringify(commit));
  writeFileSync(checksPath, JSON.stringify([{ check_runs: checks }]));
  writeFileSync(
    laterChecksPath,
    JSON.stringify([
      {
        check_runs:
          change === "later-pending-check"
            ? [
                ...checks,
                {
                  id: 100,
                  name: "ci-gate",
                  status: "in_progress",
                  conclusion: null,
                  app: { id: 15368 },
                  ...checkBinding,
                },
              ]
            : checks,
      },
    ]),
  );
  writeFileSync(
    statusesPath,
    JSON.stringify([
      {
        statuses:
          change === "failed-commit-status" ||
          change === "successful-commit-status"
            ? [
                {
                  id: 1,
                  context: "ci-gate",
                  state:
                    change === "successful-commit-status"
                      ? "success"
                      : "failure",
                },
              ]
            : [],
      },
    ]),
  );
  writeFileSync(
    workflowRunsPath,
    JSON.stringify([{ workflow_runs: workflowRuns }]),
  );
  writeFileSync(
    commentsPath,
    JSON.stringify([
      [
        {
          user: {
            login:
              change === "spoofed-codex"
                ? "chatgpt-codex-connector-review"
                : "chatgpt-codex-connector[bot]",
            id: change === "spoofed-codex" ? 999 : 199175422,
            type: change === "spoofed-codex" ? "User" : "Bot",
          },
          created_at: "2026-07-29T10:00:00Z",
          body:
            "Codex Review: Didn't find any major issues. Keep it up!\n\n" +
            `**Reviewed commit:** \`${change === "stale-codex" ? "0000000000" : head.slice(0, 10)}\``,
        },
      ],
    ]),
  );
  writeFileSync(reviewsPath, JSON.stringify([[]]));
  writeFileSync(
    inlineCommentsPath,
    JSON.stringify([
      change === "later-codex-finding"
        ? [
            {
              user: {
                login: "chatgpt-codex-connector[bot]",
                id: 199175422,
                type: "Bot",
              },
              commit_id: head,
              created_at: "2026-07-29T10:01:00Z",
              body: "A later finding",
            },
          ]
        : [],
    ]),
  );
  writeFileSync(
    reactionsPath,
    JSON.stringify([
      change === "later-codex-reaction" ||
      change === "later-codex-bot-reaction"
        ? [
            {
              user: {
                login: "chatgpt-codex-connector[bot]",
                id: 199175422,
                type:
                  change === "later-codex-bot-reaction" ? "Bot" : "User",
              },
              created_at: "2026-07-29T10:01:00Z",
              content: "eyes",
            },
          ]
        : [],
    ]),
  );
  writeFileSync(
    threadsPath,
    JSON.stringify([
      {
        data: {
          repository: {
            pullRequest: {
              reviewDecision:
                change === "changes-requested"
                  ? "CHANGES_REQUESTED"
                  : "REVIEW_REQUIRED",
              reviewThreads: {
                nodes: [
                  { isResolved: change !== "unresolved-thread" },
                ],
                pageInfo: { hasNextPage: false, endCursor: null },
              },
            },
          },
        },
      },
    ]),
  );
  writeFileSync(prCallsPath, "0\n");
  writeFileSync(checkCallsPath, "0\n");
  writeExecutable(
    join(root, "scripts", "release", "verify-cloud-target-source-gate.sh"),
    `#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "candidate" ]]
[[ "$HA_NOVA_CLOUD_GATE_SOURCE_REF" == "refs/pull/42/merge" ]]
[[ "$HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT" == "$FAKE_MERGE" ]]
[[ "$HA_NOVA_CLOUD_GATE_EXPECTED_HEAD_COMMIT" == "$FAKE_HEAD" ]]
[[ "$HA_NOVA_CLOUD_GATE_EXPECTED_BASE_COMMIT" == "$FAKE_BASE" ]]
[[ "$FAKE_CHANGE" != "source-rejected" ]]
`,
  );
  writeExecutable(
    join(fakeBin, "gh"),
    `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "api repos/markusleben/ha-nova/pulls/42")
    calls="$(( $(<"$FAKE_PR_CALLS") + 1 ))"
    printf '%s\n' "$calls" >"$FAKE_PR_CALLS"
    if [[ "$FAKE_CHANGE" == "moved-late" && "$calls" -gt 1 ]] || \
       [[ "$FAKE_CHANGE" == "moved-during-final-checks" && $(<"$FAKE_CHECK_CALLS") -gt 1 ]]; then
      sed 's/"state":"open"/"state":"closed"/' "$FAKE_PR"
    else
      cat "$FAKE_PR"
    fi
    ;;
  "api repos/markusleben/ha-nova/git/commits/$FAKE_MERGE") cat "$FAKE_COMMIT" ;;
  "api --paginate --slurp repos/markusleben/ha-nova/commits/$FAKE_HEAD/check-runs?filter=latest&per_page=100")
    calls="$(( $(<"$FAKE_CHECK_CALLS") + 1 ))"
    printf '%s\n' "$calls" >"$FAKE_CHECK_CALLS"
    if [[ "$FAKE_CHANGE" == "later-pending-check" && "$calls" -gt 1 ]]; then
      cat "$FAKE_LATER_CHECKS"
    else
      cat "$FAKE_CHECKS"
    fi
    ;;
  "api --paginate --slurp repos/markusleben/ha-nova/commits/$FAKE_HEAD/status?per_page=100") cat "$FAKE_STATUSES" ;;
  "api --paginate --slurp repos/markusleben/ha-nova/actions/runs?head_sha=$FAKE_HEAD&per_page=100") cat "$FAKE_WORKFLOW_RUNS" ;;
  "api --paginate --slurp repos/markusleben/ha-nova/issues/42/comments?per_page=100") cat "$FAKE_COMMENTS" ;;
  "api --paginate --slurp repos/markusleben/ha-nova/pulls/42/reviews?per_page=100") cat "$FAKE_REVIEWS" ;;
  "api --paginate --slurp repos/markusleben/ha-nova/pulls/42/comments?per_page=100") cat "$FAKE_INLINE_COMMENTS" ;;
  "api --paginate --slurp repos/markusleben/ha-nova/issues/42/reactions?per_page=100") cat "$FAKE_REACTIONS" ;;
  api\\ graphql*) cat "$FAKE_THREADS" ;;
  *) exit 2 ;;
esac
`,
  );
  const output = join(root, "github-output");
  writeFileSync(output, "");
  return spawnSync(
    "bash",
    [
      resolve("scripts/release/resolve-cloud-candidate-source.sh"),
      pullRequest,
      policyPath,
    ],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${fakeBin}:${process.env.PATH ?? ""}`,
        FAKE_PR: prPath,
        FAKE_COMMIT: commitPath,
        FAKE_CHECKS: checksPath,
        FAKE_LATER_CHECKS: laterChecksPath,
        FAKE_STATUSES: statusesPath,
        FAKE_WORKFLOW_RUNS: workflowRunsPath,
        FAKE_COMMENTS: commentsPath,
        FAKE_REVIEWS: reviewsPath,
        FAKE_INLINE_COMMENTS: inlineCommentsPath,
        FAKE_REACTIONS: reactionsPath,
        FAKE_THREADS: threadsPath,
        FAKE_PR_CALLS: prCallsPath,
        FAKE_CHECK_CALLS: checkCallsPath,
        FAKE_CHANGE: change ?? "valid",
        FAKE_BASE: base,
        FAKE_HEAD: head,
        FAKE_MERGE: merge,
        GITHUB_EVENT_NAME: "workflow_dispatch",
        GITHUB_REF: "refs/heads/main",
        GITHUB_REPOSITORY: "markusleben/ha-nova",
        GITHUB_ACTOR: change === "wrong-actor" ? "other-user" : "markusleben",
        GITHUB_ACTOR_ID: change === "wrong-actor" ? "999" : "6522814",
        GITHUB_TRIGGERING_ACTOR:
          change === "wrong-trigger" ? "other-user" : "markusleben",
        GITHUB_RUN_ATTEMPT: change === "rerun" ? "2" : "1",
        GITHUB_SHA: change === "stale-base" ? head : base,
        GITHUB_OUTPUT: output,
        ...(change === "identity-mismatch"
          ? {
              HA_NOVA_CLOUD_CANDIDATE_EXPECTED_COMMIT:
                "0".repeat(40),
              HA_NOVA_CLOUD_CANDIDATE_EXPECTED_TREE: tree,
              HA_NOVA_CLOUD_CANDIDATE_EXPECTED_BASE: base,
              HA_NOVA_CLOUD_CANDIDATE_EXPECTED_HEAD: head,
            }
          : {}),
      },
    },
  );
}

describe("Cloud candidate source resolver", () => {
  it("accepts one current reviewed merge source with only the evidence gate failed", () => {
    const result = runResolver();
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(result.stdout).toContain("OK: PR #42");
  });

  it("uses the latest protected and evidence check runs", () => {
    const result = runResolver("older-failed-check");
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it("accepts GitHub's passing check conclusions", () => {
    const result = runResolver("passing-conclusions");
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it("accepts workflow history larger than the process argument limit", () => {
    const result = runResolver("large-workflow-history");
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it.each([
    ["a draft", "draft"],
    ["a non-maintainer dispatcher", "wrong-actor"],
    ["a non-maintainer rerun trigger", "wrong-trigger"],
    ["a rerun attempt", "rerun"],
    ["a stale base", "stale-base"],
    ["a fork", "wrong-head-repo"],
    ["wrong merge parents", "wrong-parent"],
    ["another failed check", "failed-check"],
    ["a same-name check from another App", "spoofed-check"],
    ["a same-name check from another workflow", "same-app-spoofed-check"],
    ["a check backed by the wrong workflow event", "wrong-check-event"],
    ["a check backed by another pull request", "wrong-workflow-pr"],
    ["an older check behind a newer pending workflow", "later-pending-workflow"],
    ["checks from a stale pull-request base", "stale-check-base"],
    ["a Cloud check for another merge target", "wrong-cloud-target"],
    ["a later pending check", "later-pending-check"],
    ["a failed same-name commit status", "failed-commit-status"],
    ["a successful same-name commit status", "successful-commit-status"],
    ["a stale Codex result", "stale-codex"],
    ["a prefixed Codex impersonator", "spoofed-codex"],
    ["a later Codex finding", "later-codex-finding"],
    ["a later Codex reaction", "later-codex-reaction"],
    ["a later Codex bot reaction", "later-codex-bot-reaction"],
    ["an unresolved review thread", "unresolved-thread"],
    ["requested changes", "changes-requested"],
    ["a pull request that moves during resolution", "moved-late"],
    ["a pull request that moves during final check validation", "moved-during-final-checks"],
    ["a source rejected by the trusted bootstrap verifier", "source-rejected"],
    ["a changed expected identity", "identity-mismatch"],
    ["an already-successful evidence gate", "cloud-success"],
  ] as const)("rejects %s", (_label, change) => {
    const result = runResolver(change);
    expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
  });

  it("rejects a shell payload passed as the pull request input", () => {
    const result = runResolver(
      undefined,
      '42"; printf "commit_sha=0000000000000000000000000000000000000000\\n" >>"$GITHUB_OUTPUT"; #',
    );
    expect(result.status).not.toBe(0);
    expect(result.stdout).not.toContain("OK: PR #42");
  });
});

describe("Cloud candidate workflow contract", () => {
  const workflowPath = ".github/workflows/cloud-candidate-bundle.yml";
  const resolverPath = "scripts/release/resolve-cloud-candidate-source.sh";
  const verifierPath = "scripts/release/verify-cloud-candidate-workflow.mjs";
  const workflow = readFileSync(workflowPath, "utf8");

  it("passes the trusted non-publishing workflow verifier", () => {
    const result = spawnSync(
      "node",
      [verifierPath, workflowPath, resolverPath],
      { encoding: "utf8" },
    );
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it("rejects publication commands", () => {
    const root = mkdtempSync(
      join(tmpdir(), "ha-nova-cloud-candidate-contract-"),
    );
    const unsafeWorkflow = join(root, "cloud-candidate-bundle.yml");
    writeFileSync(unsafeWorkflow, `${workflow}\n# gh release create\n`);
    const result = spawnSync(
      "node",
      [verifierPath, unsafeWorkflow, resolverPath],
      { encoding: "utf8" },
    );
    expect(result.status).not.toBe(0);
  });

  it("rejects raw dispatch-input interpolation in a run block", () => {
    const root = mkdtempSync(
      join(tmpdir(), "ha-nova-cloud-candidate-contract-"),
    );
    const unsafeWorkflow = join(root, "cloud-candidate-bundle.yml");
    writeFileSync(
      unsafeWorkflow,
      workflow.replace(
        '"${PR_NUMBER}"',
        '"${{ inputs.pull_request }}; echo injected"',
      ),
    );
    const result = spawnSync(
      "node",
      [verifierPath, unsafeWorkflow, resolverPath],
      { encoding: "utf8" },
    );
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "workflow_dispatch inputs must reach run scripts only through env",
    );
  });

  it("uses trusted scripts with an explicit immutable source root", () => {
    for (const path of [
      "scripts/release/build-rc-binaries.sh",
      "scripts/release/build-sign-darwin-binaries.sh",
      "scripts/release/build-install-bundle.sh",
      "scripts/release/verify-release-metadata.sh",
    ]) {
      expect(readFileSync(path, "utf8"), path).toContain(
        "HA_NOVA_SOURCE_ROOT:-${TRUSTED_ROOT}",
      );
    }
    expect(workflow).not.toMatch(/\b(?:bash|node)\s+target\//);
    expect(workflow).not.toContain("HA_NOVA_CLOUD_GATE_EVIDENCE_JSON");
    expect(workflow).not.toMatch(/\bgh\s+release\b|\bgit\s+tag\b/);
    expect(readFileSync("docs/releasing.md", "utf8")).toContain(
      "gh workflow run cloud-candidate-bundle.yml --ref main",
    );
    expect(readFileSync(".github/CODEOWNERS", "utf8")).toContain(
      "/.github/workflows/cloud-candidate-bundle.yml @markusleben",
    );
  });

  it("rejects Cloud evidence that lacks a valid release signature", () => {
    const root = mkdtempSync(join(tmpdir(), "ha-nova-cloud-evidence-"));
    const binary = join(root, "ha-nova");
    const bundle = join(root, "bundle.json");
    writeFileSync(binary, "candidate");
    writeFileSync(
      bundle,
      JSON.stringify({
        bundle_format_version: 1,
        version: "0.22.0-rc1",
        os: "linux",
        arch: "amd64",
        binary_name: "ha-nova",
        cloud_release: {
          schema: 1,
          source_tree_sha: "1".repeat(40),
          binary_sha256:
            "dda18a0e21ae47c53b4309434cbc02ae8bf764fa83a6defbb719431242722aa7",
          signature: Buffer.alloc(64).toString("base64"),
        },
      }),
    );
    const result = spawnSync(
      "node",
      [
        "scripts/release/verify-cloud-release-evidence.mjs",
        bundle,
        binary,
        "0.22.0-rc1",
        "linux",
        "amd64",
        "ha-nova",
        "1".repeat(40),
        "darwin,linux,windows",
      ],
      { encoding: "utf8" },
    );
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("signature is invalid");
  });
});
