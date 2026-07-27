import { describe, expect, it } from "vitest";

import { runSourceGate } from "./cloud-source-runner-behavior.js";

export function registerCloudSourceMaterializationBehaviorTests(): void {
  describe("Cloud source PR materialization", () => {
    it("retries an absent full merge commit in one bounded run", () => {
      const { result, trace } = runSourceGate({ mergeCommitSHA: null });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain("merge commit is not materialized yet");
      expect(trace.some((entry) => entry.method === "POST")).toBe(false);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" && entry.path.endsWith("/pulls/449"),
        ),
      ).toHaveLength(3);
    });

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
  });
}
