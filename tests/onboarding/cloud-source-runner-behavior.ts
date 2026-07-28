import { spawnSync } from "node:child_process";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { CloudSourceRunOptions } from "./cloud-source-runner-options.js";

export const headSHA = "a".repeat(40);
const baseSHA = "b".repeat(40);
export const mergeSHA = "d".repeat(40);

export function runSourceGate(options: CloudSourceRunOptions = {}) {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-source-runner-"));
  const bin = join(root, "bin");
  const eventPath = join(root, "event.json");
  const gitIndexPath = join(root, "git-index");
  const gitSequencePath = join(root, "git-sequence");
  const preloadPath = join(root, "mock-fetch.mjs");
  const tracePath = join(root, "trace.jsonl");
  mkdirSync(bin);

  const action = options.action ?? "completed";
  const event = options.event ?? "pull_request";
  const workflowRun = {
    conclusion: options.conclusion ?? "success",
    event,
    head_branch:
      event === "merge_group"
        ? "gh-readonly-queue/main/pr-123-deadbeef"
        : "feature",
    head_sha: headSHA,
    id: 123,
    name: "CI",
    run_attempt: 1,
    status: action === "completed" ? "completed" : "in_progress",
    workflow_id: 77,
  };
  const pull = {
    base: {
      ref: "main",
      repo: { full_name: "markusleben/ha-nova" },
      sha: baseSHA,
    },
    head: { sha: headSHA },
    merge_commit_sha:
      options.mergeCommitSHA === undefined ? mergeSHA : options.mergeCommitSHA,
    number: 449,
    state: "open",
  };
  const pulls = Array.from({ length: options.pullCount ?? 1 }, (_, index) => ({
    ...pull,
    merge_commit_sha:
      options.associationMergeCommitSHA === undefined
        ? pull.merge_commit_sha
        : options.associationMergeCommitSHA,
    number: pull.number + index,
  }));

  writeFileSync(
    eventPath,
    JSON.stringify({ action, workflow_run: workflowRun }),
    "utf8",
  );
  writeFileSync(tracePath, "", "utf8");
  writeFileSync(
    join(bin, "git"),
    `#!/bin/sh
printf '{"method":"GIT","path":"%s","body":null}\\n' "$4" >> "$MOCK_TRACE"
index="$(cat "$MOCK_GIT_INDEX_FILE")"
index="$((index + 1))"
printf '%s' "$index" > "$MOCK_GIT_INDEX_FILE"
count="$(wc -l < "$MOCK_GIT_SEQUENCE_FILE" | tr -d ' ')"
if [ "$index" -gt "$count" ]; then
  index="$count"
fi
mock_sha="$(sed -n "\${index}p" "$MOCK_GIT_SEQUENCE_FILE")"
if [ -n "$mock_sha" ]; then
  printf '%s\\t%s\\n' "$mock_sha" "$4"
fi
`,
    "utf8",
  );
  writeFileSync(
    join(bin, "bash"),
    `#!/bin/sh
exit "$MOCK_BASH_EXIT"
`,
    "utf8",
  );
  chmodSync(join(bin, "git"), 0o755);
  chmodSync(join(bin, "bash"), 0o755);
  const defaultGitSHA =
    options.gitSHA === undefined
      ? event === "pull_request"
        ? (pull.merge_commit_sha ?? "")
        : headSHA
      : (options.gitSHA ?? "");
  writeFileSync(gitIndexPath, "0", "utf8");
  writeFileSync(
    gitSequencePath,
    `${(options.gitSHASequence ?? [defaultGitSHA])
      .map((sha) => sha ?? "")
      .join("\n")}\n`,
    "utf8",
  );

  writeFileSync(
    preloadPath,
    `import { appendFileSync } from "node:fs";
let checks = JSON.parse(process.env.MOCK_CHECKS);
let hiddenChecks = [];
let postVisibilityScansRemaining = Number(
  process.env.MOCK_POST_VISIBILITY_DELAY,
);
let lateVisibleChecks = JSON.parse(process.env.MOCK_LATE_VISIBLE_CHECKS);
let lateVisibleScansRemaining = Number(
  process.env.MOCK_LATE_VISIBLE_CHECKS_DELAY,
);
const monotonicNowSequence = JSON.parse(
  process.env.MOCK_MONOTONIC_NOW_SEQUENCE,
);
let monotonicNowIndex = 0;
let monotonicNow = 0;
Object.defineProperty(globalThis.performance, "now", {
  value: () => {
    if (monotonicNowSequence.length === 0) {
      return monotonicNow;
    }
    return monotonicNowSequence[
      Math.min(monotonicNowIndex++, monotonicNowSequence.length - 1)
    ];
  },
});
const pulls = JSON.parse(process.env.MOCK_PULLS);
const fullPulls = JSON.parse(process.env.MOCK_FULL_PULLS);
const mergeCommitResponses = JSON.parse(
  process.env.MOCK_MERGE_COMMIT_RESPONSES,
);
let mergeCommitResponseIndex = 0;
let fullPullIndex = 0;
let terminalPatchCompleted = false;
let postAttempted = false;
const postReconcileListStatuses = JSON.parse(
  process.env.MOCK_POST_RECONCILE_LIST_STATUSES,
);
let postReconcileListStatusIndex = 0;
const deleteStatuses = JSON.parse(process.env.MOCK_DELETE_STATUSES);
let deleteStatusIndex = 0;
const associationPresent = JSON.parse(process.env.MOCK_ASSOCIATION_PRESENT);
let associationIndex = 0;
const workflowRuns = JSON.parse(process.env.MOCK_WORKFLOW_RUNS);
let workflowRunIndex = 0;
const workflowAPIStatuses = JSON.parse(
  process.env.MOCK_WORKFLOW_API_STATUSES,
);
let workflowAPIStatusIndex = 0;
globalThis.setTimeout = (callback, delay = 0) => {
  monotonicNow += delay;
  appendFileSync(
    process.env.MOCK_TRACE,
    JSON.stringify({ method: "TIMER", path: String(delay), body: null }) + "\\n",
  );
  callback();
  return 0;
};
function response(data, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => data };
}
globalThis.fetch = async (url, init = {}) => {
  const method = init.method ?? "GET";
  const parsed = new URL(url);
  const path = parsed.pathname;
  const body = init.body === undefined ? null : JSON.parse(init.body);
  appendFileSync(process.env.MOCK_TRACE, JSON.stringify({ method, path, body }) + "\\n");
  if (path.endsWith("/actions/workflows/ci.yml")) {
    return response({ id: 77, path: ".github/workflows/ci.yml" });
  }
  if (path.endsWith("/actions/runs/123")) {
    return response(
      workflowRuns[Math.min(workflowRunIndex++, workflowRuns.length - 1)],
      workflowAPIStatuses[
        Math.min(workflowAPIStatusIndex++, workflowAPIStatuses.length - 1)
      ],
    );
  }
  if (path.endsWith("/commits/${headSHA}/pulls")) {
    const present =
      associationPresent[
        Math.min(associationIndex++, associationPresent.length - 1)
      ];
    return response(present ? pulls : []);
  }
  if (path.endsWith("/pulls/449")) {
    return response(fullPulls[Math.min(fullPullIndex++, fullPulls.length - 1)]);
  }
  if (path.includes("/git/commits/")) {
    const mock =
      mergeCommitResponses[
        Math.min(
          mergeCommitResponseIndex++,
          mergeCommitResponses.length - 1,
        )
      ];
    return response({
      parents: mock.parents.map((sha) => ({ sha })),
      sha: mock.sha ?? path.split("/").at(-1),
    }, mock.status ?? 200);
  }
  if (path.endsWith("/check-runs") && method === "GET") {
    if (hiddenChecks.length > 0) {
      if (postVisibilityScansRemaining === 0) {
        checks.push(...hiddenChecks);
        hiddenChecks = [];
      } else {
        postVisibilityScansRemaining -= 1;
      }
    }
    if (lateVisibleChecks.length > 0) {
      if (lateVisibleScansRemaining === 0) {
        checks.push(...lateVisibleChecks);
        lateVisibleChecks = [];
      } else {
        lateVisibleScansRemaining -= 1;
      }
    }
    const postReconcileStatus =
      postAttempted && postReconcileListStatuses.length > 0
        ? postReconcileListStatuses[
            Math.min(
              postReconcileListStatusIndex++,
              postReconcileListStatuses.length - 1,
            )
          ]
        : undefined;
    return response(
      { check_runs: checks, total_count: checks.length },
      postReconcileStatus ??
        (terminalPatchCompleted
          ? Number(process.env.MOCK_CHECK_LIST_STATUS_AFTER_PATCH)
          : 200),
    );
  }
  if (path.endsWith("/check-runs") && method === "POST") {
    postAttempted = true;
    if (process.env.MOCK_POST_THROWS_BEFORE_APPLY === "true") {
      throw new DOMException("mock response timeout", "TimeoutError");
    }
    const created = {
      ...body,
      app: { id: 42 },
      id: 900,
    };
    if (process.env.MOCK_POST_THROWS_AFTER_APPLY === "true") {
      if (postVisibilityScansRemaining > 0) {
        hiddenChecks.push(created);
      } else {
        checks.push(created);
      }
      throw new DOMException("mock response timeout", "TimeoutError");
    }
    checks.push(created);
    return response(created, 201);
  }
  if (path.includes("/check-runs/") && method === "GET") {
    const id = Number(path.split("/").at(-1));
    const found = checks.find((candidate) => candidate.id === id);
    return response(
      found ?? {},
      found === undefined ? 404 : Number(process.env.MOCK_CHECK_READ_STATUS),
    );
  }
  if (path.includes("/check-runs/") && method === "PATCH") {
    const id = Number(path.split("/").at(-1));
    const patchApplied =
      Number(process.env.MOCK_PATCH_STATUS) === 200 ||
      process.env.MOCK_PATCH_THROWS_AFTER_APPLY === "true";
    if (patchApplied) {
      checks = checks.map((candidate) =>
        candidate.id === id ? { ...candidate, ...body } : candidate
      );
    }
    terminalPatchCompleted = patchApplied;
    if (process.env.MOCK_PATCH_THROWS_AFTER_APPLY === "true") {
      throw new DOMException("mock response timeout", "TimeoutError");
    }
    return response(
      checks.find((candidate) => candidate.id === id),
      Number(process.env.MOCK_PATCH_STATUS),
    );
  }
  if (path.includes("/check-runs/") && method === "DELETE") {
    const id = Number(path.split("/").at(-1));
    const deleteStatus =
      deleteStatuses[Math.min(deleteStatusIndex++, deleteStatuses.length - 1)];
    if (deleteStatus === 204) {
      checks = checks.filter((candidate) => candidate.id !== id);
    }
    return response({}, deleteStatus);
  }
  return response({ message: "unexpected request" }, 500);
};
`,
    "utf8",
  );

  const result = spawnSync(
    process.execPath,
    ["--import", preloadPath, "scripts/release/run-cloud-source-check.mjs"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        GH_TOKEN: "github-token-at-least-twenty-characters",
        GITHUB_EVENT_PATH: eventPath,
        GITHUB_REPOSITORY: "markusleben/ha-nova",
        GITHUB_RUN_ID: "999",
        GITHUB_WORKSPACE: process.cwd(),
        HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID: "42",
        HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN:
          "dedicated-token-at-least-twenty-characters",
        MOCK_ASSOCIATION_PRESENT: JSON.stringify(
          options.associationPresentSequence ?? [true],
        ),
        MOCK_BASH_EXIT: String(options.bashExit ?? 0),
        MOCK_CHECK_LIST_STATUS_AFTER_PATCH: String(
          options.checkListStatusAfterPatch ?? 200,
        ),
        MOCK_CHECK_READ_STATUS: String(options.checkReadStatus ?? 200),
        MOCK_CHECKS: JSON.stringify(options.initialChecks ?? []),
        MOCK_DELETE_STATUSES: JSON.stringify(
          options.deleteStatusSequence ?? [options.deleteStatus ?? 204],
        ),
        MOCK_GIT_INDEX_FILE: gitIndexPath,
        MOCK_GIT_SEQUENCE_FILE: gitSequencePath,
        MOCK_MONOTONIC_NOW_SEQUENCE: JSON.stringify(
          options.monotonicNowSequence ?? [],
        ),
        MOCK_LATE_VISIBLE_CHECKS: JSON.stringify(
          options.lateVisibleChecks ?? [],
        ),
        MOCK_LATE_VISIBLE_CHECKS_DELAY: String(
          options.lateVisibleChecksDelay ?? 0,
        ),
        MOCK_MERGE_COMMIT_RESPONSES: JSON.stringify(
          (options.mergeCommitResponses ?? [{}]).map((response) => ({
            ...response,
            parents: response.parents ?? [baseSHA, headSHA],
          })),
        ),
        MOCK_PULLS: JSON.stringify(pulls),
        MOCK_FULL_PULLS: JSON.stringify(
          (options.mergeCommitSHASequence ?? [pull.merge_commit_sha]).map(
            (merge_commit_sha, index) => ({
              ...pull,
              base: {
                ...pull.base,
                sha:
                  options.mergeCommitBaseSHASequence?.[
                    Math.min(
                      index,
                      options.mergeCommitBaseSHASequence.length - 1,
                    )
                  ] ?? pull.base.sha,
              },
              merge_commit_sha,
            }),
          ),
        ),
        MOCK_PATCH_STATUS: String(options.patchStatus ?? 200),
        MOCK_PATCH_THROWS_AFTER_APPLY: String(
          options.patchThrowsAfterApply ?? false,
        ),
        MOCK_POST_RECONCILE_LIST_STATUSES: JSON.stringify(
          options.postReconcileListStatusSequence ?? [],
        ),
        MOCK_POST_THROWS_BEFORE_APPLY: String(
          options.postThrowsBeforeApply ?? false,
        ),
        MOCK_POST_THROWS_AFTER_APPLY: String(
          options.postThrowsAfterApply ?? false,
        ),
        MOCK_POST_VISIBILITY_DELAY: String(options.postVisibilityDelay ?? 0),
        MOCK_TRACE: tracePath,
        MOCK_WORKFLOW_RUNS: JSON.stringify(
          (
            options.currentWorkflowStatusSequence ?? [
              options.currentWorkflowStatus ?? workflowRun.status,
            ]
          ).map((status, index) => {
            const attemptSequence = options.currentWorkflowAttemptSequence ?? [
              options.currentWorkflowAttempt ?? workflowRun.run_attempt,
            ];
            return {
              ...workflowRun,
              run_attempt:
                attemptSequence[Math.min(index, attemptSequence.length - 1)],
              status,
            };
          }),
        ),
        MOCK_WORKFLOW_API_STATUSES: JSON.stringify(
          options.workflowAPIStatusSequence ?? [
            options.workflowAPIStatus ?? 200,
          ],
        ),
        PATH: `${bin}:${process.env.PATH ?? ""}`,
      },
    },
  );
  const trace = readFileSync(tracePath, "utf8")
    .trim()
    .split("\n")
    .filter(Boolean)
    .map(
      (line) =>
        JSON.parse(line) as {
          body: Record<string, unknown> | null;
          method: string;
          path: string;
        },
    );
  return { result, trace };
}
