import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import {
  type AvailabilityFixture,
  summarizeAvailability,
} from "./_health-availability-oracle.js";

describe("health availability source contract", () => {
  it("pins the executable fixture rules to the health skill source", () => {
    const health = readFileSync("skills/health/SKILL.md", "utf8");
    const availability = readFileSync(
      "skills/health/availability-analysis.md",
      "utf8",
    );
    const normalized = `${health}\n${availability}`.replace(/\s+/g, " ");
    for (const rule of [
      "Treat `unavailable` and `unknown` as entity states.",
      "`attributes.restored == true`",
      "Count the union once.",
      "Display at most five group details across both owners.",
      "hidden tie-breaker",
      "This Integration-entry selection is independent from the availability group-detail cap below.",
      "at least 80% of the",
      "the three largest groups",
      "shared by `Entities` and `Integrations`",
      "Transitional, disabled, unknown-state, loaded, and missing-entry-metadata groups stay contextual in `Entities`.",
      "Availability classification alone never changes overall",
      "Output is aggregate-only.",
      "Deterministic internal sorting happens before localization",
      "then any config-entry attention/failure state from Availability Analysis",
      "Render unrecognized config-entry state strings as a localized generic unknown state",
      "require `ok:true`, a 2xx REST `data.status`",
      "`^[a-z0-9_]{1,128}$`",
      "never echo or derive a visible label from the invalid value",
    ]) {
      expect(normalized).toContain(rule);
    }
    const oracle = readFileSync(
      "tests/skills/_health-availability-oracle.ts",
      "utf8",
    );
    expect(oracle).toContain("English-only semantic oracle");
    expect(oracle).not.toContain("localeCompare");
  });

  it("keeps classification contextual and overall status deterministic", () => {
    const fixture: AvailabilityFixture = {
      states: Array.from({ length: 3 }, (_, index) => ({
        entity_id: `sensor.private_${index}`,
        state: "unavailable",
      })),
      registry: Array.from({ length: 3 }, (_, index) => ({
        entity_id: `sensor.private_${index}`,
        config_entry_id: "entry-loaded",
        platform: "mqtt",
      })),
      entries: [
        {
          entry_id: "entry-loaded",
          domain: "mqtt",
          state: "loaded",
          title: "Private",
        },
      ],
      devices: [],
    };
    const ok = summarizeAvailability(fixture);
    expect(ok).toContain("Status: OK.");
    expect(ok).toContain("Next step: No immediate action found.");
    const attention = summarizeAvailability({ ...fixture, activeRepairs: 1 });
    expect(attention).toContain("Status: Attention.");
    expect(attention).toContain("Next step: Review active repairs.");

    const empty = summarizeAvailability({ states: [], entries: [] });
    expect(empty).toContain("0 unavailable, 0 unknown entity states.");
    expect(empty).not.toContain("Classification:");
    expect(empty).not.toContain("Device registry unavailable");
  });
});
