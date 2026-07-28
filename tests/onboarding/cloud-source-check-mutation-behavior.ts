import { describe, expect, it } from "vitest";

type Candidate = ReturnType<typeof reconciliationCandidate>;
type Scan = Candidate[] | Error;
type ReconciliationOptions = {
  cleanupPending: () => Promise<void>;
  create: () => Promise<Candidate>;
  listMatches: () => Promise<Candidate[]>;
  retainPendingOnTerminalConflict?: boolean;
  waitFor?: () => Promise<void>;
};

function reconciliationCandidate(
  id: number,
  status: "completed" | "in_progress",
  conclusion: "failure" | "success" | null = null,
) {
  return { conclusion, id, status };
}

const mutationModulePath: string =
  "../../scripts/release/cloud-source-check-mutation.mjs";
const { AmbiguousSourceCheckMutationError, createCheckWithReconciliation } =
  (await import(mutationModulePath)) as {
    AmbiguousSourceCheckMutationError: typeof Error;
    createCheckWithReconciliation: (
      options: ReconciliationOptions,
    ) => Promise<Candidate>;
  };

async function reconcileSequence(
  sequence: Scan[],
  retainPendingOnTerminalConflict = false,
) {
  let cleanupCount = 0;
  let listCount = 0;
  const result = await createCheckWithReconciliation({
    cleanupPending: async () => {
      cleanupCount += 1;
    },
    create: async () => {
      throw new Error("mock accepted POST response loss");
    },
    listMatches: async () => {
      const scan = sequence[Math.min(listCount++, sequence.length - 1)] ?? [];
      if (scan instanceof Error) {
        throw scan;
      }
      return scan;
    },
    retainPendingOnTerminalConflict,
    waitFor: async () => {},
  });
  return { cleanupCount, listCount, result };
}

export function registerCloudSourceCheckMutationBehaviorTests(): void {
  describe("Cloud source check mutation reconciliation", () => {
    const failure = reconciliationCandidate(701, "completed", "failure");
    const success = reconciliationCandidate(702, "completed", "success");
    const pending = reconciliationCandidate(900, "in_progress");

    it("retains one conflict fail-safe after terminals appear", async () => {
      const reconciled = await reconcileSequence(
        [[pending], [failure, success, pending], [failure, success, pending]],
        true,
      );
      expect(reconciled).toMatchObject({
        cleanupCount: 0,
        listCount: 3,
        result: pending,
      });
    });

    it("waits for a second terminal before choosing conflict precedence", async () => {
      const reconciled = await reconcileSequence(
        [
          [failure, pending],
          [failure, success, pending],
          [failure, success, pending],
        ],
        true,
      );
      expect(reconciled).toMatchObject({
        cleanupCount: 0,
        listCount: 3,
        result: pending,
      });
    });

    it("rejects terminal conflict during normal pending creation", async () => {
      await expect(
        reconcileSequence([[failure, success, pending], [pending], [pending]]),
      ).rejects.toBeInstanceOf(AmbiguousSourceCheckMutationError);
    });

    it.each([
      [
        "terminal then stale pending",
        [
          [reconciliationCandidate(900, "completed", "success")],
          [pending],
          [pending],
        ],
      ],
      [
        "pending then terminal then stale pending",
        [
          [pending],
          [reconciliationCandidate(900, "completed", "success")],
          [pending],
        ],
      ],
    ] as const)(
      "keeps terminal observation sticky: %s",
      async (_label, scans) => {
        const reconciled = await reconcileSequence(
          scans.map((scan) => [...scan]),
        );
        expect(reconciled).toMatchObject({
          cleanupCount: 1,
          listCount: 3,
          result: reconciliationCandidate(900, "completed", "success"),
        });
      },
    );

    it("retains conflicting conclusions observed for the same terminal ID", async () => {
      const changedFailure = reconciliationCandidate(
        900,
        "completed",
        "failure",
      );
      const changedSuccess = reconciliationCandidate(
        900,
        "completed",
        "success",
      );
      await expect(
        reconcileSequence([
          [changedFailure],
          [changedSuccess],
          [changedSuccess],
        ]),
      ).rejects.toBeInstanceOf(AmbiguousSourceCheckMutationError);
    });

    it("keeps the latest immutable terminal when conclusions agree", async () => {
      const latestFailure = reconciliationCandidate(
        703,
        "completed",
        "failure",
      );
      const reconciled = await reconcileSequence([
        [failure, pending],
        [failure, latestFailure, pending],
        [failure, latestFailure, pending],
      ]);
      expect(reconciled).toMatchObject({
        cleanupCount: 1,
        listCount: 3,
        result: latestFailure,
      });
    });

    it("lets a terminal supersede multiple observed pending checks", async () => {
      const duplicate = reconciliationCandidate(901, "in_progress");
      const reconciled = await reconcileSequence([
        [pending, duplicate],
        [pending, duplicate, success],
        [pending, duplicate, success],
      ]);
      expect(reconciled).toMatchObject({
        cleanupCount: 1,
        listCount: 3,
        result: success,
      });
    });

    it.each([0, 1, 2])(
      "tolerates a transient list failure on scan %i",
      async (failureIndex) => {
        const scans: Scan[] = [[pending], [pending], [pending]];
        scans[failureIndex] = new Error("transient list failure");
        const reconciled = await reconcileSequence(scans);
        expect(reconciled).toMatchObject({
          cleanupCount: 0,
          listCount: 3,
          result: pending,
        });
      },
    );

    it.each([
      ["conflicting terminals", [failure, success]],
      [
        "multiple pending checks",
        [pending, reconciliationCandidate(901, "in_progress")],
      ],
      [
        "conflicting terminals and multiple pending checks",
        [
          failure,
          success,
          pending,
          reconciliationCandidate(901, "in_progress"),
        ],
      ],
    ])("rejects unresolved ambiguity: %s", async (_label, matches) => {
      await expect(reconcileSequence([matches])).rejects.toBeInstanceOf(
        AmbiguousSourceCheckMutationError,
      );
    });
  });
}
