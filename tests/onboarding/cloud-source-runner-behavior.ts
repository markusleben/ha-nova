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

import { describe, expect, it } from "vitest";

export const headSHA = "a".repeat(40);
const baseSHA = "b".repeat(40);
export const mergeSHA = "d".repeat(40);

export type RunOptions = {
  action?: "completed" | "in_progress";
  associationPresentSequence?: boolean[];
  associationMergeCommitSHA?: null | string;
  bashExit?: number;
  conclusion?: "cancelled" | "failure" | "success";
  event?: "merge_group" | "pull_request";
  gitSHA?: null | string;
  initialChecks?: unknown[];
  mergeCommitSHA?: null | string;
  mergeCommitSHASequence?: Array<null | string>;
  checkListStatusAfterPatch?: number;
  checkReadStatus?: number;
  patchStatus?: number;
  patchThrowsAfterApply?: boolean;
  pullCount?: number;
  currentWorkflowStatus?: "completed" | "in_progress";
  currentWorkflowAttempt?: number;
  currentWorkflowStatusSequence?: Array<"completed" | "in_progress">;
  workflowAPIStatus?: number;
};

export function runSourceGate(options: RunOptions = {}) {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-source-runner-"));
  const bin = join(root, "bin");
  const eventPath = join(root, "event.json");
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
if [ -n "$MOCK_GIT_SHA" ]; then
  printf '%s\\t%s\\n' "$MOCK_GIT_SHA" "$4"
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

  writeFileSync(
    preloadPath,
    `import { appendFileSync } from "node:fs";
let checks = JSON.parse(process.env.MOCK_CHECKS);
const pulls = JSON.parse(process.env.MOCK_PULLS);
const fullPulls = JSON.parse(process.env.MOCK_FULL_PULLS);
let fullPullIndex = 0;
let terminalPatchCompleted = false;
const associationPresent = JSON.parse(process.env.MOCK_ASSOCIATION_PRESENT);
let associationIndex = 0;
const workflowRuns = JSON.parse(process.env.MOCK_WORKFLOW_RUNS);
let workflowRunIndex = 0;
globalThis.setTimeout = (callback) => {
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
      Number(process.env.MOCK_WORKFLOW_API_STATUS),
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
  if (path.endsWith("/check-runs") && method === "GET") {
    return response(
      { check_runs: checks, total_count: checks.length },
      terminalPatchCompleted
        ? Number(process.env.MOCK_CHECK_LIST_STATUS_AFTER_PATCH)
        : 200,
    );
  }
  if (path.endsWith("/check-runs") && method === "POST") {
    const created = { ...body, app: { id: 42 }, id: 900 };
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
    checks = checks.filter((candidate) => candidate.id !== id);
    return response({}, 204);
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
        MOCK_GIT_SHA:
          options.gitSHA === undefined
            ? event === "pull_request"
              ? (pull.merge_commit_sha ?? "")
              : headSHA
            : (options.gitSHA ?? ""),
        MOCK_PULLS: JSON.stringify(pulls),
        MOCK_FULL_PULLS: JSON.stringify(
          (options.mergeCommitSHASequence ?? [pull.merge_commit_sha]).map(
            (merge_commit_sha) => ({ ...pull, merge_commit_sha }),
          ),
        ),
        MOCK_PATCH_STATUS: String(options.patchStatus ?? 200),
        MOCK_PATCH_THROWS_AFTER_APPLY: String(
          options.patchThrowsAfterApply ?? false,
        ),
        MOCK_TRACE: tracePath,
        MOCK_WORKFLOW_RUNS: JSON.stringify(
          (
            options.currentWorkflowStatusSequence ?? [
              options.currentWorkflowStatus ?? workflowRun.status,
            ]
          ).map((status) => ({
            ...workflowRun,
            run_attempt:
              options.currentWorkflowAttempt ?? workflowRun.run_attempt,
            status,
          })),
        ),
        MOCK_WORKFLOW_API_STATUS: String(options.workflowAPIStatus ?? 200),
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

export function registerCloudSourceRunnerBehaviorTests(): void {
  describe("Cloud source runner behavior", () => {
    it("creates only a provisional pending check while CI is in progress", () => {
      const { result, trace } = runSourceGate({ action: "in_progress" });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "provisional source check recorded for active CI",
      );
      const post = trace.find((entry) => entry.method === "POST");
      expect(post?.body?.external_id).toBe(
        `workflow-run:123:attempt:1:target:${headSHA}`,
      );
      expect(
        trace.some((entry) => entry.path.includes(`/commits/${headSHA}/pulls`)),
      ).toBe(false);
    });

    it("replaces the provisional check only after the exact check is pending", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        initialChecks: [provisional],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      const exactPostIndex = trace.findIndex(
        (entry) =>
          entry.method === "POST" &&
          entry.body?.external_id ===
            `workflow-run:123:attempt:1:target:${mergeSHA}`,
      );
      const provisionalDeleteIndex = trace.findIndex(
        (entry) =>
          entry.method === "DELETE" &&
          entry.path.endsWith(`/check-runs/${provisional.id}`),
      );
      expect(exactPostIndex).toBeGreaterThanOrEqual(0);
      expect(provisionalDeleteIndex).toBeGreaterThan(exactPostIndex);
    });

    it.each(["cancelled", "failure"] as const)(
      "exits without a check after %s CI",
      (conclusion) => {
        const { result, trace } = runSourceGate({ conclusion });
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(result.stdout).toContain("no source check emitted");
        expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      },
    );

    it("keeps infrastructure failures before check creation visible", () => {
      const { result, trace } = runSourceGate({ workflowAPIStatus: 500 });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("returned HTTP 500");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    });

    it.each([
      [null, "no longer current"],
      ["c".repeat(40), "moved before source verification"],
    ] as const)(
      "exits without a check when the merge-queue ref is stale: %s",
      (gitSHA, message) => {
        const { result, trace } = runSourceGate({
          event: "merge_group",
          gitSHA,
        });
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(result.stdout).toContain(message);
        expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      },
    );

    it("reports successful completed verification once", () => {
      const { result, trace } = runSourceGate({ event: "merge_group" });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.filter((entry) => entry.method === "POST")).toHaveLength(1);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toHaveLength(1);
    });

  });
}
