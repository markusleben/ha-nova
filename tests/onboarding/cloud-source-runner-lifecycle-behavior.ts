import { describe, expect, it } from "vitest";

import {
  headSHA,
  mergeSHA,
  runSourceGate,
} from "./cloud-source-runner-behavior.js";

function terminalConflict(targetSHA: string) {
  const externalId = `workflow-run:123:attempt:1:target:${targetSHA}`;
  return [
    {
      app: { id: 42 },
      conclusion: "failure",
      external_id: externalId,
      id: 701,
      name: "cloud-source-gate",
      status: "completed",
    },
    {
      app: { id: 42 },
      conclusion: "success",
      external_id: externalId,
      id: 702,
      name: "cloud-source-gate",
      status: "completed",
    },
  ];
}

function newerPending(targetSHA: string) {
  return {
    app: { id: 42 },
    external_id: `workflow-run:123:attempt:2:target:${targetSHA}`,
    id: 703,
    name: "cloud-source-gate",
    status: "in_progress",
  };
}

function expectStaleConflictCleanup(
  result: ReturnType<typeof runSourceGate>["result"],
  trace: ReturnType<typeof runSourceGate>["trace"],
  expectedPostCount: number,
) {
  expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  expect(result.stdout).toContain("older CI attempt");
  expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
  const posts = trace.filter((entry) => entry.method === "POST");
  expect(posts).toHaveLength(expectedPostCount);
  expect(
    posts.every((entry) =>
      String(entry.body?.external_id).startsWith(
        "workflow-run:123:attempt:1:target:",
      ),
    ),
  ).toBe(true);
  expect(
    trace.filter(
      (entry) =>
        entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
    ),
  ).toHaveLength(expectedPostCount);
  expect(
    trace.some(
      (entry) =>
        entry.method === "DELETE" && entry.path.endsWith("/check-runs/703"),
    ),
  ).toBe(false);
  expect(
    trace.some(
      (entry) =>
        entry.method === "DELETE" &&
        ["/check-runs/701", "/check-runs/702"].some((suffix) =>
          entry.path.endsWith(suffix),
        ),
    ),
  ).toBe(false);
}

export function registerCloudSourceRunnerLifecycleBehaviorTests(): void {
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

    it("drops an attempt-wide conflict fail-safe after a newer attempt starts", () => {
      const { result, trace } = runSourceGate({
        currentWorkflowAttemptSequence: [1, 2],
        currentWorkflowStatusSequence: ["completed", "in_progress"],
        initialChecks: [...terminalConflict(headSHA), newerPending(headSHA)],
      });
      expectStaleConflictCleanup(result, trace, 1);
    });

    it("drops an exact-target conflict fail-safe after a newer attempt starts", () => {
      const { result, trace } = runSourceGate({
        currentWorkflowAttemptSequence: [1, 1, 2],
        currentWorkflowStatusSequence: [
          "completed",
          "completed",
          "in_progress",
        ],
        initialChecks: [newerPending(mergeSHA)],
        lateVisibleChecks: terminalConflict(mergeSHA),
        lateVisibleChecksDelay: 1,
      });
      expectStaleConflictCleanup(result, trace, 1);
    });

    it("drops a raced conflict fail-safe after a newer attempt starts", () => {
      const { result, trace } = runSourceGate({
        currentWorkflowAttemptSequence: [1, 1, 2],
        currentWorkflowStatusSequence: [
          "completed",
          "completed",
          "in_progress",
        ],
        initialChecks: [newerPending(mergeSHA)],
        lateVisibleChecks: terminalConflict(mergeSHA),
        lateVisibleChecksDelay: 2,
      });
      expectStaleConflictCleanup(result, trace, 2);
    });
  });
}
