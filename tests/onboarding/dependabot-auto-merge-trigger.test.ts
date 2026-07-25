import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const sha = "a".repeat(40);

function runResolver(
  eventName: "check_run" | "workflow_run",
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
        reporter_app_slug: "markusleben-ha-nova-cloud-source-gate",
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

function driftCleanupEligible(author: string, labels: string[]) {
  const result = spawnSync(
    "jq",
    [
      "-r",
      "--arg",
      "safe_label",
      "dependabot-safe:auto-merge",
      '.author.login == "dependabot[bot]" and any(.labels[]?; .name == $safe_label)',
    ],
    {
      encoding: "utf8",
      input: JSON.stringify({
        author: { login: author },
        labels: labels.map((name) => ({ name })),
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

  it.each([
    ["wrong name", { name: "cloud-source-gate-spoof" }, 42],
    [
      "wrong App id",
      { app: { id: 43, slug: "markusleben-ha-nova-cloud-source-gate" } },
      42,
    ],
    ["wrong App slug", { app: { id: 42, slug: "spoof" } }, 42],
    ["failed conclusion", { conclusion: "failure" }, 42],
    [
      "unprovisioned policy",
      { app: { id: 0, slug: "markusleben-ha-nova-cloud-source-gate" } },
      0,
    ],
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

  it.each([
    ["labeled Dependabot PR", "dependabot[bot]", ["dependabot-safe:auto-merge"], true],
    ["unlabeled Dependabot PR", "dependabot[bot]", [], false],
    ["labeled human PR", "markusleben", ["dependabot-safe:auto-merge"], false],
    ["unlabeled human PR", "markusleben", [], false],
  ])(
    "limits policy-drift cleanup for %s",
    (_label, author, labels, expected) => {
      expect(
        driftCleanupEligible(
          author as string,
          labels as string[],
        ),
      ).toBe(expected);
    },
  );
});
