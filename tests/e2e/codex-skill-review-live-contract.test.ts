import { constants, readFileSync, statSync } from "node:fs";

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
    expect(content).toContain("extract_last_agent_message");
    expect(content).toContain("Use only the local repo skills plus the pasted YAML");
    expect(content).toContain("Do not browse the web");
    expect(content).toContain("Do not use Exa, Ref, web search, or official-doc lookup tools.");
    expect(content).toContain("Treat the local repo skill guidance as authoritative for this harness");
    expect(content).toContain("state the uncertainty from local context instead of researching");
    expect(content).toContain("count_external_research_hits");
    expect(content).toContain('(.type == "web_search")');
    expect(content).toContain('test("^(exa|Ref|web)$")');
    expect(content).toContain("search_query");
    expect(content).toContain("ref_read_url");
    expect(content).toContain("must_contain_text");
    expect(content).toContain("must_not_contain_text");
    expect(content).toContain("ordered_text");
    expect(content).toContain("section_order");
    expect(content).toContain("Review scenario suite failed");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("unexpected_external_research_detected");
    expect(content).toContain("ordered_text_mismatch");
    expect(content).toContain("section_order_mismatch");
    expect(content).toContain("required_text_missing");
    expect(content).toContain("forbidden_text_present");
    expect(content).toContain("missing_agent_message");
  });

  it("ships focused standalone review live scenarios", () => {
    const content = readFileSync("scripts/e2e/codex-ha-nova-review-live-scenarios.json", "utf8");
    const scenarios = JSON.parse(content) as ReviewScenarioDefinition[];

    expect(Array.isArray(scenarios)).toBe(true);
    expect(scenarios.length).toBe(3);

    const byId = new Map(scenarios.map((scenario) => [scenario.id, scenario]));

    const questioned = byId.get("review-question-not-confident-removal");
    expect(questioned).toBeDefined();
    expect(questioned?.must_contain_text).toEqual(
      expect.arrayContaining(["Review target", "Questions to consider", "Question, not confident recommendation"])
    );
    expect(questioned?.must_not_contain_text).toContain("Remove the redundant branch");
    expect(questioned?.section_order).toContain("Suggestions");

    const ranked = byId.get("review-ranks-root-cause-before-watchdog");
    expect(ranked).toBeDefined();
    expect(ranked?.must_contain_text).toEqual(expect.arrayContaining(["Suggestions", "Fix existing", "Add new"]));
    expect(ranked?.ordered_text).toEqual(["Suggestions", "Fix existing", "Add new"]);

    const emptyQuestions = byId.get("review-questions-section-not-needed");
    expect(emptyQuestions).toBeDefined();
    expect(emptyQuestions?.must_contain_text).toEqual(
      expect.arrayContaining(["Questions to consider", "not needed"])
    );
    expect(emptyQuestions?.section_order).toEqual(
      expect.arrayContaining(["Review target", "Questions to consider", "Summary"])
    );
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
