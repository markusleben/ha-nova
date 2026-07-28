import { describe, expect, it } from "vitest";

import {
  headSHA,
  mergeSHA,
  runSourceGate,
} from "./cloud-source-runner-behavior.js";

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
  });
}
