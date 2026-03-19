import { constants, existsSync, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-nova contract", () => {
  it("provides context skill with skill discovery table", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("ha-nova:write");
    expect(context).toContain("ha-nova:read");
    expect(context).toContain("ha-nova:helper");
    expect(context).toContain("ha-nova:service-call");
    expect(context).toContain("ha-nova:entity-discovery");
    expect(context).toContain("ha-nova:onboarding");
    expect(context).toContain("ha-nova:review");
    expect(context).toContain("ha-nova:fallback");
    expect(context).toContain("Sub-skills are discovered independently");
    expect(context).not.toContain(".agents/skills/");
    expect(context).not.toContain("core/intents.md");
    expect(context).not.toContain("Lazy Discovery Protocol");
    expect(context).not.toContain("Orchestration Hard Gate");
  });

  it("keeps sequential intent re-dispatch guidance in the context skill", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("re-evaluate intent once before continuing");
    expect(context).toContain("config change on automation/script → `ha-nova:write`");
    expect(context).toContain("helper change → `ha-nova:helper`");
    expect(context).toContain("automation/script: `entity_id`, `unique_id`, current config");
    expect(context).toContain("storage-based family: `entity_id`, helper type, internal helper id when already known");
    expect(context).toContain("config-entry family: `entry_id`, domain, title, linked entities when already known");
    expect(context).toContain("one skill at a time, never parallel");
  });

  it("uses App terminology in active fallback skill surfaces", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");

    expect(context).toContain('"How do I manage Apps?"');
    expect(context).not.toContain('"How do I manage add-ons?"');
    expect(fallback).toContain("Apps / Supervisor");
    expect(fallback).toContain("Settings > Apps");
    expect(fallback).not.toContain("Settings > Add-ons");
  });

  it("keeps relay-bootstrap runtime prerequisite and safety baseline in context skill", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("Runtime Prerequisite");
    expect(context).toContain("Relay-only auth model");
    expect(context).toContain("ha-nova relay health");
    expect(context).toContain("ha-nova setup");
    expect(context).not.toContain("git rev-parse");
    expect(context).not.toContain('eval "$(bash');
    expect(context).toContain("Quoting Reliability (Critical)");
    expect(context).toContain("--data-file");
    expect(context).toContain("--body-file");
    expect(context).toContain("--out");
    expect(context).toContain("ha-nova relay jq --file <result-file> length");
    expect(context).toContain("never chain commands with `&&` or `||`");
    expect(context).toContain("Never call external `jq`");
    expect(context).not.toContain("ask user to switch shell");
    expect(context).not.toContain("If shell is not bash-compatible, stop and ask user to switch shell.");
    expect(context).toContain("Safety Baseline");
    expect(context).toContain("confirm:<token>");
    expect(context).toContain("Do not ask user to paste tokens in chat.");
  });

  it("defines structured summary + YAML response format", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("Response Format");
    expect(context).toContain("Automation` or `Script");
    expect(context).toContain("Entities");
    expect(context).toContain("Triggers");
    expect(context).toContain("Actions");
    expect(context).toContain("full YAML config");
    expect(context).toContain("Next Step");
  });

  it("keeps new reference files present", () => {
    const files = [
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/ha-nova/agents/review-agent.md",
      "skills/review/checks.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
    }
  });

  it("documents relay API contract centrally", () => {
    const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");
    const apiMatrix = readFileSync("docs/reference/ha-api-matrix.md", "utf8");

    expect(relayApi).toContain("GET /health");
    expect(relayApi).toContain("POST /ws");
    expect(relayApi).toContain("POST /core");
    expect(relayApi).toContain("--data-file");
    expect(relayApi).toContain("--body-file");
    expect(relayApi).toContain("--out");
    expect(relayApi).toContain("--jq-file <filter-file>");
    expect(relayApi).not.toContain("ha-nova relay ws -d '{");
    expect(relayApi).toContain("{ \"ok\": true, \"data\": ... }");
    expect(relayApi).toContain("/api/config/automation/config/{id}");
    expect(relayApi).toContain("/api/config/script/config/{id}");
    expect(relayApi).toContain("UPSTREAM_WS_ERROR");
    expect(relayApi).toContain("Config writes/deletes");
    expect(relayApi).toContain("Post-Write Verification");
    expect(relayApi).toContain("Timeout and Retry Guidance");
    expect(relayApi).toContain("Safe Bulk Patterns");
    expect(relayApi).toContain("Do not rely on external `jq` pipes");
    expect(apiMatrix).toContain("Config Entry Flow");
    expect(apiMatrix).toContain("WS | `config_entries/get` | Retrieve config-entry metadata");
    expect(apiMatrix).toContain("`POST /api/config/config_entries/flow`");
    expect(apiMatrix).toContain("`POST /api/config/config_entries/flow/{flow_id}`");
    expect(apiMatrix).toContain("`DELETE /api/config/config_entries/entry/{entry_id}`");
    expect(apiMatrix).toContain("raw WS `config_entries/flow` did not succeed in this session");
  });

  it("documents tiered best-practice gate", () => {
    const bp = readFileSync("skills/ha-nova/best-practices.md", "utf8");

    expect(bp).toContain("Tiered policy for automation writes");
    expect(bp).toContain("Simple automation");
    expect(bp).toContain("Complex automation");
    expect(bp).toContain("hard gate");
    expect(bp).toContain("Enforcement Checklist");
  });

  it("keeps agent templates parameterized and structured", () => {
    const resolve = readFileSync("skills/ha-nova/agents/resolve-agent.md", "utf8");
    const apply = readFileSync("skills/ha-nova/agents/apply-agent.md", "utf8");

    expect(resolve).toContain("{DOMAIN}");
    expect(resolve).toContain("{OPERATION}");
    expect(resolve).toContain("{USER_INTENT}");
    expect(resolve).toContain("ha-nova relay ws");
    expect(resolve).toContain("ha-nova relay core");
    expect(resolve).not.toContain("{RELAY_BASE_URL}");
    expect(resolve).not.toContain("{RELAY_AUTH_TOKEN}");
    expect(resolve).not.toContain("macos-onboarding.sh");
    expect(resolve).not.toContain("git rev-parse");
    expect(resolve).toContain("RESOLVED_ENTITIES:");
    expect(resolve).toContain("No entities matching '{USER_INTENT}' found");
    expect(resolve).toContain("SUGGESTED_ENHANCEMENTS:");
    expect(resolve).toContain("toggle-stop");

    expect(apply).toContain("{TARGET_ID}");
    expect(apply).toContain("{PAYLOAD}");
    expect(apply).toContain("ha-nova relay ws");
    expect(apply).toContain("ha-nova relay core");
    expect(apply).not.toContain("{RELAY_BASE_URL}");
    expect(apply).not.toContain("{RELAY_AUTH_TOKEN}");
    expect(apply).not.toContain("macos-onboarding.sh");
    expect(apply).not.toContain("git rev-parse");
    expect(apply).toContain("RESULT:");
    expect(apply).toContain("reloaded:");
    expect(apply).toContain("VERIFICATION:");
    expect(apply).toContain("trigger` + `triggers");
    expect(apply).toContain("automation/reload");
    expect(apply).toContain("script/reload");
    expect(apply).toContain("`write`, `read-back`, `reload`, or `runtime-verify`");

    const review = readFileSync("skills/ha-nova/agents/review-agent.md", "utf8");

    expect(review).toContain("{DOMAIN}");
    expect(review).toContain("{TARGET_ID}");
    expect(review).toContain("{CONFIG}");
    expect(review).toContain("{MODE}");
    expect(review).toContain("ha-nova relay ws");
    expect(review).toContain("ha-nova relay core");
    expect(review).not.toContain("{RELAY_BASE_URL}");
    expect(review).not.toContain("{RELAY_AUTH_TOKEN}");
    expect(review).toContain("Output Format");
    expect(review).toContain("Output Localization");
    expect(review).toContain("search/related");
    expect(review).toContain("complementary pair");
    // Review entry stays at review/SKILL.md; detailed checks live in review/checks.md.
    expect(review).toContain("skills/review/SKILL.md");
    expect(review).toContain("skills/review/checks.md");
    expect(review).toContain("mode: post-write");
    expect(review).toContain("post-write");
    expect(review).toContain("standalone");
  });

  it("keeps all operational subskills concise (<1000 words)", () => {
    const skills = [
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
    ];
    for (const file of skills) {
      const content = readFileSync(file, "utf8");
      const wordCount = content.trim().split(/\s+/).length;
      expect(wordCount, `${file} has ${wordCount} words`).toBeLessThan(1000);
    }
  });

  it("keeps all HA NOVA skills in source tree", () => {
    const files = [
      "skills/ha-nova/SKILL.md",
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/review/SKILL.md",
      "skills/review/checks.md",
      "skills/fallback/SKILL.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
      const content = readFileSync(file, "utf8");
      expect(content).not.toContain("__HA_NOVA_REPO_ROOT__");
      expect(content).not.toContain("ha-nova-managed-install");
    }
  });

  it("enforces English-only content across all skill files", () => {
    const allSkillFiles = [
      "skills/ha-nova/SKILL.md",
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/review/SKILL.md",
      "skills/review/checks.md",
      "skills/fallback/SKILL.md",
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/payload-schemas.md",
      "skills/ha-nova/automation-patterns.md",
      "skills/ha-nova/template-guidelines.md",
      "skills/ha-nova/safe-refactoring.md",
      "skills/ha-nova/helper-schemas.md",
      "skills/ha-nova/helper-flow-schemas.md",
      "skills/ha-nova/update-guide.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/ha-nova/agents/review-agent.md",
    ];

    // German words/phrases that should never appear in skill files
    const germanPatterns = [
      /\bAnalysiere\b/,
      /\bZeige\b/,
      /\bErstelle\b/,
      /\bÄndere\b/,
      /\bSpeichere\b/,
      /\bImportiere\b/,
      /\bmeine\b/,
      /\bfehlt\b/,
      /\blöschen\b/,
      /\bBitte\b/,
      /\bBedingung\b/,
      /\bWohnzimmer\b/,
      /\bfunktioniert\b/,
      /\bgeht nicht\b/,
      /\bist falsch\b/,
    ];

    for (const file of allSkillFiles) {
      const content = readFileSync(file, "utf8");
      for (const pattern of germanPatterns) {
        expect(
          pattern.test(content),
          `${file} contains German text matching ${pattern}`,
        ).toBe(false);
      }
    }
  });

  it("enforces relay CLI bootstrap across all operational subskills", () => {
    const skills = [
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
    ];

    for (const file of skills) {
      const content = readFileSync(file, "utf8");
      expect(content, `${file} should use relay CLI`).toContain("ha-nova relay");
      expect(content, `${file} should not use eval bootstrap`).not.toContain("macos-onboarding.sh");
      expect(content, `${file} should not use git rev-parse`).not.toContain("git rev-parse");
      expect(content, `${file} should not reference RELAY_BASE_URL`).not.toContain("RELAY_BASE_URL");
    }
  });

  it("keeps active skills on a shell-agnostic relay contract", () => {
    const files = [
      "skills/read/SKILL.md",
      "skills/review/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/fallback/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/ha-nova/safe-refactoring.md",
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/ha-nova/agents/review-agent.md",
      "skills/write/SKILL.md",
      "skills/review/checks.md",
    ];

    for (const file of files) {
      const content = readFileSync(file, "utf8");
      expect(content, `${file} should teach file-based relay payloads`).toMatch(/--(data-file|body-file|out)\b/);
      expect(content, `${file} should not teach inline ws/core JSON as canonical path`).not.toContain("relay ws -d '{");
      expect(content, `${file} should not teach inline core JSON as canonical path`).not.toContain("relay core -d '{");
      expect(content, `${file} should not rely on /tmp`).not.toContain("/tmp/");
      expect(content, `${file} should not rely on python post-processing`).not.toContain("python -c");
      expect(content, `${file} should not rely on node post-processing`).not.toContain("node -e");
      expect(content, `${file} should not teach mktemp`).not.toContain("mktemp");
      expect(content, `${file} should not teach shell piping into relay jq`).not.toContain("| ha-nova relay jq");
      expect(content, `${file} should not teach shell heredocs`).not.toContain("<< 'EOF'");
      expect(content, `${file} should not teach shell heredocs`).not.toContain('<< "EOF"');
      expect(content, `${file} should not teach glob-sensitive inline jq selectors`).not.toContain("--jq .data[]");
    }

    const entityDiscovery = readFileSync("skills/entity-discovery/SKILL.md", "utf8");
    expect(entityDiscovery).toContain("/api/config/automation/config/{unique_id}");
    expect(entityDiscovery).toContain("--out <result-file>");
    expect(entityDiscovery).toContain("never chain commands with `&&` or `||`");
    expect(entityDiscovery).toContain("Never call external `jq`");

    const review = readFileSync("skills/review/SKILL.md", "utf8");
    expect(review).toContain("ha-nova relay health");
  });

  it("moves complex relay filtering to jq files and native file tools", () => {
    const jqFileExamples = [
      "skills/entity-discovery/SKILL.md",
      "skills/read/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/review/SKILL.md",
      "skills/write/SKILL.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/safe-refactoring.md",
      "skills/review/checks.md",
    ];

    for (const file of jqFileExamples) {
      const content = readFileSync(file, "utf8");
      expect(content, `${file} should teach --jq-file for complex filters`).toContain("--jq-file");
      expect(content, `${file} should not teach inline body extraction filters`).not.toContain(
        "--jq 'if .ok then .data.body",
      );
    }

    const updateGuide = readFileSync("skills/ha-nova/update-guide.md", "utf8");
    expect(updateGuide).toContain("ha-nova relay jq --file ~/.config/ha-nova/version.json .skill_version");
    expect(updateGuide).not.toContain("cat ~/.config/ha-nova/version.json");
    expect(updateGuide).toContain("Other clients use the same shared CLI updater path");

    const bestPractices = readFileSync("skills/ha-nova/best-practices.md", "utf8");
    expect(bestPractices).not.toContain('cat > "${HOME}/.cache/ha-nova/automation-bp-snapshot.json"');
    expect(bestPractices).toContain("native file-writing tool");

    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");
    expect(fallback).toContain("<history-path>");
    expect(fallback).toContain("<logbook-path>");
    expect(fallback).toContain("<calendar-events-path>");
    expect(fallback).not.toContain("--path '/api/history/");
    expect(fallback).not.toContain("--path '/api/logbook/");
    expect(fallback).not.toContain("--path '/api/calendars/");
  });

  it("keeps migration and search-related guidance in the shared refactoring doc", () => {
    const refactorGuide = readFileSync("skills/ha-nova/safe-refactoring.md", "utf8");

    expect(refactorGuide).toContain("Safe Migration Pattern");
    expect(refactorGuide).toContain("Review -> Write/Helper -> Verify -> Review");
    expect(refactorGuide).toContain("search/related Signal Strength");
    expect(refactorGuide).toContain("Helpers: strong signal");
    expect(refactorGuide).toContain("Scenes: weak signal");
  });

  it("avoids jq regex-dot escaping in helper-domain examples", () => {
    const files = [
      "skills/helper/SKILL.md",
      "skills/review/SKILL.md",
      "skills/ha-nova/safe-refactoring.md",
    ];

    for (const file of files) {
      const content = readFileSync(file, "utf8");
      expect(content, `${file} should avoid helper-domain regex escapes`).not.toContain(
        'test("^(input_boolean|input_number|input_text|input_select|input_datetime|input_button|counter|timer|schedule)\\\\.")',
      );
      expect(content, `${file} should use split-domain helper filtering`).toContain(
        'split(".")[0]',
      );
    }
  });

  it("keeps relay Go binary source present", () => {
    expect(existsSync("cli/main.go")).toBe(true);
    expect(existsSync("cli/relay.go")).toBe(true);
  });

  it("provides Claude Code plugin manifest", () => {
    const plugin = JSON.parse(readFileSync(".claude-plugin/plugin.json", "utf8"));
    expect(plugin.name).toBe("ha-nova");
    expect(plugin.description).toBeTruthy();
  });

  it("keeps all version files in sync with version.json", () => {
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    const expected = versionJson.skill_version;
    expect(expected).toMatch(/^\d+\.\d+\.\d+$/);

    const plugin = JSON.parse(readFileSync(".claude-plugin/plugin.json", "utf8"));
    expect(plugin.version).toBe(expected);

    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    expect(pkg.version).toBe(expected);

    const marketplace = JSON.parse(
      readFileSync(".claude-plugin/marketplace.json", "utf8"),
    );
    expect(marketplace.metadata.version).toBe(expected);
    expect(marketplace.metadata.description).toBeTruthy();
    expect(marketplace.plugins[0].source).toBe("./");
    expect(marketplace.plugins[0].version).toBe(expected);
  });

  it("tells non-Claude clients to run a quiet update check on first skill use", () => {
    const content = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(content).toContain("ha-nova check-update --quiet");
    expect(content).toContain("Before the first HA task in a session");
  });

  it("provides SessionStart hook for context skill auto-loading", () => {
    const hooksJson = JSON.parse(readFileSync("hooks/hooks.json", "utf8"));
    expect(hooksJson.hooks.SessionStart).toBeDefined();
    expect(hooksJson.hooks.SessionStart[0].matcher).toBe("startup|resume|clear|compact");

    const hookScript = "hooks/session-start";
    expect(existsSync(hookScript)).toBe(true);
    const mode = statSync(hookScript).mode;
    expect(mode & constants.S_IXUSR).toBeGreaterThan(0);
    const content = readFileSync(hookScript, "utf8");
    expect(content).toContain("skills/ha-nova/SKILL.md");
    expect(content).toContain("additional_context");
  });
});
