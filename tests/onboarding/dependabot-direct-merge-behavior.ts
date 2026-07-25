import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const baseSHA = "b".repeat(40);
const driftSHA = "d".repeat(40);
const headSHA = "a".repeat(40);
const mergeSHA = "c".repeat(40);
const safeLabel = "dependabot-safe:auto-merge";
const marker = "<!-- ha-nova-dependabot-safe-policy -->";

function policy(activated = true) {
  return {
    cloud_source_gate: {
      check_name: "cloud-source-gate",
      reporter_app_id: activated ? 42 : 0,
      synchronous_invalidator_app_id: activated ? 43 : 0,
      synchronous_invalidator_check_name: "cloud-source-invalidator",
    },
    dependabot_safe_lane: { label: safeLabel },
    main_branch_protection: {
      required_status_check_apps: activated
        ? {
            "cloud-source-gate": 42,
            "cloud-source-invalidator": 43,
          }
        : {},
      required_status_checks: activated
        ? ["ci-gate", "cloud-source-gate", "cloud-source-invalidator"]
        : ["ci-gate"],
    },
  };
}

function runDirectMerge(options: {
  activeCI?: boolean;
  activeCIPage2?: boolean;
  activated?: boolean;
  comments?: unknown[];
  currentRef?: string;
  labels?: string[];
  liveStrict?: boolean;
  markerPage?: number;
  invalidatorTarget?: string;
  sourceTarget?: string;
  timeline?: unknown[];
}) {
  const directory = mkdtempSync(join(tmpdir(), "ha-nova-direct-merge-"));
  const preloadPath = join(directory, "mock-fetch.mjs");
  const tracePath = join(directory, "trace.jsonl");
  const activated = options.activated ?? true;
  const policyBytes = Buffer.from(
    `${JSON.stringify(policy(activated), null, 2)}\n`,
  );
  const policySHA = createHash("sha256").update(policyBytes).digest("hex");
  const policyComment = {
    body: `${marker}\nsafe_label=${safeLabel}\npolicy_sha=${policySHA}\n`,
    id: 90,
    user: { login: "github-actions[bot]" },
  };
  const comments =
    options.comments ??
    (options.markerPage === 2
      ? Array.from({ length: 100 }, (_, id) => ({
          body: `ordinary ${id}`,
          id,
          user: { login: "markusleben" },
        })).concat(policyComment)
      : [policyComment]);
  const timeline = options.timeline ?? [
    {
      actor: { login: "github-actions[bot]" },
      event: "labeled",
      label: { name: safeLabel },
    },
  ];
  const pr = {
    auto_merge: {
      enabled_by: { login: "github-actions[bot]" },
    },
    base: {
      ref: "main",
      repo: { full_name: "markusleben/ha-nova" },
      sha: options.currentRef ?? baseSHA,
    },
    draft: false,
    head: { sha: headSHA },
    labels: (options.labels ?? [safeLabel]).map((name) => ({ name })),
    merge_commit_sha: mergeSHA,
    node_id: "PR_node",
    number: 7,
    state: "open",
    user: { login: "dependabot[bot]" },
  };
  const checks = [
    {
      app: { id: 1 },
      conclusion: "success",
      id: 10,
      name: "ci-gate",
      status: "completed",
    },
    {
      app: { id: 42 },
      conclusion: "success",
      external_id: `workflow-run:123:attempt:1:target:${options.sourceTarget ?? mergeSHA}`,
      id: 11,
      name: "cloud-source-gate",
      status: "completed",
    },
    {
      app: { id: 43 },
      conclusion: "success",
      external_id: `pull-request:7:target:${options.invalidatorTarget ?? mergeSHA}`,
      id: 12,
      name: "cloud-source-invalidator",
      status: "completed",
    },
  ];
  writeFileSync(
    preloadPath,
    `import { appendFileSync } from "node:fs";
const policy = ${JSON.stringify(policyBytes.toString("base64"))};
const pr = ${JSON.stringify(pr)};
const comments = ${JSON.stringify(comments)};
const timeline = ${JSON.stringify(timeline)};
const checks = ${JSON.stringify(checks)};
const currentRef = ${JSON.stringify(options.currentRef ?? baseSHA)};
const activeCI = ${JSON.stringify(options.activeCI ?? false)};
const activeCIPage2 = ${JSON.stringify(options.activeCIPage2 ?? false)};
const activated = ${JSON.stringify(activated)};
const liveStrict = ${JSON.stringify(options.liveStrict ?? true)};
function response(data, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => data };
}
globalThis.fetch = async (url, init = {}) => {
  const parsed = new URL(url);
  const path = parsed.pathname;
  const method = init.method ?? "GET";
  const body = init.body === undefined ? null : JSON.parse(init.body);
  appendFileSync(process.env.MOCK_TRACE, JSON.stringify({ body, method, path, search: parsed.search }) + "\\n");
  if (path.endsWith("/contents/.github/policy/repo-policy.json")) {
    return response({ content: policy, encoding: "base64" });
  }
  if (path.endsWith("/branches/main")) return response({ commit: { sha: currentRef } });
  if (path.endsWith("/branches/main/protection")) {
    return response({ required_status_checks: {
      checks: [
        { app_id: null, context: "ci-gate" },
        ...(activated ? [
          { app_id: 42, context: "cloud-source-gate" },
          { app_id: 43, context: "cloud-source-invalidator" }
        ] : [])
      ],
      strict: liveStrict
    } });
  }
  if (path.endsWith("/actions/runs/123")) return response({ pull_requests: [{ number: 7 }] });
  if (path.endsWith("/pulls/7") && method === "GET") return response(pr);
  if (path.endsWith("/issues/7/comments")) {
    const page = Number(parsed.searchParams.get("page"));
    return response(page === 1 ? comments.slice(0, 100) : comments.slice(100, 200));
  }
  if (path.endsWith("/issues/7/timeline")) {
    const page = Number(parsed.searchParams.get("page"));
    return response(page === 1 ? timeline.slice(0, 100) : timeline.slice(100, 200));
  }
  if (path.endsWith("/git/ref/pull/7/merge")) return response({ object: { sha: ${JSON.stringify(mergeSHA)} } });
  if (path.endsWith("/commits/${headSHA}/check-runs")) {
    return response({ check_runs: checks, total_count: checks.length });
  }
  if (path.endsWith("/actions/workflows/ci.yml/runs")) {
    const page = Number(parsed.searchParams.get("page"));
    if (activeCIPage2) {
      const completed = Array.from({ length: 100 }, (_, id) => ({
        head_repository: { full_name: "markusleben/ha-nova" },
        head_sha: ${JSON.stringify(headSHA)},
        id,
        status: "completed"
      }));
      const running = [{
        head_repository: { full_name: "markusleben/ha-nova" },
        head_sha: ${JSON.stringify(headSHA)},
        id: 101,
        status: "in_progress"
      }];
      return response({
        total_count: 101,
        workflow_runs: page === 1 ? completed : running
      });
    }
    const workflow_runs = activeCI ? [{
      head_repository: { full_name: "markusleben/ha-nova" },
      head_sha: ${JSON.stringify(headSHA)},
      status: "in_progress"
    }] : [];
    return response({ total_count: workflow_runs.length, workflow_runs });
  }
  if (path.endsWith("/graphql")) return response({ data: {} });
  if (path.endsWith("/pulls/7/merge") && method === "PUT") return response({ merged: true });
  if (method === "DELETE") return response(undefined, 204);
  return response({ message: "unexpected request" }, 500);
};
`,
    "utf8",
  );
  const result = spawnSync(
    process.execPath,
    ["--import", preloadPath, "scripts/release/merge-safe-dependabot-pr.mjs"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        AUTHENTICATED_POLICY_REF: baseSHA,
        DEFAULT_BRANCH: "main",
        EXPECTED_POLICY_SHA: policySHA,
        GH_TOKEN: "github-token-at-least-twenty-characters",
        GITHUB_REPOSITORY: "markusleben/ha-nova",
        MOCK_TRACE: tracePath,
        RUN_ID: "123",
        RUN_KIND: "workflow_run",
        RUN_SHA: headSHA,
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
          search: string;
        },
    );
  return { result, trace };
}

