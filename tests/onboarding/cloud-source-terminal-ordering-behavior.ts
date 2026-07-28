import { describe, expect, it } from "vitest";

import { mergeSHA, runSourceGate } from "./cloud-source-runner-behavior.js";

export function registerCloudSourceTerminalOrderingBehaviorTests(): void {
  describe("Cloud source terminal ordering", () => {
    it("rejects a merge ref that moves after final PR verification", () => {
      const { result, trace } = runSourceGate({
        gitSHASequence: [mergeSHA, mergeSHA, "c".repeat(40)],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stderr).toContain(
        "source ref changed immediately before terminal reporting",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });

    it("drops success when a newer CI attempt starts during the final ref read", () => {
      const { result, trace } = runSourceGate({
        currentWorkflowAttemptSequence: [1, 1, 1, 2],
        currentWorkflowStatusSequence: [
          "completed",
          "completed",
          "completed",
          "in_progress",
        ],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("older CI attempt");
      const finalRefRead = trace.reduce(
        (last, entry, index) => (entry.method === "GIT" ? index : last),
        -1,
      );
      const terminalCIRead = trace.reduce(
        (last, entry, index) =>
          entry.method === "GET" &&
          entry.path.endsWith("/actions/runs/123")
            ? index
            : last,
        -1,
      );
      expect(finalRefRead).toBeGreaterThan(-1);
      expect(terminalCIRead).toBeGreaterThan(finalRefRead);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" && entry.path.endsWith("/check-runs/900"),
        ),
      ).toBe(true);
    });
  });
}
