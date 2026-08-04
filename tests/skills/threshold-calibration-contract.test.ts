import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #484: changing how an automation classifies a physical process requires
// history-based calibration evidence in the preview — structurally valid
// threshold edits can silently remove false-positive guards.

const calibration = readFileSync(
  "skills/ha-nova/threshold-calibration.md",
  "utf8",
);
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

describe("threshold calibration preflight (#484)", () => {
  it("scopes the preflight to every wired target type", () => {
    expect(calibration).toContain("automation, script, or `threshold` helper");
    expect(calibration).toContain("Threshold helpers have no traces");
    expect(calibration).toContain("`numeric_state` trigger/condition `above` or `below`");
    expect(calibration).toContain("`for:` duration");
    expect(calibration).toContain("`wait_for_trigger` / `wait_template` timeout");
    // An unrelated numeric-state edit needs no history.
    expect(calibration).toContain("does NOT apply to unrelated");
    expect(calibration).toContain("classifies nothing and needs no history");
  });

  it("bounds the evidence and derives the ambiguity numbers honestly", () => {
    expect(calibration).toContain("up to 30 days, bounded reads");
    expect(calibration).toContain("recorder/statistics_during_period");
    expect(calibration).toContain("Never\n   unbounded");
    expect(calibration).toContain("LONGEST ambiguous phase");
    expect(calibration).toContain("data gaps");
    // Hourly aggregates only shortlist; durations carry their resolution.
    expect(calibration).toContain("SHORTLISTS candidate windows");
    expect(calibration).toContain("longest observed at the available");
    // "Still running" needs independent evidence or stays unverified.
    expect(calibration).toContain("needs independent evidence");
    expect(calibration).toContain("mark the\n   ambiguous phase as unverified");
    // Bounded reads of existing traces are the explicit no-auto-trace exception.
    expect(calibration).toContain("never\n   trigger a run to create one");
  });

  it("compares the right duration to the right evidence", () => {
    // Debounce vs timeout semantics (Codex round 3).
    expect(calibration).toContain("longest ambiguous phase");
    expect(calibration).toContain("start-to-crossing/completion latency");
    expect(calibration).toContain("falsely validate a timeout");
    // Every helper boundary field is value-domain validated.
    expect(calibration).toContain("EVERY `lower`, `upper`, or\n  `hysteresis` update");
  });

  it("makes the preview honest about both failure directions and thin evidence", () => {
    expect(calibration).toContain("BOTH failure directions");
    expect(calibration).toContain("fires too early");
    expect(calibration).toContain("fires late or never");
    // Insufficient history is stated plainly, never validated by silence.
    expect(calibration).toContain("Insufficient evidence");
    expect(calibration).toContain("never present an uncalibrated value as validated");
    // Guards survive thin evidence without an explicit user decision.
    expect(calibration).toContain("never removed on\n  insufficient evidence without an explicit user decision");
    // Read-only until the normal confirm flow.
    expect(calibration).toContain("read-only");
  });

  it("wires the preflight into the write and helper update flows", () => {
    expect(writeSkill).toContain("3d) Threshold Calibration (update only)");
    expect(writeSkill).toContain("skills/ha-nova/threshold-calibration.md");
    expect(writeSkill).toContain("longest ambiguous phase");
    expect(writeSkill).toContain("Unrelated numeric edits skip it.");
    expect(writeSkill).toContain("explicit exception to the no-auto-trace rule");
    const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
    expect(helperSkill).toContain("skills/ha-nova/threshold-calibration.md");
    expect(helperSkill).toContain("`lower`, `upper`, or `hysteresis`");
  });
});
