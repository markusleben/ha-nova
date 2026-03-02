import { constants, existsSync, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-nova contract", () => {
  it("keeps router mirror in sync between repo and installed skill", () => {
    const repoSkill = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(repoSkill).toBe(installedSkill);
  });

  it("routes operations to consolidated skills", () => {
    const router = readFileSync("skills/ha-nova.md", "utf8");

    expect(router).toContain("use skill `ha-nova-write`");
    expect(router).toContain("use skill `ha-nova-read`");
    expect(router).toContain("use skill `ha-nova-entity-discovery`");
    expect(router).toContain("use skill `ha-nova-onboarding`");
    expect(router).not.toContain(".agents/skills/");
    expect(router).not.toContain("core/intents.md");
    expect(router).not.toContain("Lazy Discovery Protocol");
    expect(router).not.toContain("Orchestration Hard Gate");
  });

  it("keeps relay-bootstrap runtime prerequisite and safety baseline in router", () => {
    const router = readFileSync("skills/ha-nova.md", "utf8");

    expect(router).toContain("Runtime Prerequisite (macOS)");
    expect(router).toContain("Relay-only auth model");
    expect(router).toContain("~/.config/ha-nova/relay health");
    expect(router).toContain("npm run onboarding:macos");
    expect(router).not.toContain("git rev-parse");
    expect(router).not.toContain('eval "$(bash');
    expect(router).toContain("Quoting Reliability (Critical)");
    expect(router).toContain("Safety Baseline");
    expect(router).toContain("confirm:<token>");
    expect(router).toContain("Do not ask user to paste tokens in chat.");
  });

  it("defines compact preview block response format", () => {
    const router = readFileSync("skills/ha-nova.md", "utf8");

    expect(router).toContain("Response Format (Writes)");
    expect(router).toContain("Automation` or `Script");
    expect(router).toContain("Entities");
    expect(router).toContain("Behavior");
    expect(router).toContain("Suggested Enhancements");
    expect(router).toContain("Next Step");
  });

  it("keeps new reference files present", () => {
    const files = [
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
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
  });

  it("keeps all operational subskills concise (<600 words)", () => {
    const skills = [
      ".agents/skills/ha-nova-write/SKILL.md",
      ".agents/skills/ha-nova-read/SKILL.md",
      ".agents/skills/ha-nova-entity-discovery/SKILL.md",
      ".agents/skills/ha-nova-onboarding/SKILL.md",
    ];
    for (const file of skills) {
      const content = readFileSync(file, "utf8");
      const wordCount = content.trim().split(/\s+/).length;
      expect(wordCount, `${file} has ${wordCount} words`).toBeLessThan(600);
    }
  });

  it("keeps only 5 installable HA NOVA skills in source tree", () => {
    const files = [
      ".agents/skills/ha-nova/SKILL.md",
      ".agents/skills/ha-nova-write/SKILL.md",
      ".agents/skills/ha-nova-read/SKILL.md",
      ".agents/skills/ha-nova-entity-discovery/SKILL.md",
      ".agents/skills/ha-nova-onboarding/SKILL.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
      const content = readFileSync(file, "utf8");
      expect(content).toContain("ha-nova-managed-install repo_root:");
    }
  });

  it("enforces relay CLI bootstrap across all operational subskills", () => {
    const skills = [
      ".agents/skills/ha-nova-write/SKILL.md",
      ".agents/skills/ha-nova-read/SKILL.md",
      ".agents/skills/ha-nova-entity-discovery/SKILL.md",
      ".agents/skills/ha-nova-onboarding/SKILL.md",
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
});
