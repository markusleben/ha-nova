import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-entities contract", () => {
  it("uses repo-root-aware onboarding env path", () => {
    const content = readFileSync("skills/ha-entities.md", "utf8");

    expect(content).toContain('NOVA_REPO_ROOT="${NOVA_REPO_ROOT:-${HA_NOVA_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}}"');
    expect(content).toContain('"$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env');
    expect(content).not.toContain('eval "$(bash scripts/onboarding/macos-onboarding.sh env)"');
  });

  it("uses object-only state filters for ws get_states parsing", () => {
    const entities = readFileSync("skills/ha-entities.md", "utf8");

    expect(entities).toContain('select(type=="object" and (.entity_id|type)=="string")');
    expect(entities).toContain("Do not run schema-probing jq one-offs in normal flow");
  });
});
