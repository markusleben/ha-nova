import { execFileSync } from "node:child_process";
import { constants, existsSync, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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
  must_not_contain_text?: string[];
  max_duration_sec?: number;
};

function extractShellFunction(content: string, name: string): string {
  const match = content.match(new RegExp(String.raw`${name}\(\)\s*\{[\s\S]*?\n\}`, "m"));

  if (!match) {
    throw new Error(`shell function not found: ${name}`);
  }

  return match[0] ?? "";
}

describe("codex skill scenario e2e contract", () => {
  it("provides executable scenario harness", () => {
    const file = "scripts/e2e/codex-ha-nova-scenarios-e2e.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain('"codex"');
    expect(content).toContain('"exec"');
    expect(content).toContain("run_codex_with_timeout");
    expect(content).toContain("wait_for_log_completion");
    expect(content).toContain("NOVA_SCENARIO_RESULT");
    expect(content).toContain("doctor readiness gate once");
    expect(content).toContain("Scenario suite failed");
    expect(content).toContain("proactive_doctor_or_ready_detected");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("health_preflight_before_ws_detected");
    expect(content).toContain("invalid_jsonl_transcript");
    expect(content).toContain("incomplete_transcript");
    expect(content).toContain("status_line_not_final");
    expect(content).toContain("trailing_events_after_final_message");
    expect(content).toContain("count_mode");
    expect(content).toContain("expected_status");
    expect(content).toContain("expected_error");
    expect(content).toContain("forbid_patterns");
    expect(content).toContain("must_contain_text");
    expect(content).toContain("must_not_contain_text");
    expect(content).toContain("expected_status_mismatch");
    expect(content).toContain("expected_error_mismatch");
    expect(content).toContain("forbidden_pattern_detected");
    expect(content).toContain("required_text_missing");
    expect(content).toContain("forbidden_text_present");
    expect(content).toContain("json_array_values");
    expect(content).toMatch(/if \[\[ "\$status" == "pass" \]\]; then\s+while IFS= read -r forbidden_text;/);
    expect(content).toContain('normalized_tmp="${parsed_log}.tmp"');
    expect(content).toContain('normalize_jsonl_transcript "$scenario_log" >"$normalized_tmp"');
    expect(content).toContain('mv "$normalized_tmp" "$parsed_log"');
    expect(content).toContain('rm -f "$normalized_tmp" "$parsed_log"');
    expect(content).not.toContain('normalize_jsonl_transcript "$scenario_log" >"$parsed_log" || true');
    expect(content).toContain('(\\./)?scripts/(smoke|e2e|dev)/');
    expect(content).toContain('bash|sh|zsh|python3?|node|bunx?|bun|tsx');
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
      if (typeof scenario.must_not_contain_text !== "undefined") {
        expect(Array.isArray(scenario.must_not_contain_text)).toBe(true);
        expect(scenario.must_not_contain_text.length).toBeGreaterThan(0);
        expect(scenario.must_not_contain_text.every((text) => typeof text === "string" && text.length > 0)).toBe(
          true
        );
      }
      if (typeof scenario.max_duration_sec !== "undefined") {
        expect(typeof scenario.max_duration_sec).toBe("number");
        expect(scenario.max_duration_sec).toBeGreaterThan(0);
      }
    }
  });

  it("ships focused R-18 wording scenarios without changing the scenario schema", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ScenarioDefinition[];

    const byId = new Map(scenarios.map((scenario) => [scenario.id, scenario]));

    const topLevel = byId.get("r18-draft-risk-top-level");
    expect(topLevel).toBeDefined();
    expect(topLevel?.expect.type).toBe("json_array_values");
    expect(topLevel?.must_contain_text).toContain("REST/UI write can break dependent variables in this block");
    expect(topLevel?.must_contain_text).toContain("check_flag -> reading");

    const localBlock = byId.get("r18-draft-risk-local-block");
    expect(localBlock).toBeDefined();
    expect(localBlock?.expect.type).toBe("json_array_values");
    expect(localBlock?.must_contain_text).toContain("local variables block");
    expect(localBlock?.must_contain_text).toContain("check_flag -> reading");

    const fragileAlpha = byId.get("r18-fragile-alpha-order");
    expect(fragileAlpha).toBeDefined();
    expect(fragileAlpha?.expect.type).toBe("json_array_values");
    expect(fragileAlpha?.must_contain_text).toContain("REST/UI write can break dependent variables in this block");
    expect(fragileAlpha?.must_contain_text).toContain("fragile alphabetical order");
    expect(fragileAlpha?.must_contain_text).toContain("check_flag -> reading");

    const safeAlpha = byId.get("r18-safe-alpha-order");
    expect(safeAlpha).toBeDefined();
    expect(safeAlpha?.expect.type).toBe("json_array_values");
    expect(safeAlpha?.must_contain_text).toContain("No R-18 risk detected");
    expect(safeAlpha?.must_contain_text).toContain("safe alphabetical order");
    expect(safeAlpha?.prompt).toContain('a_reading: "{{ states(\'sensor.flow_rate\') | float(-999) }}"');
    expect(safeAlpha?.prompt).toContain('check_flag: "{{ a_reading > -998 }}"');

    const safeSet = byId.get("r18-safe-set-local");
    expect(safeSet).toBeDefined();
    expect(safeSet?.expect.type).toBe("json_array_values");
    expect(safeSet?.must_contain_text).toContain("No R-18 risk detected");
    expect(safeSet?.must_contain_text).toContain("self-contained template");

    const safeActions = byId.get("r18-safe-ordered-actions");
    expect(safeActions).toBeDefined();
    expect(safeActions?.expect.type).toBe("json_array_values");
    expect(safeActions?.must_contain_text).toContain("No R-18 risk detected");
    expect(safeActions?.must_contain_text).toContain("ordered variables actions");
  });

  it("ships focused R-19 wording scenarios without changing the scenario schema", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ScenarioDefinition[];

    const byId = new Map(scenarios.map((scenario) => [scenario.id, scenario]));

    const flagged = byId.get("r19-bare-else-trigger-flagged");
    expect(flagged).toBeDefined();
    expect(flagged?.expect.type).toBe("json_array_values");
    expect(flagged?.must_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(flagged?.must_contain_text).toContain("Move the `trigger.id` check into an explicit `elif`");
    expect(flagged?.must_contain_text).toContain("Or refactor to `choose` + `condition: trigger`");

    const safe = byId.get("r19-safe-non-state-selector-tree");
    expect(safe).toBeDefined();
    expect(safe?.expect.type).toBe("json_array_values");
    expect(safe?.must_contain_text).toContain("No R-19 risk detected");
    expect(safe?.must_contain_text).toContain("mode selector tree");
    expect(safe?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const numericTree = byId.get("r19-safe-numeric-range-selector-tree");
    expect(numericTree).toBeDefined();
    expect(numericTree?.expect.type).toBe("json_array_values");
    expect(numericTree?.must_contain_text).toContain("No R-19 risk detected");
    expect(numericTree?.must_contain_text).toContain("numeric range selector tree");
    expect(numericTree?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const timeTree = byId.get("r19-safe-time-window-selector-tree");
    expect(timeTree).toBeDefined();
    expect(timeTree?.expect.type).toBe("json_array_values");
    expect(timeTree?.must_contain_text).toContain("No R-19 risk detected");
    expect(timeTree?.must_contain_text).toContain("time window selector tree");
    expect(timeTree?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const singleIfElse = byId.get("r19-safe-single-if-else");
    expect(singleIfElse).toBeDefined();
    expect(singleIfElse?.expect.type).toBe("json_array_values");
    expect(singleIfElse?.must_contain_text).toContain("No R-19 risk detected");
    expect(singleIfElse?.must_contain_text).toContain("single if else");
    expect(singleIfElse?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const explicitElif = byId.get("r19-safe-trigger-id-elif");
    expect(explicitElif).toBeDefined();
    expect(explicitElif?.expect.type).toBe("json_array_values");
    expect(explicitElif?.must_contain_text).toContain("No R-19 risk detected");
    expect(explicitElif?.must_contain_text).toContain("explicit elif trigger id");
    expect(explicitElif?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const extraGuard = byId.get("r19-safe-else-extra-guard");
    expect(extraGuard).toBeDefined();
    expect(extraGuard?.expect.type).toBe("json_array_values");
    expect(extraGuard?.must_contain_text).toContain("No R-19 risk detected");
    expect(extraGuard?.must_contain_text).toContain("else extra guard");
    expect(extraGuard?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const chooseTrigger = byId.get("r19-safe-choose-condition-trigger");
    expect(chooseTrigger).toBeDefined();
    expect(chooseTrigger?.expect.type).toBe("json_array_values");
    expect(chooseTrigger?.must_contain_text).toContain("No R-19 risk detected");
    expect(chooseTrigger?.must_contain_text).toContain("choose condition trigger routing");
    expect(chooseTrigger?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
  });

  it("proves malformed and non-object JSONL transcripts fail atomically without leaving partial parsed output", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");
    const normalizeFn = extractShellFunction(content, "normalize_jsonl_transcript");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-scenarios-jsonl-"));
    const runAtomicNormalization = (name: string, invalidLine: string) => {
      const scenarioLog = join(tempDir, `${name}.jsonl`);
      const parsedLog = join(tempDir, `${name}.parsed.jsonl`);

      writeFileSync(
        scenarioLog,
        `${JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "ok" } })}\n${invalidLine}\n`
      );

      let failure: unknown;
      try {
        execFileSync(
          "bash",
          [
            "-lc",
            `set -euo pipefail
${normalizeFn}
scenario_log="$1"
parsed_log="$2"
normalized_tmp="\${parsed_log}.tmp"
rm -f "$normalized_tmp" "$parsed_log"
if normalize_jsonl_transcript "$scenario_log" >"$normalized_tmp"; then
  mv "$normalized_tmp" "$parsed_log"
else
  rm -f "$normalized_tmp" "$parsed_log"
  exit 99
fi`,
            "bash",
            scenarioLog,
            parsedLog,
          ],
          { encoding: "utf8", stdio: "pipe" }
        );
      } catch (error) {
        failure = error;
      }

      expect(failure).toMatchObject({ status: 99 });
      expect(existsSync(parsedLog)).toBe(false);
      expect(existsSync(`${parsedLog}.tmp`)).toBe(false);
    };

    runAtomicNormalization("malformed", '{"bad":');
    runAtomicNormalization("non-object", "[]");
  });

  it("detects direct and zsh-prefixed helper script escapes", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");
    const countCommandHitsFn = extractShellFunction(content, "count_command_hits");
    const countHelperScriptExecHitsFn = extractShellFunction(content, "count_helper_script_exec_hits");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-scenarios-helper-"));
    const parsedLog = join(tempDir, "parsed.jsonl");

    writeFileSync(
      parsedLog,
      [
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: "./scripts/e2e/demo.sh" },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: "zsh scripts/dev/demo.sh" },
        }),
      ].join("\n") + "\n"
    );

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
${countCommandHitsFn}
${countHelperScriptExecHitsFn}
count_helper_script_exec_hits "$1"`,
        "bash",
        parsedLog,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("2");
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
