import { describe, expect, it } from "vitest";

import {
  headSHA,
  mergeSHA,
  runSourceGate,
} from "./cloud-source-runner-behavior.js";

export function registerCloudSourceMergeRefFallbackBehaviorTests(): void {
  describe("Cloud source merge-ref fallback", () => {
    it("accepts an exact merge ref while the full PR merge SHA is null", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/git/commits/${mergeSHA}`),
        ),
      ).toHaveLength(2);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("rejects a merge ref that does not contain the current head", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [{ parents: ["b".repeat(40), "c".repeat(40)] }],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "merge ref does not match the current base and head",
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

    it("keeps API/ref equality authoritative when the API SHA exists", () => {
      const { result, trace } = runSourceGate({
        gitSHA: "c".repeat(40),
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(result.stdout).toContain(
        "pull request API and merge ref are temporarily inconsistent",
      );
      expect(trace.some((entry) => entry.path.includes("/git/commits/"))).toBe(
        false,
      );
    });

    it("requires the GitHub merge-parent order", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [{ parents: [headSHA, "b".repeat(40)] }],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "failure",
        ),
      ).toBe(true);
    });

    it.each([
      ["wrong base", ["c".repeat(40), headSHA]],
      ["one parent", ["b".repeat(40)]],
      ["three parents", ["b".repeat(40), headSHA, "c".repeat(40)]],
    ])("rejects an invalid merge-parent shape: %s", (_label, parents) => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [{ parents }],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/git/commits/${mergeSHA}`),
        ),
      ).toHaveLength(31);
      expect(trace.filter((entry) => entry.method === "TIMER")).toHaveLength(
        30,
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });

    it("allows strict equality to resolve an earlier API/ref mismatch", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitSHASequence: ["c".repeat(40), mergeSHA, null],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/git/commits/${mergeSHA}`),
        ),
      ).toHaveLength(1);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("does not carry an API mismatch across a changed base identity", () => {
      const changedBase = "e".repeat(40);
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitBaseSHASequence: ["b".repeat(40), changedBase],
        mergeCommitResponses: [
          { parents: [changedBase, headSHA] },
          { parents: [changedBase, headSHA] },
        ],
        mergeCommitSHASequence: ["c".repeat(40), null],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("retains unresolved mismatches for multiple source identities", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitBaseSHASequence: [
          "b".repeat(40),
          "e".repeat(40),
          "b".repeat(40),
        ],
        mergeCommitSHASequence: ["c".repeat(40), "c".repeat(40), null],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.some((entry) => entry.path.includes("/git/commits/"))).toBe(
        false,
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });

    it("retries a temporarily absent merge commit object", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [{ status: 404 }, {}, {}],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/git/commits/${mergeSHA}`),
        ),
      ).toHaveLength(3);
      expect(trace.filter((entry) => entry.method === "TIMER")).toHaveLength(1);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("accepts parent materialization inside the bounded window", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [
          { parents: ["b".repeat(40), "c".repeat(40)] },
          {},
          {},
        ],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.filter((entry) => entry.method === "TIMER")).toHaveLength(1);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it.each([500, 503])(
      "fails loud when the merge commit API returns HTTP %s",
      (status) => {
        const { result, trace } = runSourceGate({
          gitSHA: mergeSHA,
          mergeCommitResponses: [{ status }],
          mergeCommitSHA: null,
        });

        expect(result.status).not.toBe(0);
        expect(result.stderr).toContain(`returned HTTP ${status}`);
        expect(trace.some((entry) => entry.method === "TIMER")).toBe(false);
      },
    );

    it("fails loud when the merge commit response identifies another SHA", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [{ sha: "c".repeat(40) }],
        mergeCommitSHA: null,
      });

      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "pull request merge ref commit response is invalid",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });

    it("rejects parent state that changes before final verification", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitResponses: [
          {},
          { parents: ["b".repeat(40), "c".repeat(40)] },
        ],
        mergeCommitSHA: null,
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
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

    it("accepts an API SHA that appears after parent verification", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitSHASequence: [null, mergeSHA],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.filter(
          (entry) =>
            entry.method === "GET" &&
            entry.path.endsWith(`/git/commits/${mergeSHA}`),
        ),
      ).toHaveLength(1);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(true);
    });

    it("rejects an API mismatch that appears after parent verification", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitSHASequence: [null, "c".repeat(40)],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
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

    it("never falls back after observing an API/ref mismatch", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitSHASequence: ["c".repeat(40), null],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.filter((entry) => entry.method === "TIMER")).toHaveLength(
        30,
      );
      expect(result.stdout).toContain(
        "pull request API and merge ref are temporarily inconsistent",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PATCH" && entry.body?.conclusion === "success",
        ),
      ).toBe(false);
    });

    it("rejects a mid-run API mismatch even if the API later returns null", () => {
      const { result, trace } = runSourceGate({
        gitSHA: mergeSHA,
        mergeCommitSHASequence: [null, "c".repeat(40), null],
      });

      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
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
  });
}
