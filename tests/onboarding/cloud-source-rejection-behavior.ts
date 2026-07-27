import { describe, expect, it } from "vitest";

import { runSourceGate } from "./cloud-source-runner-behavior.js";

export function registerCloudSourceRejectionBehaviorTests(): void {
  describe("Cloud source rejection behavior", () => {
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

    it("reports conflicting terminal state through a fail-safe App check", () => {
      const headSHA = "a".repeat(40);
      const externalId = `workflow-run:123:attempt:1:target:${headSHA}`;
      const { result, trace } = runSourceGate({
        event: "merge_group",
        initialChecks: [
          {
            app: { id: 42 },
            conclusion: "success",
            external_id: externalId,
            id: 702,
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

    it("keeps App-check reporting failures visible as workflow failures", () => {
      const { result, trace } = runSourceGate({
        bashExit: 1,
        event: "merge_group",
        patchStatus: 500,
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("cannot report rejection");
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
    });

    it("preserves a reported rejection when sibling cleanup fails", () => {
      const { result, trace } = runSourceGate({
        bashExit: 1,
        checkListStatusAfterPatch: 500,
        event: "merge_group",
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "rejection reported, but pending sibling cleanup failed",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" &&
            entry.body?.conclusion === "failure" &&
            entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(false);
    });

    it("reconciles an accepted rejection after its PATCH response times out", () => {
      const { result, trace } = runSourceGate({
        bashExit: 1,
        event: "merge_group",
        patchThrowsAfterApply: true,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(false);
    });

    it("does not delete a check when PATCH reconciliation is unavailable", () => {
      const { result, trace } = runSourceGate({
        bashExit: 1,
        checkReadStatus: 500,
        event: "merge_group",
        patchThrowsAfterApply: true,
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "cannot reconcile the ambiguous source-check completion",
      );
      expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
    });
  });
}
