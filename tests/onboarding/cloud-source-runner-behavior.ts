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

const headSHA = "a".repeat(40);
const baseSHA = "b".repeat(40);

type RunOptions = {
  action?: "completed" | "in_progress";
  bashExit?: number;
  conclusion?: "cancelled" | "failure" | "success";
  event?: "merge_group" | "pull_request";
  gitSHA?: null | string;
  initialChecks?: unknown[];
  mergeCommitSHA?: null | string;
  patchStatus?: number;
  pullCount?: number;
  workflowAPIStatus?: number;
};

function runSourceGate(options: RunOptions = {}) {
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
      options.mergeCommitSHA === undefined ? headSHA : options.mergeCommitSHA,
    number: 449,
    state: "open",
  };
  const pulls = Array.from({ length: options.pullCount ?? 1 }, (_, index) => ({
    ...pull,
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
const workflowRun = JSON.parse(process.env.MOCK_WORKFLOW_RUN);
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
    return response(workflowRun, Number(process.env.MOCK_WORKFLOW_API_STATUS));
  }
  if (path.endsWith("/commits/${headSHA}/pulls")) {
    return response(pulls);
  }
  if (path.endsWith("/pulls/449")) {
    return response(pulls[0]);
  }
  if (path.endsWith("/check-runs") && method === "GET") {
    return response({ check_runs: checks, total_count: checks.length });
  }
  if (path.endsWith("/check-runs") && method === "POST") {
    const created = { ...body, app: { id: 42 }, id: 900 };
    checks.push(created);
    return response(created, 201);
  }
  if (path.endsWith("/check-runs/900") && method === "PATCH") {
    checks = checks.map((candidate) =>
      candidate.id === 900 ? { ...candidate, ...body } : candidate
    );
    return response(checks[0], Number(process.env.MOCK_PATCH_STATUS));
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
        MOCK_BASH_EXIT: String(options.bashExit ?? 0),
        MOCK_CHECKS: JSON.stringify(options.initialChecks ?? []),
        MOCK_GIT_SHA:
          options.gitSHA === undefined ? headSHA : (options.gitSHA ?? ""),
        MOCK_PULLS: JSON.stringify(pulls),
        MOCK_PATCH_STATUS: String(options.patchStatus ?? 200),
        MOCK_TRACE: tracePath,
        MOCK_WORKFLOW_RUN: JSON.stringify(workflowRun),
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
    it("rejects every lifecycle event except completed", () => {
      const { result, trace } = runSourceGate({ action: "in_progress" });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "only a completed CI workflow event may request this check",
      );
      expect(trace).toHaveLength(0);
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

    it("exits without a check for a stale pull-request head", () => {
      const { result, trace } = runSourceGate({ pullCount: 0 });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "no longer identifies a current pull request",
      );
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/commits/${headSHA}/pulls`),
        ),
      ).toHaveLength(3);
    });

    it("exits without a check while GitHub materializes the merge commit", () => {
      const { result, trace } = runSourceGate({ mergeCommitSHA: null });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("merge commit is not materialized yet");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    });

    it("exits without a check while GitHub materializes the merge ref", () => {
      const { result, trace } = runSourceGate({ gitSHA: null });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("merge ref is not materialized yet");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    });

    it("exits without a check when API and merge ref are temporarily inconsistent", () => {
      const { result, trace } = runSourceGate({ gitSHA: "c".repeat(40) });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("temporarily inconsistent");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    });

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

    it("reports a verification rejection only through the App check", () => {
      const { result, trace } = runSourceGate({
        bashExit: 1,
        event: "merge_group",
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.filter((entry) => entry.method === "POST")).toHaveLength(1);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toHaveLength(1);
    });

    it("reports conflicting terminal state only through a fail-safe App check", () => {
      const externalId = `workflow-run:123:attempt:1:target:${headSHA}`;
      const { result, trace } = runSourceGate({
        event: "merge_group",
        initialChecks: [
          {
            app: { id: 42 },
            conclusion: "success",
            external_id: externalId,
            id: 700,
            name: "cloud-source-gate",
            status: "completed",
          },
          {
            app: { id: 42 },
            conclusion: "failure",
            external_id: externalId,
            id: 701,
            name: "cloud-source-gate",
            status: "completed",
          },
        ],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stderr).toContain(
        "source checks have conflicting terminal conclusions",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
    });

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

    it("keeps App-check reporting failures visible as workflow failures", () => {
      const { result } = runSourceGate({
        bashExit: 1,
        event: "merge_group",
        patchStatus: 500,
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("cannot report rejection");
    });
  });
}
