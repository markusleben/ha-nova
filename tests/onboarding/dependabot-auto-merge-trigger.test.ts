import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { registerDependabotDirectMergeBehaviorTests } from "./dependabot-direct-merge-behavior.js";

const sha = "a".repeat(40);

function runResolver(
  eventName: "check_run" | "repository_dispatch" | "workflow_run",
  event: unknown,
  appId = 42,
) {
  const directory = mkdtempSync(join(tmpdir(), "ha-nova-trigger-"));
  const eventPath = join(directory, "event.json");
  const outputPath = join(directory, "output");
  const policyPath = join(directory, "policy.json");
  writeFileSync(eventPath, JSON.stringify(event), "utf8");
  writeFileSync(
    policyPath,
    JSON.stringify({
      cloud_source_gate: {
        check_name: "cloud-source-gate",
        reporter_app_id: appId,
        reporter_app_slug: "markusleben-ha-nova-cloud-source-gate",
        synchronous_invalidator_app_id: appId === 0 ? 0 : 43,
        synchronous_invalidator_app_slug:
          "markusleben-ha-nova-cloud-source-invalidator",
        synchronous_invalidator_check_name: "cloud-source-invalidator",
      },
      main_branch_protection: {
        required_status_check_apps: {
          "cloud-source-gate": appId,
        },
      },
    }),
    "utf8",
  );
  const result = spawnSync(
    "node",
    ["scripts/release/resolve-dependabot-auto-merge-trigger.mjs", policyPath],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        GITHUB_EVENT_NAME: eventName,
        GITHUB_EVENT_PATH: eventPath,
        GITHUB_OUTPUT: outputPath,
      },
    },
  );
  return {
    ...result,
    output: result.status === 0 ? readFileSync(outputPath, "utf8") : "",
  };
}

function completedCheck(overrides: Record<string, unknown> = {}) {
  return {
    action: "completed",
    check_run: {
      app: {
        id: 42,
        slug: "markusleben-ha-nova-cloud-source-gate",
      },
      conclusion: "success",
      head_sha: sha,
      name: "cloud-source-gate",
      status: "completed",
      ...overrides,
    },
  };
}

function driftCleanupOwned(author: string, markerActor: string | null) {
  const result = spawnSync(
    "jq",
    [
      "-r",
      "--arg",
      "marker_actor",
      markerActor ?? "",
      '.author.login == "dependabot[bot]" and $marker_actor == "github-actions[bot]"',
    ],
    {
      encoding: "utf8",
      input: JSON.stringify({
        author: { login: author },
      }),
    },
  );
  expect(result.status, result.stderr).toBe(0);
  return result.stdout.trim() === "true";
}

describe("Dependabot auto-merge trigger authentication", () => {
  it("accepts the exact completed dedicated App check", () => {
    const result = runResolver("check_run", completedCheck());
    expect(result.status, result.stderr).toBe(0);
    expect(result.output).toContain("run-kind=check_run");
    expect(result.output).toMatch(/policy-sha=[0-9a-f]{64}/);
    expect(result.output).toContain(`run-sha=${sha}`);
    expect(result.output.trimEnd().endsWith("should-process=true")).toBe(true);
  });

  it("accepts the exact completed synchronous invalidator App check", () => {
    const result = runResolver(
      "check_run",
      completedCheck({
        app: {
          id: 43,
          slug: "markusleben-ha-nova-cloud-source-invalidator",
        },
        name: "cloud-source-invalidator",
      }),
    );
    expect(result.status, result.stderr).toBe(0);
    expect(result.output).toContain("run-kind=check_run");
    expect(result.output.trimEnd().endsWith("should-process=true")).toBe(true);
  });

  it.each([
    ["wrong name", { name: "cloud-source-gate-spoof" }, 42],
    ["wrong App id", { app: { id: 43, slug: "markusleben-ha-nova-cloud-source-gate" } }, 42],
    ["wrong App slug", { app: { id: 42, slug: "spoof" } }, 42],
    ["failed conclusion", { conclusion: "failure" }, 42],
    ["unprovisioned policy", { app: { id: 0, slug: "markusleben-ha-nova-cloud-source-gate" } }, 0],
  ])("ignores %s", (_label, overrides, appId) => {
    const result = runResolver(
      "check_run",
      completedCheck(overrides as Record<string, unknown>),
      appId as number,
    );
    expect(result.status, result.stderr).toBe(0);
    expect(result.output).toContain("should-process=false");
    expect(result.output).not.toContain("should-process=true");
  });

  it("retains successful workflow completion triggers", () => {
    const result = runResolver("workflow_run", {
      action: "completed",
      workflow_run: {
        conclusion: "success",
        head_sha: sha,
        id: 123,
        status: "completed",
      },
    });
    expect(result.status, result.stderr).toBe(0);
    expect(result.output).toContain("run-kind=workflow_run");
    expect(result.output).toContain("run-id=123");
    expect(result.output.trimEnd().endsWith("should-process=true")).toBe(true);
  });

  it("accepts the exact Actions-owned post-marker reevaluation", () => {
    const result = runResolver("repository_dispatch", {
      action: "dependabot-safe-reevaluate",
      client_payload: {
        head_sha: sha,
        pr_number: "7",
      },
      sender: { login: "github-actions[bot]" },
    });
    expect(result.status, result.stderr).toBe(0);
    expect(result.output).toContain("run-kind=repository_dispatch");
    expect(result.output).toContain("run-id=7");
    expect(result.output).toContain(`run-sha=${sha}`);
    expect(result.output.trimEnd().endsWith("should-process=true")).toBe(true);
  });

  it.each([
    ["human sender", "markusleben", "dependabot-safe-reevaluate"],
    ["wrong event type", "github-actions[bot]", "other-event"],
  ])("ignores a repository dispatch from %s", (_label, sender, action) => {
    const result = runResolver("repository_dispatch", {
      action,
      client_payload: {
        head_sha: sha,
        pr_number: "7",
      },
      sender: { login: sender },
    });
    expect(result.status, result.stderr).toBe(0);
    expect(result.output).toContain("should-process=false");
    expect(result.output).not.toContain("should-process=true");
  });

  it.each([
    ["owned Dependabot PR", "dependabot[bot]", "github-actions[bot]", true],
    ["unowned Dependabot PR", "dependabot[bot]", null, false],
    ["human PR with marker", "markusleben", "github-actions[bot]", false],
    ["human PR without marker", "markusleben", null, false],
  ])("limits policy-drift cleanup for %s", (_label, author, markerActor, expected) => {
    expect(driftCleanupOwned(author as string, markerActor as string | null)).toBe(expected);
  });
});

registerDependabotDirectMergeBehaviorTests();
