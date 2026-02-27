import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("codex live skill e2e contract", () => {
  it("provides executable codex live e2e harness script", () => {
    const file = "scripts/e2e/codex-ha-nova-live-skill-e2e.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain("codex exec");
    expect(content).toContain("--json");
    expect(content).toContain("ha-nova");
    expect(content).toContain("NOVA_SKILL_E2E_RESULT");
    expect(content).toContain("contributor validation mode");
    expect(content).toContain("Use explicit HA_LLAT direct REST capability");
    expect(content).toContain("Contributor live CRUD check requires HA_LLAT in shell env");
    expect(content).toContain("Do not run project helper scripts.");
    expect(content).toContain("E2E_SUBAGENT_POLICY");
    expect(content).toContain("/config/automation/config/${AUTOMATION_ID}");
    expect(content).toContain("/services/automation/reload");
    expect(content).not.toContain("/api/config/automation/config/${AUTOMATION_ID}");
    expect(content).not.toContain("/api/services/automation/reload");
  });

  it("exposes npm command for codex live e2e harness", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    expect(pkg.scripts?.["e2e:skill:codex"]).toBe(
      "bash scripts/e2e/codex-ha-nova-live-skill-e2e.sh"
    );
  });

  it("documents quick and live contributor checks", () => {
    const deployLoop = readFileSync("docs/contributor-deploy-loop.md", "utf8");
    expect(deployLoop).toContain("onboarding:macos:quick");
    expect(deployLoop).toContain("e2e:skill:codex");
  });
});
