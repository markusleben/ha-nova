import { describe, expect, it } from "vitest";

import { headSHA, runSourceGate } from "./cloud-source-runner-behavior.js";

export function registerCloudSourceMaterializationBehaviorTests(): void {
  describe("Cloud source PR materialization", () => {
    it("retries an absent merge ref in one bounded run", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        initialChecks: [provisional],
        mergeCommitSHA: null,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("merge ref is not materialized yet");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" && entry.path.endsWith("/pulls/449"),
        ),
      ).toHaveLength(31);
      const retryDelays = trace.filter((entry) => entry.method === "TIMER");
      expect(retryDelays).toHaveLength(30);
      expect(retryDelays.every((entry) => entry.path === "3000")).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" &&
            entry.path.endsWith(`/check-runs/${provisional.id}`) &&
            entry.body?.conclusion === "failure" &&
            String(
              (entry.body?.output as { summary?: unknown } | undefined)
                ?.summary,
            ).includes("Re-run CI once"),
        ),
      ).toBe(true);
    });

    it("creates one fail-safe rejection without a provisional check", () => {
      const { result, trace } = runSourceGate({ mergeCommitSHA: null });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.filter((entry) => entry.method === "POST")).toHaveLength(1);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toHaveLength(1);
    });

    it("does not recreate a provisional check after CI completed", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        action: "in_progress",
        currentWorkflowStatus: "completed",
        initialChecks: [provisional],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("upstream CI already completed");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
    });

    it("removes a provisional check when CI completes during the early broker", () => {
      const { result, trace } = runSourceGate({
        action: "in_progress",
        currentWorkflowStatusSequence: ["in_progress", "completed"],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.filter((entry) => entry.method === "POST")).toHaveLength(1);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
    });

    it("cleans a stale attempt after an upstream CI rerun", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        action: "in_progress",
        currentWorkflowAttempt: 2,
        initialChecks: [provisional],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("older CI attempt");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(`/check-runs/${provisional.id}`),
        ),
      ).toBe(true);
    });

    it("tolerates one late association miss before materialization", () => {
      const { result, trace } = runSourceGate({
        associationPresentSequence: [
          ...Array<boolean>(18).fill(true),
          false,
          true,
        ],
        mergeCommitResponses: [{ parents: ["b".repeat(40), "c".repeat(40)] }],
        mergeCommitSHASequence: [...Array<null>(18).fill(null), "d".repeat(40)],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/commits/${headSHA}/pulls`),
        ),
      ).toHaveLength(21);
      expect(trace.filter((entry) => entry.method === "TIMER")).toHaveLength(
        19,
      );
    });

    it("keeps a timeout rejection immutable on duplicate delivery", () => {
      const terminal = {
        app: { id: 42 },
        conclusion: "failure",
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "completed",
      };
      const { result, trace } = runSourceGate({
        initialChecks: [terminal],
        mergeCommitSHA: null,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("already has a terminal result");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
    });

    it("deletes a fail-safe check when terminal reporting fails", () => {
      const { result, trace } = runSourceGate({
        mergeCommitSHA: null,
        patchStatus: 500,
      });
      expect(result.status).not.toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
    });

    it("deletes a reused provisional when terminal reporting fails", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        initialChecks: [provisional],
        mergeCommitSHA: null,
        patchStatus: 500,
      });
      expect(result.status).not.toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(`/check-runs/${provisional.id}`),
        ),
      ).toBe(true);
    });

    it.each([
      ["new fail-safe", []],
      [
        "reused provisional",
        [
          {
            app: { id: 42 },
            external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
            id: 700,
            name: "cloud-source-gate",
            status: "in_progress",
          },
        ],
      ],
    ] as const)(
      "reconciles an accepted %s rejection after its PATCH response times out",
      (_label, initialChecks) => {
        const { result, trace } = runSourceGate({
          initialChecks: [...initialChecks],
          mergeCommitSHA: null,
          patchThrowsAfterApply: true,
        });
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
      },
    );

    it("reads merge materialization from the full pull-request resource", () => {
      const { result, trace } = runSourceGate({
        associationMergeCommitSHA: null,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "GET" && entry.path.endsWith("/pulls/449"),
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("rejects when current pull-request association stays absent", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const exactPending = {
        ...provisional,
        external_id: `workflow-run:123:attempt:1:target:${"d".repeat(40)}`,
        id: 701,
      };
      const { result, trace } = runSourceGate({
        initialChecks: [provisional, exactPending],
        pullCount: 0,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "no longer identifies a current pull request",
      );
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" &&
            entry.path.endsWith(`/check-runs/${provisional.id}`) &&
            entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(`/check-runs/${exactPending.id}`),
        ),
      ).toBe(true);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/commits/${headSHA}/pulls`),
        ),
      ).toHaveLength(31);
    });

    it("reports a fail-safe rejection when the merge ref stays absent", () => {
      const { result, trace } = runSourceGate({ gitSHA: null });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("merge ref is not materialized yet");
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
    });

    it("reports a fail-safe rejection for persistent source inconsistency", () => {
      const { result, trace } = runSourceGate({ gitSHA: "c".repeat(40) });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("temporarily inconsistent");
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
    });
  });
}
