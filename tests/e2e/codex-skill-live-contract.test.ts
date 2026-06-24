import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

function expectFragmentsInOrder(haystack: string, fragments: string[]) {
  let cursor = 0;
  for (const fragment of fragments) {
    const index = haystack.indexOf(fragment, cursor);
    expect(index).toBeGreaterThanOrEqual(0);
    cursor = index + fragment.length;
  }
}

function expectNoFullVitestSweep(verifyScript: string) {
  expect(verifyScript).not.toMatch(/(^|&& )npm run test:safe($| &&)/);
}

describe("codex live skill e2e contract", () => {
  it("provides executable codex live e2e harness script", () => {
    const file = "scripts/e2e/codex-ha-nova-live-skill-e2e.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    expect(content).toContain("codex exec");
    expect(content).toContain("--json");
    expect(content).toContain("NOVA_SKILL_E2E_RESULT");
    expect(content).toMatch(/Use Relay POST \/core envelope calls/i);
    expect(content).toMatch(/Do not use direct Home Assistant REST/i);
    expect(content).toContain("E2E_SUBAGENT_POLICY");
    expect(content).toContain("E2E_REQUIRE_QUICK_GATE");
    expect(content).toContain("fromjson? | select(type == \"object\")");
    expect(content).toMatch(/Parallelize independent stages/i);
    expect(content).toContain("simulates the user's next post-preview reply only for that exact preview");
    expect(content).not.toContain("explicit confirmation is granted by this prompt");
    expect(content).toMatch(/E2E_SUBAGENT_POLICY.*(deny|require)/);
    expect(content).toMatch(/relay \/core/i);
    expect(content).toMatch(/verify-absent/i);
    expect(content).toMatch(/PGPGDV/);
    expect(content).toContain("^[GV]?P+G+P+G+D+V+$");
    expect(content).toContain("^NOVA_SKILL_E2E_RESULT\\\\s+ok\\\\s+automation_id=");
    expect(content).toMatch(/bypassing relay \/core/i);
    expect(content).toContain("forbidden /core response redirection");
    expect(content).toContain("\\\"ok\\\"[[:space:]]*:[[:space:]]*true");
    expect(content).toContain("20(0|1)");
    expect(content).toContain("20(0|4)");
    expect(content).toContain("read GET(200) evidence");
    expect(content).toContain("scripts/smoke/|scripts/dev/|scripts/e2e/");
  });

  it("uses sequence regex that allows one optional pre-create read token", () => {
    const sequenceRegex = /^[GV]?P+G+P+G+D+V+$/;

    expect(sequenceRegex.test("PGPGDV")).toBe(true);
    expect(sequenceRegex.test("GPGPGDV")).toBe(true);
    expect(sequenceRegex.test("VPGPGDV")).toBe(true);

    expect(sequenceRegex.test("GGPGPGDV")).toBe(false);
    expect(sequenceRegex.test("GVPGPGDV")).toBe(false);
    expect(sequenceRegex.test("PGPDV")).toBe(false);
  });

  it("exposes npm command for codex live e2e harness", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    expect(pkg.scripts?.["e2e:skill:codex"]).toBe(
      "bash scripts/e2e/codex-ha-nova-live-skill-e2e.sh"
    );
  });

  it("exposes npm scripts for contributor verification and live e2e checks", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    expect(pkg.scripts?.["onboarding:macos:quick"]).toBeUndefined();
    const verify = pkg.scripts?.verify ?? "";
    expectFragmentsInOrder(verify, [
      "npm run verify:security",
      "bash scripts/release/verify-blocked-files.sh",
      "npm run typecheck",
      "npm run verify:docs",
      "npm run test:safe:core",
      "npm run verify:onboarding",
      "npm run build",
      "npm run test:cli",
      "npm run verify:release-contracts",
    ]);
    expectNoFullVitestSweep(verify);
    expect(verify).toContain("npm run verify:release-contracts");
    expect(verify).toContain("npm run verify:security");
    expect(pkg.scripts?.test).toBe("npm run test:safe");
    expect(pkg.scripts?.["test:safe:core"]).toBe("node scripts/test/run-safe-core.mjs");
    expect(pkg.scripts?.["verify:installers"]).toBe("npx vitest run tests/onboarding/installer-contract.test.ts");
    expect(pkg.scripts?.["e2e:skill:codex"]).toBeDefined();
  });
});
