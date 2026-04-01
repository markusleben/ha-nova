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
    expect(content).toContain("must_contain_prewrite_text");
    expect(content).toContain("must_not_contain_prewrite_text");
    expect(content).toContain("must_contain_postwrite_text");
    expect(content).toContain("must_not_contain_postwrite_text");
    expect(content).toContain("required_prewrite_text_missing");
    expect(content).toContain("forbidden_prewrite_text_present");
    expect(content).toContain("required_postwrite_text_missing");
    expect(content).toContain("forbidden_postwrite_text_present");
    expect(content).toContain("incomplete_transcript");
    expect(content).toContain("duration_exceeded");
    expect(content).toContain("status_line_not_final");
    expect(content).toContain("trailing_events_after_final_message");
    expect(content).toContain("the harness will clean it up after the session");
    expect(content).toContain("missing_first_write");
    expect(content).toContain("missing_postwrite_verification");
    expect(content).toContain("postwrite_verification_out_of_order");
    expect(content).toContain("missing_prewrite_preview_section");
    expect(content).toContain("missing_postwrite_review_section");
    expect(content).toContain("missing_final_status_line");
    expect(content).toContain("unexpected_external_research_detected");
    expect(content).toContain("forbidden_onboarding_check_detected");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("Post-Write Review");
    expect(content).toContain("Use the repo-local HA NOVA skill files in this checkout as authoritative for this task.");
    expect(content).toContain("Do not use installed skill copies from ~/.local/share/ha-nova/skills.");
    expect(content).toContain('Use the canonical automation payload keys "triggers", "conditions", and "actions".');
    expect(content).toContain("Keep repo reads minimal.");
    expect(content).toContain("## Preview Payload");
    expect(content).toContain("conditions");
    expect(content).toContain("PREWRITE CHECK: clean");
    expect(content).toContain("PREWRITE CHECK: R-19 detected");
    expect(content).toContain("config read-back, automation reload, one target entity state read, one collision scan");
    expect(content).toContain("Keep the collision-scan evidence explicit.");
    expect(content).toContain("must also inline or create the");
    expect(content).toContain("search/related");
    expect(content).toContain("payload for the target entity in that same command block");
    expect(content).toContain("Use one dedicated payload file for that collision scan command block");
    expect(content).toContain("make the");
    expect(content).toContain("--data-file");
    expect(content).toContain("argument point to that same file");
    expect(content).toContain("write that payload file exactly once before the ws call");
    expect(content).toContain('normalized_tmp="${parsed_log}.tmp"');
    expect(content).toContain('normalize_jsonl_transcript "$scenario_log" >"$normalized_tmp"');
    expect(content).toContain('mv "$normalized_tmp" "$parsed_log"');
    expect(content).toContain('rm -f "$normalized_tmp" "$parsed_log"');
    expect(content).not.toContain('normalize_jsonl_transcript "$scenario_log" >"$parsed_log" || true');
    expect(content).toContain('(\\./)?scripts/(smoke|e2e|dev)/');
    expect(content).toContain('bash|sh|zsh|python3?|node|bunx?|bun|tsx');
    expect(content).toContain("Before the first HA action, read at most these repo-local files unless a write would otherwise fail");
    expect(content).toContain("Do not read docs/reference/, tests/, workflows, or release files for this harness.");
    expect(content).toContain("Do not emit todo lists, meta progress updates, or extra planning summaries.");
    expect(content).toContain("End with exactly one final machine line on its own line");
    expect(content).toContain("NOVA_WRITE_REVIEW_RESULT id=${scenario_id} automation_id=${automation_id} status=ok");
    expect(content).toContain("/api/config/automation/config/");
    expect(content).toContain("/api/services/automation/reload");
    expect(content).toContain("/api/states/");
    expect(content).toContain('"type":"search/related"');
  });

  it("ships focused write-review live scenarios", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-write-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as WriteReviewScenarioDefinition[];

    expect(Array.isArray(scenarios)).toBe(true);
    expect(scenarios.length).toBeGreaterThanOrEqual(6);

    const byId = new Map(scenarios.map((scenario) => [scenario.id, scenario]));

    const flagged = byId.get("write-r19-flagged-prewrite");
    expect(flagged).toBeDefined();
    expect(flagged?.must_contain_prewrite_text).toEqual(
      expect.arrayContaining(["## Preview Payload", "PREWRITE CHECK: R-19 detected", ...r19WarningText])
    );
    expect(flagged?.must_contain_postwrite_text).toEqual(
      expect.arrayContaining(["Post-Write Review", "Findings", "Collision check"])
    );
    expect(flagged?.collision_item_id).toBe("input_boolean.mcp_stress_toggle");
    expect(flagged?.must_not_contain_postwrite_text).toEqual(
      expect.arrayContaining(r19WarningText)
    );
    expect(flagged?.must_not_contain_prewrite_text).toEqual(expect.arrayContaining(["PREWRITE CHECK: clean"]));

    const assertSafeScenario = (id: string) => {
      const scenario = byId.get(id);
      expect(scenario).toBeDefined();
      expect(scenario?.collision_item_id).toBe("input_boolean.mcp_stress_toggle");
      expect(scenario?.must_contain_prewrite_text).toEqual(expect.arrayContaining(["## Preview Payload", "PREWRITE CHECK: clean"]));
      expect(scenario?.must_contain_postwrite_text).toEqual(
        expect.arrayContaining(["Post-Write Review", "Findings", "Collision check"])
      );
      expect(scenario?.must_not_contain_prewrite_text).toEqual(
        expect.arrayContaining(["PREWRITE CHECK: R-19 detected", ...r19WarningText])
      );
      expect(scenario?.must_not_contain_prewrite_text).toEqual(expect.arrayContaining(r19WarningText));
      expect(scenario?.must_not_contain_postwrite_text).toEqual(expect.arrayContaining(r19WarningText));
    };

    assertSafeScenario("write-r19-safe-no-warning");
    assertSafeScenario("write-r19-safe-single-if-else");
    assertSafeScenario("write-r19-safe-explicit-elif-trigger-id");
    assertSafeScenario("write-r19-safe-else-extra-guard");
    assertSafeScenario("write-r19-safe-mode-selector-tree");
    assertSafeScenario("write-r19-safe-numeric-range-selector-tree");
    assertSafeScenario("write-r19-safe-time-window-selector-tree");
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
          text: "## Preview Payload\ntriggers:\nconditions:\nactions:\nPREWRITE CHECK: clean",
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
            `Post-Write Review\nFindings\nCollision check\nNOVA_WRITE_REVIEW_RESULT id=test automation_id=${automationId} status=ok`,
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
          text: "## Preview Payload\ntriggers:\nconditions:\nactions:\nPREWRITE CHECK: clean",
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
    expect(result.prewrite_text).not.toContain("## Preview Payload");

    rmSync(tempDir, { recursive: true, force: true });
  });
});
