import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

type ScenarioDefinition = {
  id: string;
  prompt: string;
  expect:
    | {
        type: "entity_id_prefix_count";
        prefix: string;
        count: number;
        count_mode?: "exact" | "up_to";
      }
    | {
        type: "json_array_values";
      };
  expected_status?: "pass" | "fail";
  expected_error?: string;
  forbid_patterns?: string[];
  must_contain_text?: string[];
  max_duration_sec?: number;
};

describe("codex skill scenario e2e contract", () => {
  it("provides executable scenario harness", () => {
    const file = "scripts/e2e/codex-ha-nova-scenarios-e2e.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain("codex exec");
    expect(content).toContain("NOVA_SCENARIO_RESULT");
    expect(content).toContain("doctor readiness gate once");
    expect(content).toContain("Scenario suite failed");
    expect(content).toContain("proactive_doctor_or_ready_detected");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("health_preflight_before_ws_detected");
    expect(content).toContain("invalid_jsonl_transcript");
    expect(content).toContain("count_mode");
    expect(content).toContain("expected_status");
    expect(content).toContain("expected_error");
    expect(content).toContain("forbid_patterns");
    expect(content).toContain("must_contain_text");
    expect(content).toContain("expected_status_mismatch");
    expect(content).toContain("expected_error_mismatch");
    expect(content).toContain("forbidden_pattern_detected");
    expect(content).toContain("required_text_missing");
    expect(content).toContain("json_array_values");
  });

  it("ships default scenario definitions", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ScenarioDefinition[];

    expect(Array.isArray(scenarios)).toBe(true);
    expect(scenarios.length).toBeGreaterThan(0);

    for (const scenario of scenarios) {
      expect(typeof scenario.id).toBe("string");
      expect(scenario.id.length).toBeGreaterThan(0);
      expect(typeof scenario.prompt).toBe("string");
      expect(scenario.prompt.length).toBeGreaterThan(0);

      expect(["entity_id_prefix_count", "json_array_values"]).toContain(scenario.expect.type);
      if (scenario.expect.type === "entity_id_prefix_count") {
        expect(typeof scenario.expect.prefix).toBe("string");
        expect(scenario.expect.prefix.endsWith(".")).toBe(true);
        expect(Number.isInteger(scenario.expect.count)).toBe(true);
        expect(scenario.expect.count).toBeGreaterThan(0);
        expect(["exact", "up_to"]).toContain(scenario.expect.count_mode ?? "exact");
      }

      if (typeof scenario.expected_status !== "undefined") {
        expect(["pass", "fail"]).toContain(scenario.expected_status);
      }
      if (typeof scenario.expected_error !== "undefined") {
        expect(typeof scenario.expected_error).toBe("string");
        expect(scenario.expected_error.length).toBeGreaterThan(0);
      }
      if (typeof scenario.forbid_patterns !== "undefined") {
        expect(Array.isArray(scenario.forbid_patterns)).toBe(true);
        expect(scenario.forbid_patterns.length).toBeGreaterThan(0);
        expect(scenario.forbid_patterns.every((pattern) => typeof pattern === "string" && pattern.length > 0)).toBe(
          true
        );
      }
      if (typeof scenario.must_contain_text !== "undefined") {
        expect(Array.isArray(scenario.must_contain_text)).toBe(true);
        expect(scenario.must_contain_text.length).toBeGreaterThan(0);
        expect(scenario.must_contain_text.every((text) => typeof text === "string" && text.length > 0)).toBe(true);
      }
      if (typeof scenario.max_duration_sec !== "undefined") {
        expect(typeof scenario.max_duration_sec).toBe("number");
        expect(scenario.max_duration_sec).toBeGreaterThan(0);
      }
    }
  });

  it("exposes npm command for scenario harness", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["e2e:skill:codex:scenarios"]).toBe(
      "bash scripts/e2e/codex-ha-nova-scenarios-e2e.sh"
    );
  });
});
