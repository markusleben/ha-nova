import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #596: a configured time window (statistics `max_age`, a bounded history
// query) is a MAXIMUM, not proof of coverage. Observed `age_coverage_ratio`
// values of 0.11-0.46 on freshly created 3h statistics helpers were still
// described as "3-hour trends". These pins hold the shared Time-Window
// Evidence contract and its wiring in the owning skills.

const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
const historySkill = readFileSync("skills/history/SKILL.md", "utf8");
const calibration = readFileSync(
  "skills/ha-nova/threshold-calibration.md",
  "utf8",
);
const energySkill = readFileSync("skills/energy/SKILL.md", "utf8");

describe("time-window honesty contract (#596)", () => {
  it("keeps the shared contract in write-safety and distinguishes window from coverage", () => {
    expect(writeSafety).toContain(
      "## Time-Window Evidence (window-derived claims)",
    );
    expect(writeSafety).toContain(
      "A configured window is a MAXIMUM, not proof of coverage",
    );
    // The full evidence distinction from the issue: configured window,
    // observed span, density, source validity, coverage attributes.
    expect(writeSafety).toContain(
      "the configured maximum window vs the actual oldest-to-newest sample span",
    );
    expect(writeSafety).toContain("sample count and density when available");
    expect(writeSafety).toContain(
      "source validity and `unknown`/`unavailable` states in the source series",
    );
    expect(writeSafety).toContain("`age_coverage_ratio` and `source_value_valid`");
  });

  it("reads coverage attributes before any full-window claim and qualifies partial coverage", () => {
    expect(writeSafety).toContain(
      "Read coverage and source-validity attributes when present BEFORE",
    );
    // Partial coverage gets qualified wording via a localized semantic slot.
    expect(writeSafety).toContain(
      '"based on samples covering X% of the window"',
    );
    expect(writeSafety).toContain("semantic slot, localized");
  });

  it("never presents one retained sample as stability across the window", () => {
    expect(writeSafety).toContain("a retained last sample (`keep_last_sample`)");
    expect(writeSafety).toContain(
      "proves persistence of that value, never stability",
    );
  });

  it("bounds claims by evidence for sparse, discontinuous, and Recorder-unavailable data", () => {
    expect(writeSafety).toContain(
      "sparse,\ndiscontinuous, or Recorder data is unavailable",
    );
    expect(writeSafety).toContain(
      "the observed evidence span —\nnot the configured window — bounds the claim",
    );
  });

  it("treats partial coverage after a helper write as advisory, never failure", () => {
    expect(writeSafety).toContain(
      "partial\ncoverage is an advisory in the result, never a verification failure",
    );
    // Helper post-write verification reads the attributes and stays passed.
    expect(helperSkill).toContain(
      "for `statistics` and `history_stats` writes, also read the linked entity's state attributes",
    );
    expect(helperSkill).toContain(
      "`skills/ha-nova/write-safety.md` → Time-Window Evidence",
    );
    expect(helperSkill).toContain(
      "partial window coverage (`age_coverage_ratio` below 1) or an invalid source is an advisory in the result, never a failed create",
    );
    expect(helperSkill).toContain(
      "never describe the value as covering the configured window without that coverage evidence",
    );
  });

  it("makes history workflows report the actual evidence span and gaps", () => {
    expect(historySkill).toContain(
      "The queried window is what was REQUESTED, not what was observed",
    );
    expect(historySkill).toContain(
      "report the actual first-to-last sample span and material gaps alongside the requested window",
    );
    expect(historySkill).toContain(
      'never phrase a result as covering "the last N days" when the evidence spans less',
    );
    expect(historySkill).toContain(
      "`skills/ha-nova/write-safety.md` → Time-Window Evidence",
    );
    // Pre-existing honesty duties the new rule builds on stay in place.
    expect(historySkill).toContain("notable gaps or bursts");
    expect(historySkill).toContain(
      "If the data is incomplete for the requested conclusion, say so explicitly.",
    );
  });

  it("wires calibration evidence to the same window-vs-coverage distinction", () => {
    expect(calibration).toContain("`write-safety.md` → Time-Window\n  Evidence");
    expect(calibration).toContain(
      "name the requested window AND the observed sample span",
    );
    expect(calibration).toContain(
      "never\n  present the configured window as observed coverage",
    );
  });

  it("keeps the energy partial-period rule that already covers this class", () => {
    expect(energySkill).toContain(
      "present a partial day/month as a complete period (label it and compare like-for-like)",
    );
  });
});
