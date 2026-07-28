import { describe, expect, it } from "vitest";

import {
  headSHA,
  mergeSHA,
  runSourceGate,
} from "./cloud-source-runner-behavior.js";

export function registerCloudSourceMaterializationWindowBehaviorTests(): void {
  describe("Cloud source PR materialization window", () => {
    it("accepts a PR association materialized at the deadline", () => {
      const { result, trace } = runSourceGate({
        associationPresentSequence: [...Array<boolean>(30).fill(false), true],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      const exactCheckIndex = trace.findIndex(
        (entry) => entry.method === "POST",
      );
      expect(
        trace
          .slice(0, exactCheckIndex)
          .filter(
            (entry) =>
              entry.method === "GET" &&
              entry.path.endsWith(`/commits/${headSHA}/pulls`),
          ),
      ).toHaveLength(31);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("accepts a merge commit materialized at the deadline", () => {
      const { result, trace } = runSourceGate({
        mergeCommitSHASequence: [...Array<null>(30).fill(null), mergeSHA],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      const exactCheckIndex = trace.findIndex(
        (entry) => entry.method === "POST",
      );
      expect(
        trace
          .slice(0, exactCheckIndex)
          .filter(
            (entry) =>
              entry.method === "GET" && entry.path.endsWith("/pulls/449"),
          ),
      ).toHaveLength(31);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it.each([
      ["absent", null],
      ["inconsistent", "c".repeat(40)],
    ] as const)(
      "accepts a merge ref that becomes consistent at the deadline: %s",
      (_name, delayedRef) => {
        const { result, trace } = runSourceGate({
          gitSHASequence: [
            ...Array<null | string>(30).fill(delayedRef),
            mergeSHA,
            mergeSHA,
          ],
        });
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(
          trace.some(
            (entry) =>
              entry.method === "PATCH" && entry.body?.conclusion === "success",
          ),
        ).toBe(true);
      },
    );

    it("never accepts source consistency after the bounded deadline", () => {
      const { result, trace } = runSourceGate({
        monotonicNowSequence: [0, 90_001, 90_001],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "source materialized after the bounded deadline",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });

    it("reconciles an exact pending check after its POST response times out", () => {
      const { result, trace } = runSourceGate({
        postThrowsAfterApply: true,
        postVisibilityDelay: 2,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
      expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
    });

    it("retries a transient list failure during POST reconciliation", () => {
      const { result, trace } = runSourceGate({
        postReconcileListStatusSequence: [500, 200],
        postThrowsAfterApply: true,
        postVisibilityDelay: 1,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("reconciles a fail-safe check after its POST response times out", () => {
      const { result, trace } = runSourceGate({
        mergeCommitSHA: null,
        postThrowsAfterApply: true,
        postVisibilityDelay: 2,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
      expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
    });

    it("eventually deletes a delayed pending check after unresolved POST ambiguity", () => {
      const { result, trace } = runSourceGate({
        deleteStatusSequence: [500, 204],
        postThrowsAfterApply: true,
        postVisibilityDelay: 4,
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "source-check creation remained invisible",
      );
      expect(
        trace.filter(
          (entry) =>
            entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
        ),
      ).toHaveLength(2);
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
    });

    it("never overwrites terminal success found during POST reconciliation", () => {
      const terminalSuccess = {
        app: { id: 42 },
        conclusion: "success",
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 701,
        name: "cloud-source-gate",
        status: "completed",
      };
      const { result, trace } = runSourceGate({
        lateVisibleChecks: [terminalSuccess],
        lateVisibleChecksDelay: 2,
        mergeCommitSHA: null,
        postThrowsBeforeApply: true,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stderr).toContain(
        "terminal source check raced fail-safe creation",
      );
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
      expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
    });

    it.each([
      ["pending during reconciliation after terminal", 2, 1],
      ["pending during cleanup after terminal", 2, 4],
      ["terminal during final reconciliation scan after pending", 4, 0],
    ] as const)(
      "cleans an accepted hidden POST beside a late terminal success %s",
      (_label, lateVisibleChecksDelay, postVisibilityDelay) => {
        const terminalSuccess = {
          app: { id: 42 },
          conclusion: "success",
          external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
          id: 701,
          name: "cloud-source-gate",
          status: "completed",
        };
        const { result, trace } = runSourceGate({
          lateVisibleChecks: [terminalSuccess],
          lateVisibleChecksDelay,
          mergeCommitSHA: null,
          postThrowsAfterApply: true,
          postVisibilityDelay,
        });
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(
          trace.some(
            (entry) =>
              entry.method === "DELETE" &&
              entry.path.endsWith("/check-runs/900"),
          ),
        ).toBe(true);
        expect(
          trace.some(
            (entry) =>
              entry.method === "DELETE" &&
              entry.path.endsWith(`/check-runs/${terminalSuccess.id}`),
          ),
        ).toBe(false);
        expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
      },
    );

    it("cleans a provisional check when the post-window CI refresh fails", () => {
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
        workflowAPIStatusSequence: [200, 500],
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("returned HTTP 500");
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(`/check-runs/${provisional.id}`),
        ),
      ).toBe(true);
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
    });

    it("drops a timeout rejection after a newer CI attempt starts", () => {
      const provisional = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
        id: 700,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        currentWorkflowAttemptSequence: [1, 2],
        currentWorkflowStatusSequence: ["completed", "in_progress"],
        initialChecks: [provisional],
        mergeCommitSHA: null,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("older CI attempt");
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(`/check-runs/${provisional.id}`),
        ),
      ).toBe(true);
    });

    it.each([
      ["reused provisional", true],
      ["new fail-safe", false],
    ] as const)(
      "drops %s when a newer CI attempt starts during rejection",
      (_label, reuseProvisional) => {
        const provisional = {
          app: { id: 42 },
          external_id: `workflow-run:123:attempt:1:target:${headSHA}`,
          id: 700,
          name: "cloud-source-gate",
          status: "in_progress",
        };
        const { result, trace } = runSourceGate({
          currentWorkflowAttemptSequence: [1, 1, 2],
          currentWorkflowStatusSequence: [
            "completed",
            "completed",
            "in_progress",
          ],
          initialChecks: reuseProvisional ? [provisional] : [],
          mergeCommitSHA: null,
        });
        expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
        expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
        expect(
          trace.some(
            (entry) =>
              entry.method === "DELETE" &&
              entry.path.endsWith(
                `/check-runs/${reuseProvisional ? provisional.id : 900}`,
              ),
          ),
        ).toBe(true);
      },
    );

    it("does not reject or delete a newer attempt after policy failure", () => {
      const newerAttempt = {
        app: { id: 42 },
        external_id: `workflow-run:123:attempt:2:target:${mergeSHA}`,
        id: 702,
        name: "cloud-source-gate",
        status: "in_progress",
      };
      const { result, trace } = runSourceGate({
        bashExit: 1,
        currentWorkflowAttemptSequence: [1, 1, 2],
        currentWorkflowStatusSequence: [
          "completed",
          "completed",
          "in_progress",
        ],
        initialChecks: [newerAttempt],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(`/check-runs/${newerAttempt.id}`),
        ),
      ).toBe(false);
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
    });

    it("never converts stale cleanup failure into terminal rejection", () => {
      const { result, trace } = runSourceGate({
        bashExit: 1,
        currentWorkflowAttemptSequence: [1, 1, 2],
        currentWorkflowStatusSequence: [
          "completed",
          "completed",
          "in_progress",
        ],
        deleteStatus: 500,
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("cannot clean the stale CI attempt");
      expect(trace.some((entry) => entry.method === "PATCH")).toBe(false);
    });

    it("removes exact pending state when a newer attempt starts before success", () => {
      const { result, trace } = runSourceGate({
        currentWorkflowAttemptSequence: [1, 1, 2],
        currentWorkflowStatusSequence: [
          "completed",
          "completed",
          "in_progress",
        ],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("older CI attempt");
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });
  });
}
