// tests/skills/entity-discovery-truncation-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";
import { spawnSync } from "child_process";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

const FILTER = read("skills/entity-discovery/discovery-filter.jq");

describe("entity-discovery truncation contract (#582)", () => {
  it("pins the canonical discovery filter byte-exact", () => {
    // The cap is presentation-only: counts are computed BEFORE the slice, so
    // 20 shown rows are never ambiguous between "20 total" and "more than 20".
    expect(FILTER.trim()).toBe(
      '[.data.entities[] | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}]\n' +
        "| {total: length, shown: (.[0:20] | length), omitted: ([length - 20, 0] | max), truncated: (length > 20), matches: .[0:20]}",
    );
  });

  it("pins the swept sibling envelopes in read and helper", () => {
    const tail20 =
      "| {total: length, shown: (.[0:20] | length), omitted: ([length - 20, 0] | max), truncated: (length > 20), matches: .[0:20]}";
    const tail30 =
      "| {total: length, shown: (.[0:30] | length), omitted: ([length - 30, 0] | max), truncated: (length > 30), matches: .[0:30]}";
    const readSkill = read("skills/read/SKILL.md");
    const helper = read("skills/helper/SKILL.md");
    expect(readSkill.split(tail30)).toHaveLength(3); // automation + script list
    expect(readSkill).toContain(tail20); // keyword search
    expect(helper).toContain(tail20); // keyword search
    expect(helper).toContain(tail30); // inventory list
    // No bare display slice may survive in any swept file.
    for (const doc of [readSkill, helper, read("skills/entity-discovery/SKILL.md")]) {
      expect(doc).not.toMatch(/\| \.\[0:\d+\]\s*$/m);
    }
  });

  it("makes the skill fail closed while truncated is true", () => {
    const skill = flat(read("skills/entity-discovery/SKILL.md"));
    expect(skill).toContain("skills/entity-discovery/discovery-filter.jq");
    expect(skill).toContain("{total, shown, omitted, truncated, matches}");
    expect(skill).toContain(
      "the 20-row cap applies to `matches` only; the counts are exact",
    );
    expect(skill).toContain(
      "while `truncated` is true the result proves neither absence nor uniqueness",
    );
    expect(skill).toContain("until `truncated` is false before concluding either");
    expect(skill).toContain("never show an unfiltered full-registry dump");
  });

  // Execute the real filter where a jq binary exists (GitHub runners ship
  // one); the byte-pin above keeps the contract stable everywhere else.
  const hasJq = spawnSync("jq", ["--version"]).status === 0;

  const run = (matching: number, extra = 0): any => {
    const entities = [
      ...Array.from({ length: matching }, (_, i) => ({
        ei: `light.kitchen_${i}`,
        en: `Kitchen Light ${i}`,
        ai: "kitchen",
      })),
      ...Array.from({ length: extra }, (_, i) => ({
        ei: `sensor.other_${i}`,
        en: null,
        ai: null,
      })),
    ];
    const filter = FILTER.replaceAll("KEYWORD", "kitchen");
    const proc = spawnSync("jq", [filter], {
      input: JSON.stringify({ data: { entities } }),
      encoding: "utf-8",
    });
    expect(proc.status).toBe(0);
    return JSON.parse(proc.stdout);
  };

  it.skipIf(!hasJq)("reports exactly 20 matches as complete", () => {
    const out = run(20, 5);
    expect(out).toMatchObject({ total: 20, shown: 20, omitted: 0, truncated: false });
    expect(out.matches).toHaveLength(20);
  });

  it.skipIf(!hasJq)("flags 21 matches as truncated, never as absent", () => {
    // The intended target sits at position 21 — the filter must say the
    // result is incomplete instead of letting a resolver report it missing.
    const out = run(21);
    expect(out).toMatchObject({ total: 21, shown: 20, omitted: 1, truncated: true });
    expect(out.matches).toHaveLength(20);
  });

  it.skipIf(!hasJq)("keeps small results untruncated", () => {
    const out = run(3, 2);
    expect(out).toMatchObject({ total: 3, shown: 3, omitted: 0, truncated: false });
  });
});
