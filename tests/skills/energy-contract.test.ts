import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const energySkill = readFileSync("skills/energy/SKILL.md", "utf8");
const energyReference = readFileSync("skills/energy/energy-reference.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("energy contract", () => {
  it("enforces read-merge-save against the full-list replace trap", () => {
    // save_prefs replaces every provided key's ENTIRE list (verified in core source).
    expect(energySkill).toContain("Fresh `energy/get_prefs` immediately before composing the save");
    expect(energySkill).toContain("never reuse an earlier read");
    expect(energySkill).toContain("send only the top-level keys you touched, each as its complete list");
    expect(energyReference).toContain("every key present in the message replaces its ENTIRE list");
    expect(energyReference).toContain("There is no per-item merge.");
  });

  it("verifies saves with read-back plus validate, never save success alone", () => {
    // Save accepts dangling statistic IDs silently — validate is the only proof.
    expect(energySkill).toContain("a successful save does NOT prove the config works");
    expect(energySkill).toContain("dangling statistic IDs are accepted silently");
    expect(energySkill).toContain(
      "confirm the invariant and that every untouched entry is deep-equal to the fresh pre-save read",
    );
    expect(energySkill).toContain("count before ± changes = count after");
  });

  it("defines initial setup and wholesale-replace tiers (red-team blockers)", () => {
    // ERR_NOT_FOUND means there is no list to read — the write flow must stay executable.
    expect(energySkill).toContain(
      "Initial setup after `ERR_NOT_FOUND`: treat every list as empty — compose only the keys being populated (count before = 0)",
    );
    // First grid source: nothing to detect the schema generation from.
    expect(energySkill).toContain("With no existing grid source to detect from, pick by HA version");
    // Emptying/replacing a whole list is destructive, not a natural-confirmation edit.
    expect(energySkill).toContain(
      "a save that empties or wholesale-replaces a list on a configured instance is destructive — typed token confirmation",
    );
    // The plausible wrong payload guess is echoing get_prefs back.
    expect(energyReference).toContain("Never echo the whole `get_prefs` object back as the save body");
    expect(energyReference).toContain('{"type":"energy/save_prefs","device_consumption"');
  });

  it("handles both grid schema generations without mixing them", () => {
    // HA 2026.3 migrated grid sources from flow_from/flow_to to a flat object.
    expect(energySkill).toContain("`flow_from` present = legacy nested; flat keys = HA 2026.3+");
    expect(energySkill).toContain("Emit the same generation; never mix.");
    expect(energySkill).toContain("Preserve every field you do not edit verbatim");
    expect(energyReference).toContain("Legacy nested (HA ≤ 2026.2)");
    expect(energyReference).toContain("Flat (HA 2026.3+");
    expect(energyReference).toContain("Grid statistic IDs must be unique across all grid sources.");
  });

  it("keeps cost configuration on HA's rails (red-team blockers)", () => {
    // Cost sensors are auto-created by HA; inventing stat_cost breaks the model.
    expect(energySkill).toContain("never invent `stat_cost`");
    expect(energySkill).toContain("`energy/info` → `cost_sensors`");
    expect(energySkill).toContain("at most one of `entity_energy_price`/`number_energy_price` per direction");
    expect(energySkill).toContain("never set price fields on an external statistic (`:` in the ID)");
    expect(energyReference).toContain("Price changes apply forward-only");
  });

  it("guards included_in_stat integrity that core does not validate", () => {
    expect(energySkill).toContain("`included_in_stat` must reference a `stat_consumption` present in the same list");
    expect(energyReference).toContain("Core does NOT validate `included_in_stat`");
    expect(energyReference).toContain("no cycle");
  });

  it("teaches the statistics contract exactly (change-type, local buckets, ms epoch)", () => {
    expect(energySkill).toContain('`types:["change"]`');
    expect(energySkill).toContain('`units:{"energy":"kWh"}`');
    expect(energyReference).toContain("Never derive consumption by subtracting raw `total_increasing` states");
    expect(energyReference).toContain("ms since epoch");
    expect(energyReference).toContain("weeks start Monday");
    expect(energyReference).toContain("DST days have 23/25 hours");
    expect(energyReference).toContain("retained only ~10 days");
    expect(energyReference).toContain("never assume cents vs. main currency unit");
    // Live-E2E finding: HA returns the bucket starting exactly at end_time.
    expect(energyReference).toContain("`end_time` is boundary-inclusive");
    expect(energyReference).toContain("drop buckets with `start >= end_time`");
    expect(energyReference).toContain("Resolve HA's timezone via `GET /api/config` → `time_zone`");
  });

  it("matches the HA frontend KPI formulas and labels approximations", () => {
    expect(energyReference).toContain("used_total = grid_in + solar + battery_out − grid_out − battery_in");
    expect(energyReference).toContain("(1 − min(1, grid_in / max(0, used_total))) × 100");
    expect(energyReference).toContain("approximation");
    expect(energyReference).toContain("battery homes always show a nonzero residual");
    // Ratio KPIs apply once to window totals; averaged per-bucket percentages are wrong.
    expect(energyReference).toContain("never average per-bucket percentages");
    expect(energyReference).toContain("report the KPI as not computable for that window, never Infinity/NaN");
  });

  it("draws honest analysis boundaries (do-not list)", () => {
    expect(energySkill).toContain("report peak watts from energy-only statistics (only hourly averages exist — say so)");
    expect(energySkill).toContain("present a partial day/month as a complete period");
    expect(energySkill).toContain("HA cost math never equals the utility bill");
    expect(energyReference).toContain("No exact bill reconstruction");
    expect(energyReference).toContain("No device-level disaggregation guesses");
    expect(energyReference).toContain("No forecast accuracy scoring without a stored day-ahead snapshot");
    // EV managers own their session history — read their statistics entities.
    expect(energySkill).toContain("rebuild an EV manager's session history");
    expect(energyReference).toContain("EV via manager (evcc or similar)");
  });

  it("treats an unconfigured dashboard as setup opportunity, not failure", () => {
    expect(energySkill).toContain('`ERR_NOT_FOUND` ("No prefs")');
    expect(energySkill).toContain("offer initial setup instead of failing");
  });

  it("says removing a device keeps its recorded statistics", () => {
    expect(energySkill).toContain("removing a device stops tracking only — its recorded statistics stay");
  });

  it("is wired into dispatch, capability map, availability table, and architecture doc", () => {
    expect(contextSkill).toContain(
      "| analyze energy usage, solar/battery/grid KPIs, per-device consumption or costs; edit energy dashboard sources/devices | `ha-nova:energy` |",
    );
    expect(contextSkill).toContain('"Add this plug to the energy dashboard"** → `ha-nova:energy`');
    expect(fallbackSkill).toContain("| Energy (analysis + source/device config) | Covered | energy |");
    expect(fallbackSkill).not.toContain("Energy Configuration -- RELAY-READY");
    expect(writeSafety).toContain(
      "| `energy` | change preview + read-back & validate verify | no (corrective save) | HA Backups |",
    );
    expect(architectureDoc).toContain("energy/SKILL.md");
  });

  it("teaches file-based relay payloads and localized output slots", () => {
    expect(energySkill).toMatch(/--(data-file|body-file|out)\b/);
    expect(energySkill).toContain("skills/ha-nova/output-rules.md");
    expect(energySkill).toContain("Use stable localized slot labels in this order; omit empty slots.");
  });
});
