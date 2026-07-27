import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { registerCloudSourceRunnerBehaviorTests } from "./cloud-source-runner-behavior.js";

const headSHA = "a".repeat(40);
const appId = 42;

registerCloudSourceRunnerBehaviorTests();

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

function runLifecycle(runAttempt: number, initialChecks: unknown[]) {
  const directory = mkdtempSync(join(tmpdir(), "ha-nova-source-lifecycle-"));
  const preloadPath = join(directory, "mock-fetch.mjs");
  const runnerPath = join(directory, "runner.mjs");
  const tracePath = join(directory, "trace.jsonl");
  const workflowRun = {
    head_sha: headSHA,
    id: 123,
    run_attempt: runAttempt,
  };
  writeFileSync(
    runnerPath,
    `import { createCloudSourceCheckReporter } from ${JSON.stringify(
      join(
        process.cwd(),
        "scripts",
        "release",
        "cloud-source-check-reporter.mjs",
      ),
    )};
const reporter = createCloudSourceCheckReporter({
  appId: 42,
  repository: "markusleben/ha-nova",
  runId: "999",
  token: "dedicated-token-at-least-twenty-characters",
});
await reporter.ensurePendingCheck(
  JSON.parse(process.env.MOCK_WORKFLOW_RUN),
  process.env.MOCK_TARGET_SHA,
);
`,
    "utf8",
  );
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
  const parsed = new URL(url);
  const path = parsed.pathname;
  const body = init.body === undefined ? null : JSON.parse(init.body);
  appendFileSync(process.env.MOCK_TRACE, JSON.stringify({ method, path, body }) + "\\n");
  if (path.endsWith("/check-runs") && method === "GET") {
    const page = Number(parsed.searchParams.get("page") ?? "1");
    const start = (page - 1) * 100;
    return response({
      check_runs: checks.slice(start, start + 100),
      total_count: checks.length,
    });
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
    ["--import", preloadPath, runnerPath],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        MOCK_CHECKS: JSON.stringify(initialChecks),
        MOCK_TARGET_SHA: headSHA,
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
  it("creates one attempt-bound check for a completed CI delivery", () => {
    const { result, trace } = runLifecycle(1, []);
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

  it("reuses the same pending check for a duplicate completed delivery", () => {
    const { result, trace } = runLifecycle(1, [check(1, "in_progress")]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
  });

  it("collapses duplicate pending deliveries to one canonical check", () => {
    const first = check(1, "in_progress");
    const second = { ...check(1, "in_progress"), id: 700 };
    const { result, trace } = runLifecycle(1, [second, first]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    expect(
      trace.filter(
        (entry) =>
          entry.method === "DELETE" && entry.path.endsWith("/check-runs/700"),
      ),
    ).toHaveLength(1);
  });

  it("creates a new pending check for a rerun attempt despite prior success", () => {
    const { result, trace } = runLifecycle(2, [check(1, "completed")]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    const post = trace.find((entry) => entry.method === "POST");
    expect(post?.body?.external_id).toBe(
      `workflow-run:123:attempt:2:target:${headSHA}`,
    );
  });

  it("does not regress a terminal check for a delayed lifecycle delivery", () => {
    const { result, trace } = runLifecycle(1, [check(1, "completed")]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(trace.some((entry) => entry.method === "POST")).toBe(false);
    expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
  });

  it("retries a terminal failure for the same target and attempt", () => {
    const failed = {
      ...check(1, "completed"),
      conclusion: "failure",
    };
    const { result, trace } = runLifecycle(1, [failed]);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(
      trace.some(
        (entry) =>
          entry.method === "DELETE" &&
          entry.path.endsWith(`/check-runs/${failed.id}`),
      ),
    ).toBe(true);
    expect(
      trace.find((entry) => entry.method === "POST")?.body?.external_id,
    ).toBe(`workflow-run:123:attempt:1:target:${headSHA}`);
  });

  it("fails closed when duplicate terminal conclusions conflict", () => {
    const success = check(1, "completed");
    const failure = {
      ...check(1, "completed"),
      conclusion: "failure",
      id: 701,
    };
    const { result, trace } = runLifecycle(1, [success, failure]);
    expect(result.status).not.toBe(0);
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

  it("finds conflicting exact-target terminals beyond the first 100 checks", () => {
    const successes = Array.from({ length: 100 }, (_, index) => ({
      ...check(1, "completed"),
      id: 1_000 + index,
    }));
    const hiddenFailure = {
      ...check(1, "completed"),
      conclusion: "failure",
      id: 2_000,
    };
    const { result, trace } = runLifecycle(1, [...successes, hiddenFailure]);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "source checks have conflicting terminal conclusions",
    );
    expect(
      trace.filter(
        (entry) => entry.method === "GET" && entry.path.endsWith("/check-runs"),
      ),
    ).toHaveLength(2);
    expect(
      trace.some(
        (entry) =>
          entry.method === "PATCH" && entry.body?.conclusion === "failure",
      ),
    ).toBe(true);
  });
});
