import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const headSHA = "a".repeat(40);
const appId = 42;

type LifecycleAction = "in_progress" | "requested";

function check(runAttempt: number, status: "completed" | "in_progress") {
  return {
    app: { id: appId },
    conclusion: status === "completed" ? "success" : null,
    external_id: `workflow-run:123:attempt:${runAttempt}:target:${headSHA}`,
    id: 500 + runAttempt,
    name: "cloud-source-gate",
    status,
  };
}

function runLifecycle(
  action: LifecycleAction,
  runAttempt: number,
  initialChecks: unknown[],
) {
  const directory = mkdtempSync(join(tmpdir(), "ha-nova-source-lifecycle-"));
  const eventPath = join(directory, "event.json");
  const preloadPath = join(directory, "mock-fetch.mjs");
  const tracePath = join(directory, "trace.jsonl");
  const status = action === "requested" ? "queued" : "in_progress";
  const workflowRun = {
    event: "merge_group",
    head_branch: "gh-readonly-queue/main/pr-123-deadbeef",
    head_sha: headSHA,
    id: 123,
    name: "CI",
    run_attempt: runAttempt,
    status,
    workflow_id: 77,
  };
  writeFileSync(eventPath, JSON.stringify({ action, workflow_run: workflowRun }), "utf8");
  writeFileSync(
    preloadPath,
    `import { appendFileSync } from "node:fs";
let checks = JSON.parse(process.env.MOCK_CHECKS);
const workflowRun = JSON.parse(process.env.MOCK_WORKFLOW_RUN);
function response(data, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => data };
}
globalThis.fetch = async (url, init = {}) => {
  const method = init.method ?? "GET";
  const path = new URL(url).pathname;
  const body = init.body === undefined ? null : JSON.parse(init.body);
  appendFileSync(process.env.MOCK_TRACE, JSON.stringify({ method, path, body }) + "\\n");
  if (path.endsWith("/actions/workflows/ci.yml")) {
    return response({ id: 77, path: ".github/workflows/ci.yml" });
  }
  if (path.endsWith("/actions/runs/123")) {
    return response(workflowRun);
  }
  if (path.endsWith("/check-runs") && method === "GET") {
    return response({ check_runs: checks });
  }
  if (path.endsWith("/check-runs") && method === "POST") {
    const created = { ...body, app: { id: 42 }, id: 900, status: "in_progress" };
    checks.push(created);
    return response(created, 201);
  }
  if (path.includes("/check-runs/") && method === "DELETE") {
    const checkId = Number(path.split("/").pop());
    checks = checks.filter((candidate) => candidate.id !== checkId);
    return response({}, 204);
  }
  if (path.includes("/check-runs/") && method === "PATCH") {
    const checkId = Number(path.split("/").pop());
    checks = checks.map((candidate) =>
      candidate.id === checkId ? { ...candidate, ...body } : candidate
    );
    return response(checks.find((candidate) => candidate.id === checkId));
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
        HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID: String(appId),
        HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN: "dedicated-token-at-least-twenty-characters",
        MOCK_CHECKS: JSON.stringify(initialChecks),
        MOCK_TRACE: tracePath,
        MOCK_WORKFLOW_RUN: JSON.stringify(workflowRun),
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

describe("Cloud source check lifecycle", () => {
  it("creates one attempt-bound pending check when CI is requested", () => {
    const { result, trace } = runLifecycle("requested", 1, []);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    const posts = trace.filter((entry) => entry.method === "POST");
    expect(posts).toHaveLength(1);
    const [post] = posts;
    if (post === undefined) {
      throw new Error("pending check was not created");
    }
    expect(post.body).toMatchObject({
      external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
      head_sha: headSHA,
      name: "cloud-source-gate",
      status: "in_progress",
    });
    expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
  });

  it("reuses the same pending check for in-progress delivery", () => {
    const { result, trace } = runLifecycle("in_progress", 1, [check(1, "in_progress")]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
  });

  it("collapses duplicate pending deliveries to one canonical check", () => {
    const first = check(1, "in_progress");
    const second = { ...check(1, "in_progress"), id: 700 };
    const { result, trace } = runLifecycle("in_progress", 1, [second, first]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    expect(
      trace.filter(
        (entry) => entry.method === "DELETE" && entry.path.endsWith("/check-runs/700"),
      ),
    ).toHaveLength(1);
  });

  it("creates a new pending check for a rerun attempt despite prior success", () => {
    const { result, trace } = runLifecycle("in_progress", 2, [check(1, "completed")]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    const post = trace.find((entry) => entry.method === "POST");
    expect(post?.body?.external_id).toBe(`workflow-run:123:attempt:2:target:${headSHA}`);
  });

  it("does not regress a terminal check for a delayed lifecycle delivery", () => {
    const { result, trace } = runLifecycle("in_progress", 1, [check(1, "completed")]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
  });

  it("retries a terminal failure for the same target and attempt", () => {
    const failed = {
      ...check(1, "completed"),
      conclusion: "failure",
    };
    const { result, trace } = runLifecycle("in_progress", 1, [failed]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(
      trace.some(
        (entry) =>
          entry.method === "DELETE" && entry.path.endsWith(`/check-runs/${failed.id}`),
      ),
    ).toBe(true);
    expect(trace.find((entry) => entry.method === "POST")?.body?.external_id).toBe(
      `workflow-run:123:attempt:1:target:${headSHA}`,
    );
  });

  it("fails closed when duplicate terminal conclusions conflict", () => {
    const success = check(1, "completed");
    const failure = {
      ...check(1, "completed"),
      conclusion: "failure",
      id: 701,
    };
    const { result, trace } = runLifecycle("in_progress", 1, [success, failure]);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "source checks have conflicting terminal conclusions",
    );
    expect(
      trace.some(
        (entry) => entry.method === "PATCH" && entry.body?.conclusion === "failure",
      ),
    ).toBe(true);
  });
});
