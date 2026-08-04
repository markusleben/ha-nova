import { describe, expect, it } from "vitest";

import { groupFixture } from "./_health-availability-fixtures.js";
import {
  type ConfigEntry,
  type RegistryRow,
  type StateRow,
  summarizeAvailability,
} from "./_health-availability-oracle.js";

// Detail selection pools, budget pressure, and Progressive Detail
// follow-ups — split from health-availability-edge-context.
describe("health availability detail selection and budgets", () => {
  it("gives integration-attributed tracker groups first-pool selection under budget pressure", () => {
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    const entries: ConfigEntry[] = [];
    for (let groupIndex = 0; groupIndex < 10; groupIndex += 1) {
      const entryID = `private-sensor-${groupIndex}`;
      entries.push({
        entry_id: entryID,
        domain: "private_sensors",
        state: "loaded",
        title: `Private ${groupIndex}`,
      });
      for (let rowIndex = 0; rowIndex < 5; rowIndex += 1) {
        const entityID = `sensor.private_${groupIndex}_${rowIndex}`;
        states.push({ entity_id: entityID, state: "unavailable" });
        registry.push({
          entity_id: entityID,
          config_entry_id: entryID,
          platform: "private_sensors",
        });
      }
    }
    // Tracker rows attributed to a mobile_app config entry: the group label
    // is the integration platform, only its MEMBER domains reveal trackers.
    entries.push({
      entry_id: "private-tracker",
      domain: "mobile_app",
      state: "loaded",
      title: "Private tracker",
    });
    for (const suffix of ["a", "b", "c"]) {
      const entityID = `device_tracker.private_${suffix}`;
      states.push({ entity_id: entityID, state: "unavailable" });
      registry.push({
        entity_id: entityID,
        config_entry_id: "private-tracker",
        platform: "mobile_app",
      });
    }
    const report = summarizeAvailability({
      states,
      registry,
      entries,
      devices: [],
    });
    expect(report).toContain("device_tracker.private_a");
    expect(report).toContain("device_tracker.private_c");
    expect(report).toContain("group details omitted");
  });

  it("prioritizes current rows over restored rows in large-group examples regardless of input order", () => {
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    for (let rowIndex = 0; rowIndex < 7; rowIndex += 1) {
      states.push({
        entity_id: `sensor.private_r${rowIndex}`,
        state: "unavailable",
        attributes: { restored: true },
      });
    }
    for (let rowIndex = 0; rowIndex < 5; rowIndex += 1) {
      states.push({
        entity_id: `sensor.private_c${rowIndex}`,
        state: "unavailable",
      });
    }
    for (const row of states) {
      registry.push({
        entity_id: row.entity_id,
        config_entry_id: "private-entry",
        platform: "private_platform",
      });
    }
    const report = summarizeAvailability({
      states,
      registry,
      entries: [
        {
          entry_id: "private-entry",
          domain: "private_platform",
          state: "loaded",
          title: "Private",
        },
      ],
      devices: [],
    });
    for (const suffix of ["c0", "c1", "c2", "c3", "c4"]) {
      expect(report).toContain(`  - sensor.private_${suffix}`);
    }
    expect(report).not.toContain("sensor.private_r0");
    expect(report).toContain("total 12, shown 5, omitted 7.");
  });

  it("selects attention details in failure-priority order under budget pressure", () => {
    const report = summarizeAvailability(
      groupFixture([
        { platform: "nl_a", count: 10, state: "not_loaded" },
        { platform: "nl_b", count: 10, state: "not_loaded" },
        { platform: "nl_c", count: 10, state: "not_loaded" },
        { platform: "nl_d", count: 10, state: "not_loaded" },
        { platform: "nl_e", count: 10, state: "not_loaded" },
        { platform: "nl_f", count: 10, state: "not_loaded" },
        { platform: "urgent", count: 2, state: "setup_error" },
      ]),
    );
    expect(report).toContain("urgent: setup_error; impact 2 entity states");
    expect(report.match(/; impact \d+ entity states/g)).toHaveLength(5);
    expect(report.indexOf("urgent: setup_error; impact")).toBeLessThan(
      report.indexOf("nl_a: not_loaded; impact"),
    );
  });

  it("keeps restored-only inventory out of the current pool", () => {
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    const entries: ConfigEntry[] = [];
    for (let groupIndex = 0; groupIndex < 5; groupIndex += 1) {
      const entryID = `private-restored-${groupIndex}`;
      entries.push({
        entry_id: entryID,
        domain: `restored_${groupIndex}`,
        state: "loaded",
        title: `Private ${groupIndex}`,
      });
      for (let rowIndex = 0; rowIndex < 10; rowIndex += 1) {
        const entityID = `sensor.private_${groupIndex}_${rowIndex}`;
        states.push({
          entity_id: entityID,
          state: "unavailable",
          attributes: { restored: true },
        });
        registry.push({
          entity_id: entityID,
          config_entry_id: entryID,
          platform: `restored_${groupIndex}`,
        });
      }
    }
    entries.push({
      entry_id: "private-current",
      domain: "current_small",
      state: "loaded",
      title: "Private current",
    });
    for (const suffix of ["a", "b", "c"]) {
      const entityID = `sensor.private_current_${suffix}`;
      states.push({ entity_id: entityID, state: "unavailable" });
      registry.push({
        entity_id: entityID,
        config_entry_id: "private-current",
        platform: "current_small",
      });
    }
    const report = summarizeAvailability({
      states,
      registry,
      entries,
      devices: [],
    });
    expect(report).toContain("current_small: 3 entity states (0 restored)");
    expect(report).toContain("sensor.private_current_a");
    expect(report).toContain("5 group details omitted (50 entity states).");
  });

  it("ranks current stateless rows after current findings in examples", () => {
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    for (let rowIndex = 0; rowIndex < 4; rowIndex += 1) {
      states.push({
        entity_id: `button.private_b${rowIndex}`,
        state: "unknown",
      });
    }
    for (let rowIndex = 0; rowIndex < 5; rowIndex += 1) {
      states.push({
        entity_id: `sensor.private_s${rowIndex}`,
        state: "unavailable",
      });
    }
    for (let rowIndex = 0; rowIndex < 3; rowIndex += 1) {
      states.push({
        entity_id: `sensor.private_r${rowIndex}`,
        state: "unavailable",
        attributes: { restored: true },
      });
    }
    for (const row of states) {
      registry.push({
        entity_id: row.entity_id,
        config_entry_id: "private-entry",
        platform: "private_platform",
      });
    }
    const report = summarizeAvailability({
      states,
      registry,
      entries: [
        {
          entry_id: "private-entry",
          domain: "private_platform",
          state: "loaded",
          title: "Private",
        },
      ],
      devices: [],
    });
    for (const suffix of ["s0", "s1", "s2", "s3", "s4"]) {
      expect(report).toContain(`  - sensor.private_${suffix}`);
    }
    expect(report).not.toContain("  - button.private_b0");
    expect(report).toContain("total 12, shown 5, omitted 7.");
  });

  it("renders owned entity details for displayed failed integrations", () => {
    const report = summarizeAvailability(
      groupFixture([{ platform: "hue", count: 3, state: "setup_error" }]),
    );
    expect(report).toContain("hue: setup_error; impact 3 entity states");
    for (const idx of [0, 1, 2]) {
      expect(report).toContain(`  - sensor.private_0_${idx}`);
    }
  });

  it("orders the tracker pool by finding priority under budget pressure", () => {
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    const entries: ConfigEntry[] = [];
    for (let groupIndex = 0; groupIndex < 5; groupIndex += 1) {
      const entryID = `private-tracker-${groupIndex}`;
      entries.push({
        entry_id: entryID,
        domain: `tracker_${groupIndex}`,
        state: "loaded",
        title: `Private ${groupIndex}`,
      });
      for (let rowIndex = 0; rowIndex < 10; rowIndex += 1) {
        const entityID = `device_tracker.private_${groupIndex}_${rowIndex}`;
        states.push({ entity_id: entityID, state: "unavailable" });
        registry.push({
          entity_id: entityID,
          config_entry_id: entryID,
          platform: `tracker_${groupIndex}`,
        });
      }
    }
    entries.push({
      entry_id: "private-urgent",
      domain: "tracker_urgent",
      state: "setup_error",
      title: "Private urgent",
    });
    states.push({
      entity_id: "device_tracker.private_urgent",
      state: "unavailable",
    });
    registry.push({
      entity_id: "device_tracker.private_urgent",
      config_entry_id: "private-urgent",
      platform: "tracker_urgent",
    });
    const report = summarizeAvailability({
      states,
      registry,
      entries,
      devices: [],
    });
    expect(report).toContain(
      "tracker_urgent: setup_error; impact 1 entity states",
    );
    expect(report).toContain("  - device_tracker.private_urgent");
    expect(report).toContain("group details omitted");
  });

  it("attaches exact follow-ups to every truncation path", () => {
    const largeGroup = summarizeAvailability(
      groupFixture([{ platform: "one", count: 12, state: "loaded" }]),
    );
    expect(largeGroup).toContain(
      "total 12, shown 5, omitted 7. Request this group's full detail for all rows; results may have changed (fresh live read).",
    );

    const manyGroups = summarizeAvailability(
      groupFixture(
        Array.from({ length: 11 }, (_, index) => ({
          platform: `many_${String.fromCharCode(97 + index)}`,
          count: 10,
          state: "loaded",
        })),
      ),
    );
    // Codex R6: every omitted group appears in the catalog with its own
    // counts and a label-addressed request; the aggregate line closes.
    expect(manyGroups).toMatch(
      /many_k: total 10, shown 0, omitted 10\. Request the "many_k" group's detail for its rows; results may have changed \(fresh live read\)\./,
    );
    expect(manyGroups).toMatch(/group details omitted \(\d+ entity states\)\./);

    const manyAttention = summarizeAvailability(
      groupFixture(
        Array.from({ length: 26 }, (_, index) => ({
          platform: `attn_${String.fromCharCode(97 + index)}`,
          count: 1,
          state: "setup_error",
        })),
      ),
    );
    expect(manyAttention).toContain(
      "1 attention entries omitted by integration-entry cap. Request the full integration-entry list for the rest; results may have changed (fresh live read).",
    );
  });

  it("prioritizes current findings over stateless context in the current pool", () => {
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    const entries: ConfigEntry[] = [];
    for (let groupIndex = 0; groupIndex < 5; groupIndex += 1) {
      const entryID = `private-buttons-${groupIndex}`;
      entries.push({
        entry_id: entryID,
        domain: `buttons_${groupIndex}`,
        state: "loaded",
        title: `Private ${groupIndex}`,
      });
      for (let rowIndex = 0; rowIndex < 10; rowIndex += 1) {
        const entityID = `button.private_${groupIndex}_${rowIndex}`;
        states.push({ entity_id: entityID, state: "unknown" });
        registry.push({
          entity_id: entityID,
          config_entry_id: entryID,
          platform: `buttons_${groupIndex}`,
        });
      }
    }
    entries.push({
      entry_id: "private-finding",
      domain: "finding_small",
      state: "loaded",
      title: "Private finding",
    });
    for (const suffix of ["a", "b"]) {
      const entityID = `sensor.private_finding_${suffix}`;
      states.push({ entity_id: entityID, state: "unavailable" });
      registry.push({
        entity_id: entityID,
        config_entry_id: "private-finding",
        platform: "finding_small",
      });
    }
    const report = summarizeAvailability({
      states,
      registry,
      entries,
      devices: [],
    });
    expect(report).toContain("finding_small: 2 entity states (0 restored)");
    expect(report).toContain("  - sensor.private_finding_a");
    expect(report).toContain("group details omitted");
  });
});
