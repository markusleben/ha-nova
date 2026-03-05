import { constants, existsSync, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-nova contract", () => {
  it("provides context skill with skill discovery table", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("ha-nova:write");
    expect(context).toContain("ha-nova:read");
    expect(context).toContain("ha-nova:service-call");
    expect(context).toContain("ha-nova:entity-discovery");
    expect(context).toContain("ha-nova:onboarding");
    expect(context).toContain("ha-nova:review");
    expect(context).toContain("Sub-skills are discovered independently");
    expect(context).not.toContain(".agents/skills/");
    expect(context).not.toContain("core/intents.md");
    expect(context).not.toContain("Lazy Discovery Protocol");
    expect(context).not.toContain("Orchestration Hard Gate");
  });

  it("keeps relay-bootstrap runtime prerequisite and safety baseline in context skill", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("Runtime Prerequisite (macOS)");
    expect(context).toContain("Relay-only auth model");
    expect(context).toContain("~/.config/ha-nova/relay health");
    expect(context).toContain("npm run onboarding:macos");
    expect(context).not.toContain("git rev-parse");
    expect(context).not.toContain('eval "$(bash');
    expect(context).toContain("Quoting Reliability (Critical)");
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
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
    }
  });

  it("documents relay API contract centrally", () => {
    const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");

    expect(relayApi).toContain("GET /health");
    expect(relayApi).toContain("POST /ws");
    expect(relayApi).toContain("POST /core");
    expect(relayApi).toContain("{ \"ok\": true, \"data\": ... }");
    expect(relayApi).toContain("/api/config/automation/config/{id}");
    expect(relayApi).toContain("/api/config/script/config/{id}");
    expect(relayApi).toContain("UPSTREAM_WS_ERROR");
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
    expect(resolve).toContain("~/.config/ha-nova/relay ws");
    expect(resolve).toContain("~/.config/ha-nova/relay core");
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
    expect(apply).toContain("~/.config/ha-nova/relay ws");
    expect(apply).toContain("~/.config/ha-nova/relay core");
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

    const review = readFileSync("skills/ha-nova/agents/review-agent.md", "utf8");

    expect(review).toContain("{DOMAIN}");
    expect(review).toContain("{TARGET_ID}");
    expect(review).toContain("{CONFIG}");
    expect(review).toContain("{MODE}");
    expect(review).toContain("~/.config/ha-nova/relay ws");
    expect(review).toContain("~/.config/ha-nova/relay core");
    expect(review).not.toContain("{RELAY_BASE_URL}");
    expect(review).not.toContain("{RELAY_AUTH_TOKEN}");
    expect(review).toContain("CONFIG_FINDINGS:");
    expect(review).toContain("COLLISION_SCAN:");
    expect(review).toContain("CONFLICTS:");
    expect(review).toContain("search/related");
    expect(review).toContain("complementary pair");
    expect(review).toContain("Flip-Flop");
    expect(review).toContain("Script-Specific");
    expect(review).toContain("selector:");
    expect(review).toContain("fields:");
    expect(review).toContain("| default(...)");
    expect(review).toContain("REVIEW_MODE:");
    expect(review).toContain("SUGGESTIONS:");
    expect(review).toContain("SUMMARY:");
    expect(review).toContain("post-write");
    expect(review).toContain("standalone");
    expect(review).toContain("Cascade");
    expect(review).toContain("Stale Helper");
    expect(review).toContain("Startup Flash");
  });

  it("keeps all operational subskills concise (<800 words)", () => {
    const skills = [
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
    ];
    for (const file of skills) {
      const content = readFileSync(file, "utf8");
      const wordCount = content.trim().split(/\s+/).length;
      expect(wordCount, `${file} has ${wordCount} words`).toBeLessThan(800);
    }
  });

  it("keeps all HA NOVA skills in source tree", () => {
    const files = [
      "skills/ha-nova/SKILL.md",
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/review/SKILL.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
      const content = readFileSync(file, "utf8");
      expect(content).not.toContain("__HA_NOVA_REPO_ROOT__");
      expect(content).not.toContain("ha-nova-managed-install");
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
      expect(content, `${file} should use relay CLI`).toContain("~/.config/ha-nova/relay");
      expect(content, `${file} should not use eval bootstrap`).not.toContain("macos-onboarding.sh");
      expect(content, `${file} should not use git rev-parse`).not.toContain("git rev-parse");
      expect(content, `${file} should not reference RELAY_BASE_URL`).not.toContain("RELAY_BASE_URL");
    }
  });

  it("keeps relay wrapper script present and executable", () => {
    const relayScript = "scripts/relay.sh";
    expect(existsSync(relayScript)).toBe(true);
    const mode = statSync(relayScript).mode;
    expect(mode & constants.S_IXUSR).toBeGreaterThan(0);
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
    expect(marketplace.plugins[0].version).toBe(expected);
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
