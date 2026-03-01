import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-nova skill contract", () => {
  it("uses streaming jq limit in read-only fast shortcut", () => {
    const content = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("limit($limit;");
    expect(content).not.toContain(
      'map(select((.entity_id|type)=="string" and (.entity_id|startswith($domain))) | .entity_id)[:$limit][]'
    );
  });

  it("uses repo-root-aware onboarding env path in entities skill", () => {
    const content = readFileSync("skills/ha-entities.md", "utf8");

    expect(content).toContain('NOVA_REPO_ROOT="${NOVA_REPO_ROOT:-${HA_NOVA_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}}"');
    expect(content).toContain('"$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env');
    expect(content).not.toContain('eval "$(bash scripts/onboarding/macos-onboarding.sh env)"');
  });

  it("routes automation writes through best-practice refresh skill", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");

    expect(content).toContain("ha-automation-best-practices");
    expect(content).toContain("Automation create/update/delete");
  });

  it("enforces best-practice refresh gate before automation create/update writes", () => {
    const content = readFileSync("skills/ha-automation-crud.md", "utf8");

    expect(content).toContain("Required Companion Skill for Writes");
    expect(content).toContain("Enforce best-practice session refresh gate");
    expect(content).toContain("no best-practice session refresh -> no write");
  });
});
