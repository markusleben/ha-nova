import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha entity-discovery contract", () => {
  it("uses relay-cli bootstrap instead of repo-root env eval", () => {
    const content = readFileSync("skills/entity-discovery/SKILL.md", "utf8");

    expect(content).toContain("ha-nova relay health");
    expect(content).toContain("ha-nova setup");
    expect(content).not.toContain("git rev-parse");
    expect(content).not.toContain("macos-onboarding.sh");
  });

  it("prefers entity registry over get_states and handles ambiguity", () => {
    const content = readFileSync("skills/entity-discovery/SKILL.md", "utf8");

    expect(content).toContain("entity_registry/list");
    expect(content).toContain("Never dump raw");
    expect(content).toContain("never guess entity IDs");
    expect(content).toContain("ask one selection question");
    expect(content).toContain("count only the requested domain unless the user explicitly asks");
    expect(content).toContain("ha-nova relay jq --file <result-file> length");
    expect(content).toContain("bulk inventory by `prefix`, `domain`, `area`, or `label`");
    expect(content).toContain("skills/ha-nova/bulk-patterns.md");
    expect(content).toContain("matched count");
    expect(content).toContain('{"type":"search/related","item_type":"area","item_id":"<area_id>"}');
    expect(content).toContain("Resolve room name to area_id");
    expect(content).toContain("returns the entity array directly in `.data`");
    expect(content).toContain("not explicit `prefix` matching");
    expect(content).toContain("startswith(...)");
    expect(content).toContain("do not trim to 20 inside the initial selector filter");
    expect(content).toContain("dedupe first, then sort deterministically, then compute the exact matched count, then apply the 20-row display cap");
    expect(content).toContain("ha-nova relay ws --data-file <payload-file> --out <registry-file>");
    expect(content).toContain("ha-nova relay jq -r --file <registry-file> '.data.unique_id'");
    expect(content).toContain("cap displayed shortlist rows at 20 only after exact matched-count computation for bulk inventory");
    expect(content).toContain("Treat the response as a keyed object, not an array");
    expect(content).toContain("automation discovery uses `.data.automation`");
    expect(content).toContain("helper-area semantics are explicitly defined");
  });
});