export function registerDependabotDirectMergeBehaviorTests(): void {
  describe("Dependabot exact-target direct merge", () => {
    it("disables queued auto-merge and directly squash-merges only after two validations", () => {
      const { result, trace } = runDirectMerge({});
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.filter((entry) => entry.path.endsWith("/graphql")),
      ).toHaveLength(1);
      const merge = trace.find(
        (entry) =>
          entry.method === "PUT" && entry.path.endsWith("/pulls/7/merge"),
      );
      expect(merge?.body).toEqual({ merge_method: "squash", sha: headSHA });
      expect(
        trace.filter((entry) =>
          entry.path.endsWith(`/commits/${headSHA}/check-runs`),
        ),
      ).toHaveLength(2);
    });

    it("keeps disabled routine checks free of Cloud App blockers after strict rollout", () => {
      const { result, trace } = runDirectMerge({ activated: false });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "PUT" && entry.path.endsWith("/pulls/7/merge"),
        ),
      ).toBe(true);
    });

    it("rejects direct REST merge while live protection is non-strict", () => {
      const { result, trace } = runDirectMerge({
        activated: false,
        liveStrict: false,
      });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "live required status checks are not strict",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PUT" && entry.path.endsWith("/pulls/7/merge"),
        ),
      ).toBe(false);
    });

    it("rejects a latest source success for another synthetic merge target", () => {
      const { result, trace } = runDirectMerge({ sourceTarget: driftSHA });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "latest dedicated-App source check targets another merge commit",
      );
      expect(
        trace.some(
          (entry) =>
            entry.method === "PUT" && entry.path.endsWith("/pulls/7/merge"),
        ),
      ).toBe(false);
    });

    it("rejects an invalidator success for another synthetic merge target", () => {
      const { result } = runDirectMerge({ invalidatorTarget: driftSHA });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "latest dedicated-App invalidator check targets another merge commit",
      );
    });

    it("rejects direct merge while any current-head CI run remains active", () => {
      const { result } = runDirectMerge({ activeCI: true });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "a CI run is queued or in progress for the candidate head",
      );
    });

    it("finds an active CI run beyond the first 100 workflow runs", () => {
      const { result } = runDirectMerge({ activeCIPage2: true });
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "a CI run is queued or in progress for the candidate head",
      );
    });

    it("finds an authenticated marker beyond the first 100 comments", () => {
      const { result } = runDirectMerge({ markerPage: 2 });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    });

    it("cleans unlabeled owned native auto-merge on policy-ref drift", () => {
      const { result, trace } = runDirectMerge({
        currentRef: driftSHA,
        labels: [],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.some((entry) => entry.path.endsWith("/graphql"))).toBe(true);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith("/issues/comments/90"),
        ),
      ).toBe(true);
      expect(trace.some((entry) => entry.path.includes("/labels/"))).toBe(
        false,
      );
    });

    it("removes an owned safe label during policy-ref drift", () => {
      const { result, trace } = runDirectMerge({
        currentRef: driftSHA,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(
        trace.some(
          (entry) =>
            entry.method === "DELETE" &&
            entry.path.endsWith(
              "/issues/7/labels/dependabot-safe%3Aauto-merge",
            ),
        ),
      ).toBe(true);
    });

    it("uses paginated bot-label history when the owned marker was deleted", () => {
      const history: unknown[] = [
        ...Array.from({ length: 100 }, (_, id) => ({
          actor: { login: "markusleben" },
          event: "commented",
          id,
        })),
        {
          actor: { login: "github-actions[bot]" },
          event: "labeled",
          label: { name: safeLabel },
        },
      ];
      const { result, trace } = runDirectMerge({
        comments: [],
        currentRef: driftSHA,
        labels: [],
        timeline: history,
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.some((entry) => entry.path.endsWith("/graphql"))).toBe(true);
    });

    it("leaves unowned Dependabot state untouched during policy drift", () => {
      const { result, trace } = runDirectMerge({
        comments: [],
        currentRef: driftSHA,
        labels: [],
        timeline: [],
      });
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
      expect(trace.some((entry) => entry.path.endsWith("/graphql"))).toBe(
        false,
      );
      expect(trace.some((entry) => entry.method === "DELETE")).toBe(false);
    });
  });
}
