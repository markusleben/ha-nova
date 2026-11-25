import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const flat = (file: string) => readFileSync(file, "utf8").replace(/\s+/g, " ");

describe("dead-reference sweep (#978)", () => {
  const bulk = flat("skills/ha-nova/bulk-patterns.md");
  it("checks every automation and script in one read-only call, outside the 5-target cap", () => {
    expect(bulk).toContain("## Dead-Reference Sweep (explicit request only)");
    expect(bulk).toContain("one `ha-nova get --dead-references > <scratch>.json` (it takes no `--out`)");
    expect(bulk).toContain("so the 5-target audit cap does not apply");
    expect(bulk).toContain('never say "nothing broken" while a row is not scanned');
    expect(bulk).toContain("Fixes are writes in their own flow, after the user picks them");
  });
  it("lets the Static Pass use the sweep instead of a get per id", () => {
    expect(bulk).toContain("the target's rows from one Dead-Reference Sweep (below) for the whole audit — never a per-id `ha-nova get`");
    expect(bulk).toContain("Its `not_scanned` rows count as not checked in the audit counts");
  });
  it("routes a broken-references question to the sweep before any shortlist", () => {
    expect(flat("skills/review/SKILL.md")).toContain("Asked only about broken or missing references: `../ha-nova/bulk-patterns.md` → Dead-Reference Sweep before any shortlist or Static Pass; its report is the answer.");
    expect(bulk).toContain("keep only rows whose `holder` is in that list");
    expect(bulk).toContain("when it is (also) in the blueprint itself, that fix changes every automation using it: say so, never in a batch");
  });
});
