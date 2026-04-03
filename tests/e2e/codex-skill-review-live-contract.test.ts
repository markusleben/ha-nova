import { execFileSync } from "node:child_process";
import { constants, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

type ReviewScenarioDefinition = {
  id: string;
  prompt: string;
  must_contain_text: string[];
  must_not_contain_text?: string[];
  ordered_text?: string[];
  section_order?: string[];
  max_duration_sec?: number;
};

const cleanReviewText = "No issues found in this review.";

function extractShellFunction(content: string, name: string): string {
  const match = content.match(new RegExp(String.raw`${name}\(\)\s*\{[\s\S]*?\n\}`, "m"));

  if (!match) {
    throw new Error(`shell function not found: ${name}`);
  }

  return match[0] ?? "";
}

describe("codex review live e2e contract", () => {
  it("provides executable review live e2e harness script", () => {
    const file = "scripts/e2e/codex-ha-nova-review-live-e2e.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain('"codex"');
    expect(content).toContain('"exec"');
    expect(content).toContain("--json");
    expect(content).toContain("run_codex_with_timeout");
    expect(content).toContain("normalize_jsonl_transcript");
    expect(content).toContain("extract_last_agent_message");
    expect(content).toContain('Reading additional input from stdin...');
    expect(content).toContain("Never read installed skill copies from ~/.local/share/ha-nova/skills");
    expect(content).toContain("Read only repo-local files from this checkout when you need skill guidance.");
    expect(content).toContain("Use only the local repo skills plus the pasted YAML");
    expect(content).toContain("Do not browse the web");
    expect(content).toContain("Do not use Exa, Ref, web search, or official-doc lookup tools.");
    expect(content).toContain("Treat the local repo skill guidance as authoritative for this harness");
    expect(content).toContain("state the uncertainty from local context instead of researching");
    expect(content).toContain("Use the exact English standalone review headings on their own lines in this order");
    expect(content).toContain("every issue must use the review finding microformat");
    expect(content).toContain("count_external_research_hits");
    expect(content).toContain('(.type == "web_search")');
    expect(content).toContain('test("^(exa|Ref|web)$")');
    expect(content).toContain("search_query");
    expect(content).toContain("ref_read_url");
    expect(content).toContain("count_shell_network_hits");
    expect(content).toContain("count_onboarding_check_hits");
    expect(content).toContain("count_home_assistant_read_hits");
    expect(content).toContain("count_installed_skill_copy_hits");
    expect(content).toContain("normalize_match_text");
    expect(content).toContain('re.search(r"\\bpython3?\\b", command)');
    expect(content).toContain('re.search(r"\\b(requests|urllib|httpx)\\b", command)');
    expect(content).not.toContain("python(3)?[^[:cntrl:]]*(requests|urllib|httpx)");
    expect(content).toContain("(ha-nova|nova|onboarding)");
    expect(content).toContain("(doctor|ready|quick)");
    expect(content).toContain('scenario_prompt_lc');
    expect(content).toContain('allow_home_assistant_reads');
    expect(content).toContain('home assistant reads');
    expect(content).toContain('validation_error="home_assistant_read_detected"');
    expect(content).toContain("must_contain_text");
    expect(content).toContain("must_not_contain_text");
    expect(content).toContain("ordered_text");
    expect(content).toContain("section_order");
    expect(content).toContain("and . == floor");
    expect(content).toContain("Review scenario suite failed");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("unexpected_external_research_detected");
    expect(content).toContain("forbidden_onboarding_check_detected");
    expect(content).toContain("home_assistant_read_detected");
    expect(content).toContain("installed_skill_copy_used");
    expect(content).toContain("codex_execution_failed");
    expect(content).toContain("ordered_text_mismatch");
    expect(content).toContain("section_order_mismatch");
    expect(content).toContain("required_text_missing");
    expect(content).toContain("forbidden_text_present");
    expect(content).toContain("rule_code_marker_present");
    expect(content).toContain("missing_agent_message");
    expect(content).not.toContain('fromjson? | select(type == "object")');
  });

  it("ships focused standalone review live scenarios", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ReviewScenarioDefinition[];

    expect(Array.isArray(scenarios)).toBe(true);
    expect(scenarios.length).toBeGreaterThanOrEqual(11);

    const byId = new Map(scenarios.map((scenario) => [scenario.id, scenario]));

    const questioned = byId.get("review-question-not-confident-removal");
    expect(questioned).toBeDefined();
    expect(questioned?.must_contain_text).toEqual(
      expect.arrayContaining([
        "Review target",
        "Questions to consider",
        "Purpose is unclear; verify before removing it.",
        "Suggestions",
        "No confident suggestions.",
      ])
    );
    expect(questioned?.must_not_contain_text).toContain("Remove the redundant branch");
    expect(questioned?.must_not_contain_text).toEqual(
      expect.arrayContaining(["Simplify existing", "Fix existing", "Extend existing", "Add new"])
    );
    expect(questioned?.prompt).toContain("plausibly redundant but not confidently removable");
    expect(questioned?.section_order).toContain("Suggestions");
    expect(questioned?.section_order).toContain("Instant help");

    const ranked = byId.get("review-ranks-root-cause-before-watchdog");
    expect(ranked).toBeDefined();
    expect(ranked?.must_contain_text).toEqual(expect.arrayContaining(["Suggestions", "Fix existing", "Add new"]));
    expect(ranked?.ordered_text).toEqual(["Suggestions", "Fix existing", "Add new"]);
    expect(ranked?.section_order).toContain("Instant help");

    const emptyQuestions = byId.get("review-questions-section-not-needed");
    expect(emptyQuestions).toBeDefined();
    expect(emptyQuestions?.must_contain_text).toEqual(
      expect.arrayContaining(["Questions to consider", "No follow-up questions right now."])
    );
    expect(emptyQuestions?.prompt).toContain("do not invent speculative follow-up questions");
    expect(emptyQuestions?.section_order).toEqual(
      expect.arrayContaining(["Review target", "Questions to consider", "Summary", "Instant help"])
    );

    const r19Flagged = byId.get("review-r19-bare-else-trigger-flagged");
    expect(r19Flagged).toBeDefined();
    expect(r19Flagged?.must_contain_text).toEqual(
      expect.arrayContaining([
        "Findings",
        "Why:",
        "Fix:",
        "final else branch is only reached when the earlier entity-state branches are false",
        "Move the `trigger.id` check into an explicit `elif`",
        "Or refactor to `choose` + `condition: trigger`",
      ])
    );

    const r19Safe = byId.get("review-r19-safe-mode-selector-tree");
    expect(r19Safe).toBeDefined();
    expect(r19Safe?.must_contain_text).toEqual(expect.arrayContaining([cleanReviewText, "Safe pattern: mode selector tree."]));
    expect(r19Safe?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
    expect(r19Safe?.must_not_contain_text).toEqual(expect.arrayContaining(["R-19", "R19"]));

    const r19SafeNumericRange = byId.get("review-r19-safe-numeric-range-selector-tree");
    expect(r19SafeNumericRange).toBeDefined();
    expect(r19SafeNumericRange?.must_contain_text).toEqual(
      expect.arrayContaining([cleanReviewText, "Safe pattern: numeric range selector tree."])
    );
    expect(r19SafeNumericRange?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const r19SafeTimeWindow = byId.get("review-r19-safe-time-window-selector-tree");
    expect(r19SafeTimeWindow).toBeDefined();
    expect(r19SafeTimeWindow?.must_contain_text).toEqual(
      expect.arrayContaining([cleanReviewText, "Safe pattern: time window selector tree."])
    );
    expect(r19SafeTimeWindow?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const r19SafeSingleIfElse = byId.get("review-r19-safe-single-if-else");
    expect(r19SafeSingleIfElse).toBeDefined();
    expect(r19SafeSingleIfElse?.must_contain_text).toEqual(
      expect.arrayContaining([cleanReviewText, "Safe pattern: single if else."])
    );
    expect(r19SafeSingleIfElse?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const r19SafeExplicitElif = byId.get("review-r19-safe-explicit-elif-trigger-id");
    expect(r19SafeExplicitElif).toBeDefined();
    expect(r19SafeExplicitElif?.must_contain_text).toEqual(
      expect.arrayContaining([cleanReviewText, "Safe pattern: explicit elif trigger id."])
    );
    expect(r19SafeExplicitElif?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const r19SafeExtraGuard = byId.get("review-r19-safe-else-extra-guard");
    expect(r19SafeExtraGuard).toBeDefined();
    expect(r19SafeExtraGuard?.must_contain_text).toEqual(
      expect.arrayContaining([cleanReviewText, "Safe pattern: else extra guard."])
    );
    expect(r19SafeExtraGuard?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );

    const r19SafeChooseTrigger = byId.get("review-r19-safe-choose-condition-trigger");
    expect(r19SafeChooseTrigger).toBeDefined();
    expect(r19SafeChooseTrigger?.must_contain_text).toEqual(
      expect.arrayContaining([cleanReviewText, "Safe pattern: choose condition trigger routing."])
    );
    expect(r19SafeChooseTrigger?.must_not_contain_text).toContain(
      "final else branch is only reached when the earlier entity-state branches are false"
    );
  });

  it("does not treat dotted entity-id suffixes as rule-code markers", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");
    const containsRuleCodeMarkerFn = extractShellFunction(content, "contains_rule_code_marker");

    const output = execFileSync(
      "bash",
      [
        "-lc",
        `set -euo pipefail
${containsRuleCodeMarkerFn}
if contains_rule_code_marker 'sensor.h2 switch.r3'; then
  echo bad_entity_match
elif contains_rule_code_marker 'Warning: R-19 branch risk'; then
  echo exact_rule_match
else
  echo missed_rule_match
fi`,
      ],
      { encoding: "utf8" }
    );

    expect(output.trim()).toBe("exact_rule_match");
  });

  it("keeps user-facing standalone review scenarios free of rule-code markers", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ReviewScenarioDefinition[];

    const userFacingText = scenarios.flatMap((scenario) => scenario.must_contain_text);

    for (const text of userFacingText) {
      expect(text).not.toMatch(/\b(?:S|R|P|M|F|H)-\d{2}\b/);
      expect(text).not.toMatch(/\bR\d+\b/);
    }
  });

  it("uses integer-only max_duration_sec values in review live scenarios", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ReviewScenarioDefinition[];

    for (const scenario of scenarios) {
      if (scenario.max_duration_sec === undefined) {
        continue;
      }

      expect(Number.isInteger(scenario.max_duration_sec)).toBe(true);
      expect(scenario.max_duration_sec).toBeGreaterThan(0);
    }
  });

  it("aligns standalone review live scenarios to the 8-section single-target shape", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ReviewScenarioDefinition[];

    for (const scenario of scenarios) {
      expect(scenario.section_order).toEqual([
        "Review target",
        "Findings",
        "Collision check",
        "Conflicts",
        "Questions to consider",
        "Suggestions",
        "Summary",
        "Instant help",
      ]);
    }
  });

  it("detects multiline python network commands in the review live harness", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");

    expect(content).toContain('if re.search(r"\\bpython3?\\b", command) and re.search(r"\\b(requests|urllib|httpx)\\b", command)');
    expect(content).toContain("command_execution");
  });

  it("ignores codex transport noise during review JSONL normalization", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");
    const normalizeFn = extractShellFunction(content, "normalize_jsonl_transcript");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-review-jsonl-noise-"));
    const scriptFile = join(tempDir, "check.sh");
    const scenarioLog = join(tempDir, "scenario.jsonl");

    writeFileSync(
      scenarioLog,
      [
        "Reading additional input from stdin...",
        "2026-04-02T11:06:04.993936Z ERROR codex_core::tools::router: error=write_stdin failed: stdin is closed for this session; rerun exec_command with tty=true to keep stdin open",
        JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "ok" } }),
      ].join("\n") + "\n"
    );

    writeFileSync(
      scriptFile,
      `#!/usr/bin/env bash
set -euo pipefail
${normalizeFn}
normalize_jsonl_transcript "$1"`
    );

    const normalized = execFileSync("bash", [scriptFile, scenarioLog], { encoding: "utf8", stdio: "pipe" });
    expect(normalized.trim()).toBe(JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "ok" } }));

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("detects installed skill-copy reads in the review live harness", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");
    const countCommandHitsFn = extractShellFunction(content, "count_command_hits");
    const installedSkillCopyHitsFn = extractShellFunction(content, "count_installed_skill_copy_hits");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-review-installed-skill-"));
    const parsedLog = join(tempDir, "parsed.jsonl");
    const scriptFile = join(tempDir, "check.sh");

    writeFileSync(
      parsedLog,
      [
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: '/bin/zsh -lc "sed -n \'1,220p\' /Users/markus/.local/share/ha-nova/skills/review/SKILL.md"',
          },
        }),
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: '/bin/zsh -lc "sed -n \'1,220p\' skills/review/SKILL.md"',
          },
        }),
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: '/bin/zsh -lc "sed -n \'1,220p\' /home/alice/.local/share/ha-nova/skills/review/SKILL.md"',
          },
        }),
      ].join("\n") + "\n"
    );

    writeFileSync(
      scriptFile,
      `#!/usr/bin/env bash
set -euo pipefail
${countCommandHitsFn}
${installedSkillCopyHitsFn}
count_installed_skill_copy_hits "$1"`
    );

    const hits = execFileSync("bash", [scriptFile, parsedLog], { encoding: "utf8", stdio: "pipe" });
    expect(hits.trim()).toBe("2");

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("detects helper-script execution through wrapped shell commands", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");
    const helperScriptHitsFn = extractShellFunction(content, "count_helper_script_exec_hits");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-review-helper-script-"));
    const parsedLog = join(tempDir, "parsed.jsonl");
    const scriptFile = join(tempDir, "check.sh");

    writeFileSync(
      parsedLog,
      [
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: '/bin/zsh -lc "scripts/e2e/demo.sh"',
          },
        }),
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: "env bash scripts/dev/demo.sh",
          },
        }),
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: "env FOO=1 bash scripts/e2e/demo.sh",
          },
        }),
        JSON.stringify({
          type: "item.completed",
          item: {
            type: "command_execution",
            command: "timeout 10 bash scripts/dev/demo.sh",
          },
        }),
      ].join("\n") + "\n"
    );

    writeFileSync(
      scriptFile,
      `#!/usr/bin/env bash
set -euo pipefail
${helperScriptHitsFn}
count_helper_script_exec_hits "$1"`
    );

    const hits = execFileSync("bash", [scriptFile, parsedLog], { encoding: "utf8", stdio: "pipe" });
    expect(hits.trim()).toBe("4");

    rmSync(tempDir, { recursive: true, force: true });
  });

  it("normalizes escaped markdown backticks before required-text checks", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");
    const normalizeMatchTextFn = extractShellFunction(content, "normalize_match_text");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-review-normalize-match-"));
    const scriptFile = join(tempDir, "check.sh");
    const escapedText =
      "Fix: \\`Move the \\\\`trigger.id\\\\` check into an explicit \\\\`elif\\\\`. Or refactor to \\\\`choose\\\\` + \\\\`condition: trigger\\\\`.\\`";

    writeFileSync(
      scriptFile,
      `#!/usr/bin/env bash
set -euo pipefail
${normalizeMatchTextFn}
text="$1"
normalized="$(normalize_match_text "$text")"
printf '%s' "$normalized"`
    );

    const normalized = execFileSync("bash", [scriptFile, escapedText], { encoding: "utf8", stdio: "pipe" });
    expect(normalized).toContain("Move the `trigger.id` check into an explicit `elif`");
    expect(normalized).toContain("Or refactor to `choose` + `condition: trigger`");
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("validates ordered standalone headings against the real message text", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-e2e.sh", "utf8");
    const assertHeadingSequenceFn = extractShellFunction(content, "assert_heading_sequence");
    const tempDir = mkdtempSync(join(tmpdir(), "ha-nova-review-headings-"));
    const scriptFile = join(tempDir, "check.sh");

    writeFileSync(
      scriptFile,
      `#!/usr/bin/env bash
set -euo pipefail
${assertHeadingSequenceFn}
message=$'Review target\\n\\nFindings\\n\\nCollision check\\n\\nConflicts\\n\\nQuestions to consider\\n\\nSuggestions\\n\\nSummary\\n\\nInstant help'
assert_heading_sequence "$message" "Review target" "Findings" "Collision check" "Conflicts" "Questions to consider" "Suggestions" "Summary" "Instant help"`
    );

    execFileSync("bash", [scriptFile], { encoding: "utf8", stdio: "pipe" });
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("exposes npm command for review live harness", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["e2e:skill:codex:review"]).toBe(
      "bash scripts/e2e/codex-ha-nova-review-live-e2e.sh"
    );
  });
});
