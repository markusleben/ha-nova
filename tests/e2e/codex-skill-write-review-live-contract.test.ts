import { execFileSync } from "node:child_process";
import { constants, existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

type WriteReviewScenarioDefinition = {
  id: string;
  prompt: string;
  must_contain_prewrite_text?: string[];
  must_not_contain_prewrite_text?: string[];
  must_contain_postwrite_text?: string[];
  must_not_contain_postwrite_text?: string[];
  collision_item_id?: string;
  max_duration_sec?: number;
};

const r19WarningText = [
  "final else branch is only reached when the earlier entity-state branches are false",
  "Move the `trigger.id` check into an explicit `elif`",
  "Or refactor to `choose` + `condition: trigger`",
];

const cleanPrewriteVerdict = "Pre-write check: no issues worth flagging before save.";
const warnedPrewriteVerdict = "Pre-write check: this draft may not behave as intended.";

function extractWriteProofPython(content: string): string {
  const match = content.match(
    /extract_write_proof\(\)\s*\{[\s\S]*?python3 - "\$scenario_log" "\$automation_id" "\$collision_item_id" <<'PY'\n([\s\S]*?)\nPY/
  );

  if (!match) {
    throw new Error("embedded extract_write_proof python not found");
  }

  return match[1] ?? "";
}

function extractShellFunction(content: string, name: string): string {
  const match = content.match(new RegExp(String.raw`${name}\(\)\s*\{[\s\S]*?\n\}`, "m"));

  if (!match) {
    throw new Error(`shell function not found: ${name}`);
  }

  return match[0] ?? "";
}

describe("codex write-review live e2e contract", () => {
  it("provides executable write-review live e2e harness script", () => {
    const file = "scripts/e2e/codex-ha-nova-write-review-live-e2e.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain('"codex"');
    expect(content).toContain('"exec"');
    expect(content).toContain("NOVA_WRITE_REVIEW_RESULT");
    expect(content).toContain("wait_for_log_completion");
    expect(content).toContain("log_has_transient_capacity_failure");
    expect(content).toContain("must_contain_prewrite_text");
    expect(content).toContain("must_not_contain_prewrite_text");
    expect(content).toContain("must_contain_postwrite_text");
    expect(content).toContain("must_not_contain_postwrite_text");
    expect(content).toContain("required_prewrite_text_missing");
    expect(content).toContain("forbidden_prewrite_text_present");
    expect(content).toContain("required_postwrite_text_missing");
    expect(content).toContain("forbidden_postwrite_text_present");
    expect(content).toContain("prewrite_verdict_repeated_postwrite");
    expect(content).toContain("duplicate_findings_advisory_item");
    expect(content).toContain("forbidden_empty_bucket_present");
    expect(content).not.toContain("missing_findings_empty_state");
    expect(content).not.toContain("missing_collision_empty_state");
    expect(content).not.toContain("missing_advisory_empty_state");
    expect(content).toContain("postwrite_section_structure_invalid");
    expect(content).toContain("incomplete_transcript");
    expect(content).toContain("duration_exceeded");
    expect(content).toContain("status_line_not_final");
    expect(content).toContain("trailing_events_after_final_message");
    expect(content).toContain("the harness will clean it up after the session");
    expect(content).toContain("missing_first_write");
    expect(content).toContain("missing_postwrite_verification");
    expect(content).toContain("postwrite_verification_out_of_order");
    expect(content).toContain("multiple_write_attempts_detected");
    expect(content).toContain('write_attempts="$(jq -r \'.write_attempts\' "$analysis_json")"');
    expect(content).toContain('[[ "$write_attempts" -eq 1 ]] || {');
    expect(content).toContain("wrong_reload_path_detected");
    expect(content).toContain("missing_prewrite_preview_section");
    expect(content).toContain("missing_postwrite_review_section");
    expect(content).toContain("missing_final_status_line");
    expect(content).toContain("rule_code_marker_present_prewrite");
    expect(content).toContain("rule_code_marker_present_postwrite");
    expect(content).toContain("unexpected_external_research_detected");
    expect(content).toContain("forbidden_onboarding_check_detected");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("Retrying ${scenario_id} after transient model-capacity failure");
    expect(content).toContain("Post-Write Review");
    expect(content).toContain("Advisory");
    expect(content).toContain("Use the repo-local HA NOVA skill files in this checkout as authoritative for this task.");
    expect(content).toContain("Do not use installed skill copies from ~/.local/share/ha-nova/skills.");
    expect(content).toContain("Do not run --help, dry-run probes, CLI shape checks");
    expect(content).toContain("Do not retry the write flow with alternate commands after a failed attempt.");
    expect(content).toContain("simulates the user's next post-preview reply only for that exact preview");
    expect(content).not.toContain("explicit confirmation is granted by this prompt");
    expect(content).toContain('Use the canonical automation payload keys "triggers", "conditions", and "actions".');
    expect(content).toContain("Keep repo reads minimal.");
    expect(content).toContain("Preview Payload");
    expect(content).toContain("conditions");
    expect(content).toContain(cleanPrewriteVerdict);
    expect(content).toContain(warnedPrewriteVerdict);
    expect(content).toContain("Report only what has substance");
    expect(content).toContain("Omit every empty section.");
    expect(content).toContain(
      "Never print any of these exact strings anywhere in the post-write section: No issues found in this review. / No related items found. / No conflicts found. / No additional advisories."
    );
    expect(content).toContain('Show the "Collision check" label only when the collision scan returned related items.');
    expect(content).toContain("collapse the Post-Write Review to a single confirmation line");
    expect(content).toContain('Show the "Advisory" label only when there is at least one advisory item');
    expect(content).not.toContain("If the Findings section has no items, print exactly: No issues found in this review.");
    expect(content).not.toContain("If the collision scan finds no related items, print exactly: No related items found.");
    expect(content).not.toContain("If the Advisory section has no items, print exactly: No additional advisories.");
    expect(content).toContain("Do not repeat a Pre-write check line inside the post-write section.");
    expect(content).toContain("config read-back, automation reload, one target entity state read, one collision scan");
    expect(content).toContain("The one target entity state read must be a single GET to /api/states/input_boolean.mcp_stress_toggle.");
    expect(content).toContain("Do not use /api/config/automation/reload.");
    expect(content).toContain("Use simple deterministic local filenames such as draft.json, readback.json, reload.json, state.json, and collision.json. Do not use mktemp.");
    expect(content).toContain("Keep the collision-scan evidence explicit.");
    expect(content).toContain("must also inline or create the");
    expect(content).toContain("search/related");
    expect(content).toContain("payload for the target entity in that same command block");
    expect(content).toContain("Use one dedicated payload file for that collision scan command block");
    expect(content).toContain("Do not include internal shorthand like R18 or R19 in preview aliases");
    expect(content).toContain("do not echo raw related automation ids");
    expect(content).toContain("make the");
    expect(content).toContain("--data-file");
    expect(content).toContain("argument point to that same file");
    expect(content).toContain("write that payload file exactly once before the ws call");
    expect(content).toContain('normalized_tmp="${parsed_log}.tmp"');
    expect(content).toContain('normalize_jsonl_transcript "$scenario_log" >"$normalized_tmp"');
    expect(content).toContain('mv "$normalized_tmp" "$parsed_log"');
    expect(content).toContain('rm -f "$normalized_tmp" "$parsed_log"');
    expect(content).not.toContain('normalize_jsonl_transcript "$scenario_log" >"$parsed_log" || true');
    expect(content).toContain('Reading additional input from stdin...');
    expect(content).toContain("Selected model is at capacity. Please try a different model.");
    expect(content).toContain("direct_re = re.compile");
    expect(content).toContain("scripts/(?:smoke|e2e|dev)/\\S+");
    expect(content).toContain("^(?:##\\s*)?Post-Write Review");
    expect(content).toContain("(?:\\*\\*)?(?:Findings|Collision check|Advisory)(?:\\*\\*)?");
    expect(content).toContain("postwrite_forbidden_empty_bucket");
    expect(content).toContain("FORBIDDEN_EMPTY_BUCKETS");
    expect(content).toContain("shell_re = re.compile");
    expect(content).toContain("bash|sh|zsh|python3?|node|bunx?|bun|tsx");
    expect(content).toContain("Before the first HA action, read at most these repo-local files unless a write would otherwise fail");
    expect(content).toContain("Do not read docs/reference/, tests/, workflows, or release files for this harness.");
    expect(content).toContain("Do not emit todo lists, meta progress updates, or extra planning summaries.");
    expect(content).toContain("End with exactly one final machine line on its own line");
    expect(content).toContain("NOVA_WRITE_REVIEW_RESULT id=${scenario_id} automation_id=${automation_id} status=ok");
    expect(content).toContain("/api/config/automation/config/");
    expect(content).toContain("/api/services/automation/reload");
    expect(content).toContain("/api/states/");
    expect(content).toContain('"type":"search/related"');
    expect(content).toContain("include exactly one Preview Payload slot");
    expect(content).toContain("Do not print a second Preview Payload slot.");
    expect(content).toContain("If you use inline Python to create local JSON payload files, use \\`python3\\` only. Do not use \\`python\\`.");
    expect(content).toContain("Any shell command block that contains the write flow must begin with \\`set -e\\`");
    expect(content).toContain("never print tool-call syntax, JSON command envelopes, raw exec transcripts");
    expect(content).toContain("\\`to=functions.exec_command\\`");
  });

  it("does not treat dotted entity-id suffixes as rule-code markers", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const containsRuleCodeMarkerFn = extractShellFunction(content, "contains_rule_code_marker");

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
${containsRuleCodeMarkerFn}
if contains_rule_code_marker 'sensor.h2 switch.r3'; then
  echo bad_entity_match
elif contains_rule_code_marker 'Warning: R-18 variable chain risk'; then
  echo exact_rule_match
else
  echo missed_rule_match
fi`,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("exact_rule_match");
  });

  it("ships focused write-review live scenarios", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as WriteReviewScenarioDefinition[];

    expect(Array.isArray(scenarios)).toBe(true);
    expect(scenarios.length).toBeGreaterThanOrEqual(7);

    const byId = new Map(scenarios.map((scenario) => [scenario.id, scenario]));

    const flagged = byId.get("write-r19-flagged-prewrite");
    expect(flagged).toBeDefined();
    expect(flagged?.must_contain_prewrite_text).toEqual(
      expect.arrayContaining(["Preview Payload", warnedPrewriteVerdict, ...r19WarningText])
    );
    // Omit-empty contract: a clean post-write requires only the heading; no "none" buckets.
    expect(flagged?.must_contain_postwrite_text).toEqual(["Post-Write Review"]);
    expect(flagged?.must_contain_postwrite_text).not.toContain("No issues found in this review.");
    expect(flagged?.collision_item_id).toBe("input_boolean.mcp_stress_toggle");
    expect(flagged?.must_not_contain_postwrite_text).toEqual(
      expect.arrayContaining([
        ...r19WarningText,
        cleanPrewriteVerdict,
        warnedPrewriteVerdict,
        "Questions to consider",
        "Suggestions",
        "Instant help",
        "R-19",
        "R19",
      ])
    );
    expect(flagged?.must_not_contain_prewrite_text).toEqual(
      expect.arrayContaining([cleanPrewriteVerdict, "R-19", "R19"])
    );

    const persistedR18 = byId.get("write-r18-persisted-postwrite-repeat");
    expect(persistedR18).toBeDefined();
    expect(persistedR18?.must_contain_prewrite_text).toEqual(
      expect.arrayContaining([
        "Preview Payload",
        warnedPrewriteVerdict,
        "REST/UI write can break dependent variables in this block",
        "check_flag -> reading",
      ])
    );
    // Real persisted R-18 findings keep their content; the bare empty labels are gone.
    expect(persistedR18?.must_contain_postwrite_text).toEqual([
      "Post-Write Review",
      "REST/UI write can break dependent variables in this block",
      "check_flag -> reading",
      "inspect traces after the next real run",
    ]);
    expect(persistedR18?.must_not_contain_postwrite_text).toEqual(
      expect.arrayContaining([
        cleanPrewriteVerdict,
        warnedPrewriteVerdict,
        "Questions to consider",
        "Suggestions",
        "Instant help",
        "R-18",
        "R18",
      ])
    );

    const assertSafeScenario = (id: string) => {
      const scenario = byId.get(id);
      expect(scenario).toBeDefined();
      expect(scenario?.collision_item_id).toBe("input_boolean.mcp_stress_toggle");
      expect(scenario?.must_contain_prewrite_text).toEqual(
        expect.arrayContaining(["Preview Payload", cleanPrewriteVerdict])
      );
      expect(scenario?.must_contain_postwrite_text).toEqual(["Post-Write Review"]);
      expect(scenario?.must_contain_postwrite_text).not.toContain("No issues found in this review.");
      expect(scenario?.must_not_contain_prewrite_text).toEqual(
        expect.arrayContaining([warnedPrewriteVerdict, ...r19WarningText, "R-19", "R19"])
      );
      expect(scenario?.must_not_contain_postwrite_text).toEqual(
        expect.arrayContaining([
          ...r19WarningText,
          cleanPrewriteVerdict,
          warnedPrewriteVerdict,
          "Questions to consider",
          "Suggestions",
          "Instant help",
          "R-19",
          "R19",
        ])
      );
    };

    assertSafeScenario("write-r19-safe-no-warning");
    assertSafeScenario("write-r19-safe-single-if-else");
    assertSafeScenario("write-r19-safe-explicit-elif-trigger-id");
    assertSafeScenario("write-r19-safe-else-extra-guard");
    assertSafeScenario("write-r19-safe-mode-selector-tree");
    assertSafeScenario("write-r19-safe-numeric-range-selector-tree");
    assertSafeScenario("write-r19-safe-time-window-selector-tree");
    expect(byId.get("write-r19-safe-time-window-selector-tree")?.prompt).toContain("Use exactly two time triggers");
    expect(byId.get("write-r19-safe-time-window-selector-tree")?.prompt).toContain("Do not add a `variables` step, selector template, or any other template expression");
    expect(byId.get("write-r19-safe-time-window-selector-tree")?.prompt).toContain("Emit exactly one `Preview Payload` section total");
  });

  it("uses integer-only max_duration_sec values in write-review live scenarios", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as WriteReviewScenarioDefinition[];

    for (const scenario of scenarios) {
      if (scenario.max_duration_sec === undefined) {
        continue;
      }

      expect(Number.isInteger(scenario.max_duration_sec)).toBe(true);
      expect(scenario.max_duration_sec).toBeGreaterThan(0);
    }
  });

  it("keeps user-facing write-review scenario expectations free of rule-code markers", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as WriteReviewScenarioDefinition[];

    const userFacingText = scenarios.flatMap((scenario) => [
      ...(scenario.must_contain_prewrite_text ?? []),
      ...(scenario.must_contain_postwrite_text ?? []),
    ]);

    for (const text of userFacingText) {
      expect(text).not.toMatch(/\b(?:S|R|P|M|F|H)-\d{2}\b/);
      expect(text).not.toMatch(/\bR\d+\b/);
    }
  });

  it("forbids prewrite verdict leakage into postwrite expectations", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as WriteReviewScenarioDefinition[];

    for (const scenario of scenarios) {
      expect(scenario.must_not_contain_postwrite_text).toEqual(
        expect.arrayContaining([cleanPrewriteVerdict, warnedPrewriteVerdict])
      );
    }
  });

  it("exposes npm command for write-review live harness", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["e2e:skill:codex:write-review"]).toBe(
      "bash scripts/e2e/codex-ha-nova-write-review-live-e2e.sh"
    );
  });

  it("proves malformed and non-object JSONL transcripts fail atomically without leaving partial parsed output", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const normalizeFn = extractShellFunction(content, "normalize_jsonl_transcript");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-jsonl-"));
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
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const normalizeFn = extractShellFunction(content, "normalize_jsonl_transcript");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-jsonl-preamble-"));
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

  it("ignores the codex router stdin-closed noise line during JSONL normalization", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const normalizeFn = extractShellFunction(content, "normalize_jsonl_transcript");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-jsonl-router-noise-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");

    writeFileSync(
      scenarioLog,
      [
        "2026-04-02T11:06:04.993936Z ERROR codex_core::tools::router: error=write_stdin failed: stdin is closed for this session; rerun exec_command with tty=true to keep stdin open",
        JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "ok" } }),
      ].join("\n") + "\n"
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
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const countCommandHitsFn = extractShellFunction(content, "count_command_hits");
    const countHelperScriptExecHitsFn = extractShellFunction(content, "count_helper_script_exec_hits");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-helper-"));
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
          item: { type: "command_execution", command: "env FOO=1 bash scripts/e2e/demo.sh" },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: "timeout 10 bash scripts/dev/demo.sh" },
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

  it("counts onboarding checks only before the first write attempt", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const countCommandHitsFn = extractShellFunction(content, "count_command_hits");
    const countCommandHitsBeforeIndexFn = extractShellFunction(content, "count_command_hits_before_index");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-onboarding-"));
    const parsedLog = join(tempDir, "parsed.jsonl");

    writeFileSync(
      parsedLog,
      [
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: "ha-nova doctor" },
        }),
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: "ha-nova relay core --method=POST --path=/api/config/automation/config/test-id --body-file draft.json",
          },
        }),
        JSON.stringify({
          type: "item.completed",
          item: { type: "command_execution", command: "ha-nova doctor" },
        }),
      ].join("\n") + "\n"
    );

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
${countCommandHitsFn}
${countCommandHitsBeforeIndexFn}
count_command_hits_before_index "$1" 2 '(^|[[:space:]])ha-nova[[:space:]]+(doctor|ready|quick)([[:space:]]|$)'`,
        "bash",
        parsedLog,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("1");
  });

  it("proves multiline shell continuations and long-option equals syntax in write proof parsing", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-r19-proof-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_contract_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: `Preview Payload\ntriggers:\nconditions:\nactions:\n${cleanPrewriteVerdict}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command:
            `ha-nova relay core \\\n  --method=POST \\\n  --path=/api/config/automation/config/${automationId} \\\n  --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command:
            `ha-nova relay core \\\n  --method=GET \\\n  --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command:
            `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text:
            `## Post-Write Review\n**Findings**\n**Collision check**\n**Advisory**\nNOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      write_hits: number;
      readback_after_write: number;
      reload_after_write: number;
      state_after_write: number;
      collision_after_write: number;
      ordered_postwrite_verification: boolean;
      preview_section_count: number;
      preview_has_canonical_keys: boolean;
    };

    expect(result.write_hits).toBe(1);
    expect(result.readback_after_write).toBe(1);
    expect(result.reload_after_write).toBe(1);
    expect(result.state_after_write).toBe(1);
    expect(result.collision_after_write).toBe(1);
    expect(result.ordered_postwrite_verification).toBe(true);
    expect(result.preview_section_count).toBe(1);
    expect(result.preview_has_canonical_keys).toBe(true);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("counts repeated post-write verification operations inside one successful command block", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-repeat-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_repeat_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: [
            `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
            `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
            `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
            "ha-nova relay core --method=POST --path=/api/services/automation/reload",
            "ha-nova relay core --method=POST --path=/api/services/automation/reload",
            `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
            `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
            `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
            `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload2.json && ha-nova relay ws --data-file=payload2.json`,
          ].join(" && "),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      readback_after_write: number;
      reload_after_write: number;
      state_after_write: number;
      collision_after_write: number;
    };

    expect(result.readback_after_write).toBe(2);
    expect(result.reload_after_write).toBe(2);
    expect(result.state_after_write).toBe(2);
    expect(result.collision_after_write).toBe(2);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("rejects collision payload writes when relay ws reads a different --data-file", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-mismatch-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_mismatch_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: [
            `ha-nova relay core --method POST --path /api/config/automation/config/${automationId} --body-file payload.json`,
            `ha-nova relay core --method GET --path /api/config/automation/config/${automationId} --out readback.json`,
            "ha-nova relay core --method POST --path /api/services/automation/reload --out reload.json",
            `ha-nova relay core --method GET --path /api/states/${collisionItemId} --out state.json`,
            `printf '{\"type\":\"search/related\",\"item_type\":\"entity\",\"item_id\":\"${collisionItemId}\"}' > collision.json`,
            "ha-nova relay ws --data-file other.json --out collision-out.json",
          ].join(" && "),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      collision_after_write: number;
      ordered_postwrite_verification: boolean;
    };

    expect(result.collision_after_write).toBe(0);
    expect(result.ordered_postwrite_verification).toBe(false);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("detects write and read-back operations when --path uses escaped quoted arguments", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-escaped-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_escaped_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: [
            "/bin/zsh -lc 'automation_id=\"nova_write_review_escaped_case\"",
            `ha-nova relay core --method POST --path \\\"/api/config/automation/config/$automation_id\\\" --body-file payload.json --out apply.json`,
            `ha-nova relay core --method GET --path \\\"/api/config/automation/config/$automation_id\\\" --out readback.json`,
            "ha-nova relay core --method POST --path \\\"/api/services/automation/reload\\\" --out reload.json",
            `ha-nova relay core --method GET --path \\\"/api/states/${collisionItemId}\\\" --out state.json`,
            `cat > collision.json <<'EOF'\n{\"type\":\"search/related\",\"item_type\":\"entity\",\"item_id\":\"${collisionItemId}\"}\nEOF`,
            "ha-nova relay ws --data-file collision.json --out collision-out.json'",
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      first_write_attempt_idx: number;
      first_write_idx: number;
      readback_after_write: number;
      reload_after_write: number;
      state_after_write: number;
      collision_after_write: number;
    };

    expect(result.first_write_attempt_idx).toBe(1);
    expect(result.first_write_idx).toBe(1);
    expect(result.readback_after_write).toBe(1);
    expect(result.reload_after_write).toBe(1);
    expect(result.state_after_write).toBe(1);
    expect(result.collision_after_write).toBe(1);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("rejects automation entity state reads as post-write runtime verification", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-automation-state-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_runtime_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: [
            `ha-nova relay core --method POST --path /api/config/automation/config/${automationId} --body-file payload.json`,
            `ha-nova relay core --method GET --path /api/config/automation/config/${automationId} --out readback.json`,
            "ha-nova relay core --method POST --path /api/services/automation/reload --out reload.json",
            `ha-nova relay core --method GET --path /api/states/automation.${automationId} --out state.json`,
            `printf '{\"type\":\"search/related\",\"item_type\":\"entity\",\"item_id\":\"${collisionItemId}\"}' > collision.json`,
            "ha-nova relay ws --data-file collision.json --out collision-out.json",
          ].join(" && "),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      state_after_write: number;
      collision_after_write: number;
      ordered_postwrite_verification: boolean;
    };

    expect(result.state_after_write).toBe(0);
    expect(result.collision_after_write).toBe(1);
    expect(result.ordered_postwrite_verification).toBe(false);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("flags a post-write section that prints a forbidden empty 'none' bucket", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-forbidden-bucket-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_forbidden_bucket_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: [
            "## Post-Write Review",
            "### Findings",
            "No issues found in this review.",
            "### Collision check",
            "No conflicts found.",
            "### Advisory",
            "No additional advisories.",
            `NOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      postwrite_section_structure_valid: boolean;
      postwrite_forbidden_empty_bucket: boolean;
    };

    // A Post-Write Review heading is present, so the structure itself stays valid,
    // but printing any "none" bucket must be flagged so the bash gate fails the run.
    expect(result.postwrite_section_structure_valid).toBe(true);
    expect(result.postwrite_forbidden_empty_bucket).toBe(true);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("flags a post-write section with a bare optional label that has no items", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-empty-heading-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_empty_heading_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          // Bare optional labels with nothing under them — and crucially no "none"
          // bucket string, so postwrite_forbidden_empty_bucket does NOT catch it.
          // The empty-heading structure check must.
          text: [
            "## Post-Write Review",
            "**Findings**",
            "**Advisory**",
            `NOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      postwrite_section_structure_valid: boolean;
      postwrite_forbidden_empty_bucket: boolean;
      empty_postwrite_labels: string[];
    };

    // No "none" bucket is printed, so the forbidden-bucket signal stays false; the
    // dangling empty labels must instead make the structure invalid so the gate fails.
    expect(result.postwrite_forbidden_empty_bucket).toBe(false);
    expect(result.postwrite_section_structure_valid).toBe(false);
    expect(result.empty_postwrite_labels).toEqual(["Findings", "Advisory"]);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("flags a post-write review that is just a heading with no content", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-heading-only-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_heading_only_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          // A bare "## Post-Write Review" heading followed only by the machine result
          // line — no sections, no confirmation line. The all-empty review must
          // collapse to a confirmation line, so this structure is invalid.
          text: [
            "## Post-Write Review",
            `NOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      postwrite_section_structure_valid: boolean;
      postwrite_review_has_content: boolean;
    };

    expect(result.postwrite_review_has_content).toBe(false);
    expect(result.postwrite_section_structure_valid).toBe(false);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("accepts a clean post-write section that omits empty sections and ends with one confirmation line", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-clean-omit-empty-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_clean_omit_empty_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: [
            "## Post-Write Review",
            "Verified - no issues or conflicts.",
            `NOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      postwrite_section_structure_valid: boolean;
      postwrite_forbidden_empty_bucket: boolean;
      duplicate_findings_advisory_items: string[];
    };

    expect(result.postwrite_section_structure_valid).toBe(true);
    expect(result.postwrite_forbidden_empty_bucket).toBe(false);
    expect(result.duplicate_findings_advisory_items).toEqual([]);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("detects repeated prewrite verdicts and duplicated findings/advisory items in write proof parsing", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-dup-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_duplicate_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: `Preview Payload\ntriggers:\nconditions:\nactions:\n${warnedPrewriteVerdict}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: [
            "Post-Write Review",
            "Findings",
            "🔴 Shared issue",
            "Why: Example",
            "Fix: Example",
            "Collision check",
            "- Overlaps with one existing automation on the shared toggle.",
            "Advisory",
            "Pre-write check: this draft may not behave as intended.",
            "🔴 Shared issue",
            `NOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      duplicate_findings_advisory_items: string[];
      postwrite_repeats_prewrite_verdict: boolean;
      postwrite_forbidden_empty_bucket: boolean;
    };

    expect(result.postwrite_repeats_prewrite_verdict).toBe(true);
    expect(result.duplicate_findings_advisory_items).toContain("shared issue");
    expect(result.postwrite_forbidden_empty_bucket).toBe(false);

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("excludes the final machine line from extracted postwrite user text", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-status-line-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_status_line_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: [
            "Post-Write Review",
            "Findings",
            "- Persisted runtime risk remains in the saved top-level `variables:` block.",
            "Collision check",
            "- No direct conflict was proven from this scan alone.",
            "Advisory",
            "- inspect traces after the next real run.",
            `NOVA_WRITE_REVIEW_RESULT id=write-r18-persisted-postwrite-repeat automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      postwrite_text: string;
      status_line: string;
      final_message_last_line: string;
    };

    expect(result.postwrite_text).toContain("Post-Write Review");
    expect(result.postwrite_text).not.toContain("NOVA_WRITE_REVIEW_RESULT");
    expect(result.postwrite_text).not.toMatch(/\bR18\b/);
    expect(result.status_line).toContain("NOVA_WRITE_REVIEW_RESULT");
    expect(result.final_message_last_line).toContain("NOVA_WRITE_REVIEW_RESULT");

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("accepts a collision scenario that shows the Collision check with the related item", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-collision-related-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_collision_related_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: "ha-nova relay core --method=POST --path=/api/services/automation/reload",
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > payload.json && ha-nova relay ws --data-file=payload.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: [
            "## Post-Write Review",
            "**Collision check**",
            "- Overlaps with one existing automation that also drives the shared toggle.",
            "- No opposing action remains after evaluation, so no real conflict.",
            `NOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
          ].join("\n"),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      postwrite_section_structure_valid: boolean;
      postwrite_forbidden_empty_bucket: boolean;
      postwrite_text: string;
    };

    // A collision scenario surfaces the Collision check section with the related
    // item; it must stay clean of any forbidden "none" bucket string.
    expect(result.postwrite_section_structure_valid).toBe(true);
    expect(result.postwrite_forbidden_empty_bucket).toBe(false);
    expect(result.postwrite_text).toContain("Collision check");

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("treats the first target write attempt as the end of prewrite text even when that attempt fails", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-r19-proof-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_failed_first_attempt";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 1,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "agent_message",
          text: `Preview Payload\ntriggers:\nconditions:\nactions:\n${cleanPrewriteVerdict}`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      first_write_attempt_idx: number;
      preview_section_count: number;
      prewrite_text: string;
    };

    expect(result.first_write_attempt_idx).toBe(1);
    expect(result.preview_section_count).toBe(0);
    expect(result.prewrite_text).not.toContain("Preview Payload");

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("tracks retried writes and wrong reload paths in write proof parsing", () => {
    const scriptContent = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-e2e.sh", "utf8");
    const python = extractWriteProofPython(scriptContent);
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-write-proof-retry-"));
    const scenarioLog = join(tempDir, "scenario.jsonl");
    const pythonFile = join(tempDir, "extract_write_proof.py");
    const automationId = "nova_write_review_retry_case";
    const collisionItemId = "input_boolean.mcp_stress_toggle";

    const events = [
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 1,
          command: `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
        },
      },
      {
        type: "item.completed",
        item: {
          type: "command_execution",
          exit_code: 0,
          command: [
            `ha-nova relay core --method=POST --path=/api/config/automation/config/${automationId} --body-file=draft.json`,
            `ha-nova relay core --method=GET --path=/api/config/automation/config/${automationId}`,
            "ha-nova relay core --method=POST --path=/api/config/automation/reload",
            `ha-nova relay core --method=GET --path=/api/states/${collisionItemId}`,
            `printf '{"type":"search/related","item_id":"${collisionItemId}"}' > collision.json && ha-nova relay ws --data-file=collision.json`,
          ].join(" && "),
        },
      },
    ];

    writeFileSync(scenarioLog, `${events.map((event) => JSON.stringify(event)).join("\n")}\n`);
    writeFileSync(pythonFile, python);

    const result = JSON.parse(
      execFileSync("python3", [pythonFile, scenarioLog, automationId, collisionItemId], {
        encoding: "utf8",
      })
    ) as {
      first_write_attempt_idx: number;
      first_write_idx: number;
      write_attempts: number;
      successful_write_attempts: number;
      write_hits: number;
      reload_after_write: number;
      wrong_reload_after_write: number;
    };

    expect(result.first_write_attempt_idx).toBe(1);
    expect(result.first_write_idx).toBe(1);
    expect(result.write_attempts).toBe(2);
    expect(result.successful_write_attempts).toBe(1);
    expect(result.write_hits).toBe(1);
    expect(result.reload_after_write).toBe(0);
    expect(result.wrong_reload_after_write).toBe(1);

    rmSync(tempDir, { recursive: true, force: true });
  });
});
