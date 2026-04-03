import { execFileSync } from "node:child_process";
import { constants, existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
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
    expect(content).toContain("should_retry_empty_start_timeout");
    expect(content).toContain('log "Retrying ${scenario_id} after empty startup timeout"');
    expect(content).toContain('start_ts="$(date +%s)"');
    expect(content).toContain("wait_for_log_completion");
    expect(content).toContain("NOVA_SCENARIO_RESULT");
    expect(content).toContain("Use the repo-local HA NOVA workflow from this checkout for this task.");
    expect(content).toContain("Treat pasted-YAML prompts as local review tasks");
    expect(content).toContain("Use only repo-local files from this checkout plus the pasted YAML in the prompt.");
    expect(content).toContain("Never read installed skill copies from ~/.local/share/ha-nova/skills");
    expect(content).toContain("Do not browse the web and do not use Exa, Ref, web search, or official-doc lookup tools.");
    expect(content).toContain("Treat the local repo skill guidance as authoritative for this harness even if you feel uncertain.");
    expect(content).toContain("If a conclusion would require external docs, state the uncertainty from local context instead of researching.");
    expect(content).toContain('if [[ "$expect_type" == "entity_id_prefix_count" ]]; then');
    expect(content).toContain("Fast path for this scenario:");
    expect(content).toContain("Do not inspect unrelated repo files, test harness files, package.json, install scripts, or other project metadata.");
    expect(content).toContain("If you need repo guidance, stop at skills/ha-nova/SKILL.md and skills/ha-nova/relay-api.md.");
    expect(content).toContain("Do not read scripts/e2e/*.sh for this request.");
    expect(content).toContain("Use one relay ws call against config/entity_registry/list_for_display and filter the result directly.");
    expect(content).toContain('elif [[ "$expect_type" == "json_array_values" ]]; then');
    expect(content).toContain("Minimal local-review path for this scenario:");
    expect(content).toContain("If you need local references, read each of these at most once: skills/ha-nova/SKILL.md, skills/review/SKILL.md, skills/review/checks.md, docs/reference/ha-template-reference.md.");
    expect(content).toContain("Do not run repo-wide follow-up searches, excerpt hunts, package inspection, or additional discovery commands after reading those references.");
    expect(content).toContain('if [[ "$expected_error" == "proactive_doctor_or_ready_detected" ]]; then');
    expect(content).toContain("This scenario intentionally expects one prohibited proactive doctor/ready/quick check.");
    expect(content).toContain("Run exactly one doctor/ready/quick command before the first relay call, then do one minimal relay ws inventory read and finish.");
    expect(content).toContain('elif [[ "$expected_error" == "health_preflight_before_ws_detected" ]]; then');
    expect(content).toContain("This scenario intentionally expects one prohibited relay health preflight.");
    expect(content).toContain("Run exactly one relay health command before the first relay ws/core action, then do one minimal relay ws inventory read and finish.");
    expect(content).toContain("Never include internal rule codes such as R-18");
    expect(content).toContain("Do not emit interim progress updates, evidence-loading notes, or meta narration.");
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
    expect(content).toContain("rule_code_marker_present");
    expect(content).toContain("json_array_values");
    expect(content).toContain('Reading additional input from stdin...');
    expect(content).toContain("ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed");
    expect(content).toContain("ERROR codex_api::endpoint::responses_websocket: failed to connect to websocket:");
    expect(content).toContain("CLI_CMD_PATTERN='([[:alnum:]_./-]*ha-nova|[.]/cli/cli|cli/cli)'");
    expect(content).toContain('CLI_DOCTOR_PATTERN="${CLI_CMD_PATTERN}([[:space:]]+[[:alnum:]_./-]+){0,2}[[:space:]]+(doctor|ready|quick)"');
    expect(content).toContain("GO_RUN_PATTERN='go[[:space:]]+run([[:space:]]+[[:alnum:]_./-]+){1,4}'");
    expect(content).toContain('RELAY_HEALTH_PATTERN="((^|[[:space:][:punct:]])(${CLI_CMD_PATTERN}|${GO_RUN_PATTERN})[[:space:]]+relay[[:space:]]+health([[:space:][:punct:]]|$))|/health"');
    expect(content).toContain('${CLI_DOCTOR_PATTERN}');
    expect(content).toContain('$RELAY_HEALTH_PATTERN');
    expect(content).toContain("[[:space:][:punct:]]");
    expect(content).toContain("relay[[:space:]]+(ws|core)");
    expect(content).toContain('ws_action_idx="$(first_command_index "$parsed_log"');
    expect(content).toContain('ha_action_idx="$(first_command_index "$parsed_log"');
    expect(content).toContain("relay[[:space:]]+health");
    expect(content).toContain("health_before_action");
    expect(content).toMatch(/if \[\[ "\$status" == "pass" \]\]; then\s+while IFS= read -r forbidden_text;/);
    expect(content).toContain('normalized_tmp="${parsed_log}.tmp"');
    expect(content).toContain('normalize_jsonl_transcript "$scenario_log" >"$normalized_tmp"');
    expect(content).toContain('mv "$normalized_tmp" "$parsed_log"');
    expect(content).toContain('rm -f "$normalized_tmp" "$parsed_log"');
    expect(content).not.toContain('normalize_jsonl_transcript "$scenario_log" >"$parsed_log" || true');
    expect(content).toContain("direct_re = re.compile");
    expect(content).toContain("scripts/(?:smoke|e2e|dev)/\\S+");
    expect(content).toContain("shell_re = re.compile");
    expect(content).toContain("bash|sh|zsh|python3?|node|bunx?|bun|tsx");
    expect(content).toContain('if [[ ! -f "$scenario_log" ]]; then');
    expect(content).toContain("echo 0");
    expect(content).toContain("return 0");
  });

  it("resets the scenario timer before rerunning after an empty startup timeout", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");

    expect(content).toMatch(
      /if \[\[ "\$status" == "pass" && "\$duration_sec" -gt "\$max_duration_sec" \]\]; then[\s\S]*?status="fail"/
    );
    expect(content).toMatch(
      /if should_retry_empty_start_timeout "\$codex_status" "\$scenario_log"; then[\s\S]*?rm -f "\$scenario_log"[\s\S]*?start_ts="\$\(date \+%s\)"[\s\S]*?run_codex_with_timeout/
    );
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
    expect(topLevel?.must_not_contain_text).toEqual(expect.arrayContaining(["R-18", "R18"]));

    const localBlock = byId.get("r18-draft-risk-local-block");
    expect(localBlock).toBeDefined();
    expect(localBlock?.expect.type).toBe("json_array_values");
    expect(localBlock?.must_contain_text).toContain("local variables block");
    expect(localBlock?.must_contain_text).toContain("check_flag -> reading");
    expect(localBlock?.must_not_contain_text).toEqual(expect.arrayContaining(["R-18", "R18"]));

    const fragileAlpha = byId.get("r18-fragile-alpha-order");
    expect(fragileAlpha).toBeDefined();
    expect(fragileAlpha?.expect.type).toBe("json_array_values");
    expect(fragileAlpha?.must_contain_text).toContain("REST/UI write can break dependent variables in this block");
    expect(fragileAlpha?.must_contain_text).toContain("fragile alphabetical order");
    expect(fragileAlpha?.must_contain_text).toContain("check_flag -> reading");
    expect(fragileAlpha?.must_not_contain_text).toEqual(expect.arrayContaining(["R-18", "R18"]));

    const safeAlpha = byId.get("r18-safe-alpha-order");
    expect(safeAlpha).toBeDefined();
    expect(safeAlpha?.expect.type).toBe("json_array_values");
    expect(safeAlpha?.must_contain_text).toContain("No issues found in this review.");
    expect(safeAlpha?.must_contain_text).toContain("Safe pattern: safe alphabetical order.");
    expect(safeAlpha?.must_not_contain_text).toEqual(expect.arrayContaining(["R-18", "R18"]));
    expect(safeAlpha?.prompt).toContain('a_reading: "{{ states(\'sensor.flow_rate\') | float(-999) }}"');
    expect(safeAlpha?.prompt).toContain('check_flag: "{{ a_reading > -998 }}"');

    const safeSet = byId.get("r18-safe-set-local");
    expect(safeSet).toBeDefined();
    expect(safeSet?.expect.type).toBe("json_array_values");
    expect(safeSet?.must_contain_text).toContain("No issues found in this review.");
    expect(safeSet?.must_contain_text).toContain("Safe pattern: self-contained template.");
    expect(safeSet?.must_not_contain_text).toEqual(expect.arrayContaining(["R-18", "R18"]));

    const safeActions = byId.get("r18-safe-ordered-actions");
    expect(safeActions).toBeDefined();
    expect(safeActions?.expect.type).toBe("json_array_values");
    expect(safeActions?.must_contain_text).toContain("No issues found in this review.");
    expect(safeActions?.must_contain_text).toContain("Safe pattern: ordered variables actions.");
    expect(safeActions?.must_not_contain_text).toEqual(expect.arrayContaining(["R-18", "R18"]));
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
    expect(flagged?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const safe = byId.get("r19-safe-non-state-selector-tree");
    expect(safe).toBeDefined();
    expect(safe?.expect.type).toBe("json_array_values");
    expect(safe?.must_contain_text).toContain("No issues found in this review.");
    expect(safe?.must_contain_text).toContain("Safe pattern: mode selector tree.");
    expect(safe?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(safe?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const numericTree = byId.get("r19-safe-numeric-range-selector-tree");
    expect(numericTree).toBeDefined();
    expect(numericTree?.expect.type).toBe("json_array_values");
    expect(numericTree?.must_contain_text).toContain("No issues found in this review.");
    expect(numericTree?.must_contain_text).toContain("Safe pattern: numeric range selector tree.");
    expect(numericTree?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(numericTree?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const timeTree = byId.get("r19-safe-time-window-selector-tree");
    expect(timeTree).toBeDefined();
    expect(timeTree?.expect.type).toBe("json_array_values");
    expect(timeTree?.must_contain_text).toContain("No issues found in this review.");
    expect(timeTree?.must_contain_text).toContain("Safe pattern: time window selector tree.");
    expect(timeTree?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(timeTree?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const singleIfElse = byId.get("r19-safe-single-if-else");
    expect(singleIfElse).toBeDefined();
    expect(singleIfElse?.expect.type).toBe("json_array_values");
    expect(singleIfElse?.must_contain_text).toContain("No issues found in this review.");
    expect(singleIfElse?.must_contain_text).toContain("Safe pattern: single if else.");
    expect(singleIfElse?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(singleIfElse?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const explicitElif = byId.get("r19-safe-trigger-id-elif");
    expect(explicitElif).toBeDefined();
    expect(explicitElif?.expect.type).toBe("json_array_values");
    expect(explicitElif?.must_contain_text).toContain("No issues found in this review.");
    expect(explicitElif?.must_contain_text).toContain("Safe pattern: explicit elif trigger id.");
    expect(explicitElif?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(explicitElif?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const extraGuard = byId.get("r19-safe-else-extra-guard");
    expect(extraGuard).toBeDefined();
    expect(extraGuard?.expect.type).toBe("json_array_values");
    expect(extraGuard?.must_contain_text).toContain("No issues found in this review.");
    expect(extraGuard?.must_contain_text).toContain("Safe pattern: else extra guard.");
    expect(extraGuard?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(extraGuard?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const chooseTrigger = byId.get("r19-safe-choose-condition-trigger");
    expect(chooseTrigger).toBeDefined();
    expect(chooseTrigger?.expect.type).toBe("json_array_values");
    expect(chooseTrigger?.must_contain_text).toContain("No issues found in this review.");
    expect(chooseTrigger?.must_contain_text).toContain("Safe pattern: choose condition trigger routing.");
    expect(chooseTrigger?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(chooseTrigger?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));
  });

  it("keeps user-facing scenario expectations free of rule-code markers", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ScenarioDefinition[];

    const userFacingText = scenarios.flatMap((scenario) => scenario.must_contain_text ?? []);

    for (const text of userFacingText) {
      expect(text).not.toMatch(/\b(?:S|R|P|M|F|H)-\d{2}\b/);
      expect(text).not.toMatch(/\bR\d+\b/);
    }
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

  it("ignores the codex stdin preamble line during JSONL normalization", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");
    const normalizeFn = extractShellFunction(content, "normalize_jsonl_transcript");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-scenarios-jsonl-preamble-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");

    writeFileSync(
      scenarioLog,
      `Reading additional input from stdin...\n${JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "ok" } })}\n`
    );

    const normalized = execFileSync(
      "bash",
      ["-lc", `set -euo pipefail\n${normalizeFn}\nnormalize_jsonl_transcript "$1"`, "bash", scenarioLog],
      {
        encoding: "utf8",
        stdio: "pipe",
      }
    );

    expect(normalized.trim()).toBe(JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "ok" } }));
    rmSync(tempDir, { recursive: true, force: true });
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
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: 'env FOO=1 bash scripts/e2e/demo.sh' },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: 'timeout 10 bash scripts/dev/demo.sh' },
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

    expect(output.trim()).toBe("4");
  });

  it("detects relay health preflights from local binaries and go run invocations", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");
    const countCommandHitsFn = extractShellFunction(content, "count_command_hits");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-scenarios-health-"));
    const parsedLog = join(tempDir, "parsed.jsonl");

    writeFileSync(
      parsedLog,
      [
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: '/bin/zsh -lc "go run . relay health --quiet"' },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: '/bin/zsh -lc "./cli/cli relay health"' },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: '/bin/zsh -lc "scripts/onboarding/bin/ha-nova relay health"' },
        }),
      ].join("\n") + "\n"
    );

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
CLI_CMD_PATTERN='([[:alnum:]_./-]*ha-nova|[.]/cli/cli|cli/cli)'
GO_RUN_PATTERN='go[[:space:]]+run([[:space:]]+[[:alnum:]_./-]+){1,4}'
RELAY_HEALTH_PATTERN="((^|[[:space:][:punct:]])(\${CLI_CMD_PATTERN}|\${GO_RUN_PATTERN})[[:space:]]+relay[[:space:]]+health([[:space:][:punct:]]|$))|/health"
${countCommandHitsFn}
count_command_hits "$1" "$RELAY_HEALTH_PATTERN"`,
        "bash",
        parsedLog,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("3");
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("does not treat dotted entity-id suffixes as rule-code markers", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");
    const containsRuleCodeMarkerFn = extractShellFunction(content, "contains_rule_code_marker");

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
${containsRuleCodeMarkerFn}
if contains_rule_code_marker 'sensor.h2 switch.r3'; then
  echo bad_entity_match
elif contains_rule_code_marker 'R19 fallback issue'; then
  echo exact_rule_match
else
  echo missed_rule_match
fi`,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("exact_rule_match");
  });

  it("only flags doctor and health preflights when they happen before the first HA action", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-scenarios-e2e.sh", "utf8");
    const countCommandHitsFn = extractShellFunction(content, "count_command_hits");
    const firstCommandIndexFn = extractShellFunction(content, "first_command_index");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-scenarios-preflight-order-"));
    const parsedLog = join(tempDir, "parsed.jsonl");

    writeFileSync(
      parsedLog,
      [
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: 'ha-nova relay ws --command config/entity_registry/list_for_display' },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: "ha-nova doctor" },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: 'ha-nova relay health --quiet' },
        }),
      ].join("\n") + "\n"
    );

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
CLI_CMD_PATTERN='([[:alnum:]_./-]*ha-nova|[.]/cli/cli|cli/cli)'
CLI_DOCTOR_PATTERN='((doctor|ready)([[:space:]]|$))|quick([[:space:]]+check)?([[:space:]]|$)'
GO_RUN_PATTERN='go[[:space:]]+run([[:space:]]+[[:alnum:]_./-]+){1,4}'
RELAY_HEALTH_PATTERN="((^|[[:space:][:punct:]])(\${CLI_CMD_PATTERN}|\${GO_RUN_PATTERN})[[:space:]]+relay[[:space:]]+health([[:space:][:punct:]]|$))|/health"
${countCommandHitsFn}
${firstCommandIndexFn}
doctor_count="$(count_command_hits "$1" "(^|[[:space:][:punct:]])\${CLI_DOCTOR_PATTERN}([[:space:][:punct:]]|$)")"
health_count="$(count_command_hits "$1" "$RELAY_HEALTH_PATTERN")"
ha_action_idx="$(first_command_index "$1" 'relay[[:space:]]+(ws|core)([[:space:]]|$)')"
doctor_idx="$(first_command_index "$1" "(^|[[:space:][:punct:]])\${CLI_DOCTOR_PATTERN}([[:space:][:punct:]]|$)")"
health_idx="$(first_command_index "$1" "$RELAY_HEALTH_PATTERN")"
doctor_before_action=false
health_before_action=false
if [[ -n "$doctor_idx" && ( -z "$ha_action_idx" || "$doctor_idx" -lt "$ha_action_idx" ) ]]; then
  doctor_before_action=true
fi
if [[ -n "$health_idx" && ( -z "$ha_action_idx" || "$health_idx" -lt "$ha_action_idx" ) ]]; then
  health_before_action=true
fi
printf '%s,%s,%s,%s\n' "$doctor_count" "$health_count" "$doctor_before_action" "$health_before_action"`,
        "bash",
        parsedLog,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("1,1,false,false");
    rmSync(tempDir, { recursive: true, force: true });
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
