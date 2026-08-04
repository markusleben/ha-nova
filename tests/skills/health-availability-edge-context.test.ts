import { describe, expect, it } from "vitest";

import {
  type AvailabilityFixture,
  type ConfigEntry,
  type RegistryRow,
  type StateRow,
  summarizeAvailability,
} from "./_health-availability-oracle.js";

type GroupInput = {
  platform: string;
  count: number;
  state: string;
  disabled_by?: string | null;
  error_reason?: string;
  error_reason_translation_key?: string;
};

function groupFixture(groups: GroupInput[]): AvailabilityFixture {
  const states: StateRow[] = [];
  const registry: RegistryRow[] = [];
  const entries: ConfigEntry[] = [];
  for (const [groupIndex, group] of groups.entries()) {
    const entryID = `private-entry-${groupIndex}`;
    entries.push({
      entry_id: entryID,
      domain: group.platform,
      state: group.state,
      title: `Private ${groupIndex}`,
      ...(group.disabled_by !== undefined
        ? { disabled_by: group.disabled_by }
        : {}),
      ...(group.error_reason !== undefined
        ? { error_reason: group.error_reason }
        : {}),
      ...(group.error_reason_translation_key !== undefined
        ? {
            error_reason_translation_key: group.error_reason_translation_key,
          }
        : {}),
    });
    for (let rowIndex = 0; rowIndex < group.count; rowIndex += 1) {
      const entityID = `sensor.private_${groupIndex}_${rowIndex}`;
      states.push({ entity_id: entityID, state: "unavailable" });
      registry.push({
        entity_id: entityID,
        config_entry_id: entryID,
        platform: group.platform,
      });
    }
  }
  return { states, registry, entries, devices: [] };
}

describe("health availability adversarial edge fixtures", () => {
  it("routes one sorted budgeted group ledger across both owners", () => {
    const report = summarizeAvailability(
      groupFixture([
        { platform: "loaded_a", count: 10, state: "loaded" },
        { platform: "failed_a", count: 9, state: "setup_error" },
        { platform: "loaded_b", count: 8, state: "loaded" },
        { platform: "failed_b", count: 7, state: "setup_retry" },
        { platform: "loaded_c", count: 6, state: "loaded" },
        { platform: "failed_c", count: 5, state: "migration_error" },
        { platform: "loaded_d", count: 4, state: "loaded" },
        { platform: "failed_d", count: 3, state: "failed_unload" },
        { platform: "loaded_e", count: 2, state: "loaded" },
        { platform: "failed_e", count: 1, state: "not_loaded" },
      ]),
    );
    const detailedGroups = report
      .split("\n")
      .filter(
        (line) =>
          /entity states \(\d+ restored\)/.test(line) ||
          /; impact \d+ entity states/.test(line),
      );
    // #440: the 50-row Explained budget selects attention-joined groups
    // first, then other current groups until the budget is exhausted —
    // loaded_d (4) and loaded_e (2) no longer fit after 49 spent rows.
    expect(detailedGroups).toEqual([
      expect.stringMatching(/^loaded_a:/),
      expect.stringMatching(/^loaded_b:/),
      expect.stringMatching(/^loaded_c:/),
      expect.stringMatching(/^failed_a: setup_error; impact 9/),
      expect.stringMatching(/^failed_b: setup_retry; impact 7/),
      expect.stringMatching(/^failed_c: migration_error; impact 5/),
      expect.stringMatching(/^failed_d: failed_unload; impact 3/),
      expect.stringMatching(/^failed_e: not_loaded; impact 1/),
    ]);
    expect(report).toContain("2 group details omitted (6 entity states).");
    expect(report.match(/; impact \d+ entity states/g)).toHaveLength(5);
  });

  it("chooses attention entries by state priority independently of group size", () => {
    const report = summarizeAvailability(
      groupFixture([
        { platform: "retry", count: 10, state: "setup_retry" },
        { platform: "error", count: 1, state: "setup_error" },
      ]),
    );
    expect(report.indexOf("error: setup_error")).toBeLessThan(
      report.indexOf("retry: setup_retry"),
    );

    const capped = summarizeAvailability(
      groupFixture([
        { platform: "late_a", count: 10, state: "not_loaded" },
        { platform: "late_b", count: 9, state: "not_loaded" },
        { platform: "late_c", count: 8, state: "not_loaded" },
        { platform: "late_d", count: 7, state: "not_loaded" },
        { platform: "late_e", count: 6, state: "not_loaded" },
        { platform: "urgent", count: 1, state: "setup_error" },
      ]),
    );
    expect(capped).toContain("urgent: setup_error; impact 1 entity states");
    // #440: the integration cap is 25 rows — all six entries render, still
    // ordered by state priority (setup_error before every not_loaded).
    expect(capped).not.toContain("omitted by integration-entry cap");
    expect(capped.match(/not_loaded/g)).toHaveLength(5);
    // Budget: 10+9+8+7+6+1 = 41 rows fit the 50-row budget — no omission.
    expect(capped).not.toContain("group details omitted");
  });

  it("keeps transitions, unknown states, and disabled failures as context", () => {
    const contextual = summarizeAvailability(
      groupFixture([
        { platform: "one", count: 1, state: "setup_in_progress" },
        { platform: "two", count: 1, state: "unload_in_progress" },
        { platform: "three", count: 1, state: "future_state" },
        {
          platform: "four",
          count: 1,
          state: "setup_error",
          disabled_by: "user",
        },
      ]),
    );
    expect(contextual).toContain("Status: OK.");
    expect(contextual).toContain("population 4/4");
    expect(contextual).not.toContain("Review failed integrations.");

    const hostileUnknown = summarizeAvailability(
      groupFixture([
        {
          platform: "private",
          count: 1,
          state: "token-secret-at-private-host.local",
        },
      ]),
    );
    expect(hostileUnknown).toContain("unknown config-entry state");
    expect(hostileUnknown).not.toContain("token-secret-at-private-host.local");

    const failedUnload = summarizeAvailability(
      groupFixture([{ platform: "one", count: 1, state: "failed_unload" }]),
    );
    expect(failedUnload).toContain("Status: Attention.");
    expect(failedUnload).toContain("failed_unload; impact 1 entity states");
  });

  it("labels a completely joined failed cluster as explained, not insufficient", () => {
    const report = summarizeAvailability(
      groupFixture([{ platform: "hue", count: 3, state: "setup_error" }]),
    );
    expect(report).toContain(
      "Classification: fully explained by integration failures; population 0/3",
    );
    expect(report).not.toContain("insufficient registry evidence");
  });

  it("counts and clusters registered devices without integration attribution", () => {
    const report = summarizeAvailability({
      states: [{ entity_id: "sensor.private", state: "unavailable" }],
      registry: [
        {
          entity_id: "sensor.private",
          device_id: "private-device",
        },
      ],
      entries: [],
      devices: [{ id: "private-device", name: "Private" }],
    });
    expect(report).toContain(
      "1 known device-registry records; device attribution 1/1 entity states.",
    );
    expect(report).toContain("Largest device subclusters: 1 entity states");
    expect(report).not.toContain("private-device");
  });

  it("never emits hostile platform or config-entry domain values", () => {
    const report = summarizeAvailability({
      states: [{ entity_id: "sensor.private", state: "unavailable" }],
      registry: [
        {
          entity_id: "sensor.private",
          config_entry_id: "private-entry",
          platform: "private-host.local",
        },
      ],
      entries: [
        {
          entry_id: "private-entry",
          domain: "token-secret",
          state: "loaded",
          title: "Private",
        },
      ],
      devices: [],
    });
    expect(report).not.toContain("private-host.local");
    expect(report).not.toContain("token-secret");
    // Codex R4: one malformed registry row (unsafe platform) invalidates the
    // whole registry source — no derived group survives, coverage is limited.
    expect(report).toContain("Coverage unavailable: entity registry");
    expect(report).not.toContain("sensor:");

    const unjoinedAttention = summarizeAvailability({
      states: [],
      entries: [
        {
          entry_id: "private-entry",
          domain: "token-secret",
          state: "setup_error",
          title: "Private",
        },
      ],
      devices: [],
    });
    expect(unjoinedAttention).toContain(
      "unknown integration: setup_error; impact attribution unavailable",
    );
    expect(unjoinedAttention).not.toContain("token-secret");
  });

  it("reports omitted device-cluster and entity-state counts", () => {
    const clusterSizes = [5, 4, 3, 2, 1];
    const states: StateRow[] = [];
    const registry: RegistryRow[] = [];
    const devices = clusterSizes.map((count, clusterIndex) => {
      const deviceID = `private-device-${clusterIndex}`;
      for (let rowIndex = 0; rowIndex < count; rowIndex += 1) {
        const entityID = `sensor.private_${clusterIndex}_${rowIndex}`;
        states.push({ entity_id: entityID, state: "unavailable" });
        registry.push({ entity_id: entityID, device_id: deviceID });
      }
      return { id: deviceID, name: `Private ${clusterIndex}` };
    });
    const report = summarizeAvailability({
      states,
      registry,
      entries: [],
      devices,
    });
    expect(report).toContain("2 device clusters omitted (3 entity states).");
  });

  it("uses both sides of restored/current 60 percent thresholds", () => {
    // #440: only RESTORED tracker/stateless rows count as inventory.
    const states = Array.from({ length: 10 }, (_, index) => ({
      entity_id: `${index < 6 ? "device_tracker" : "sensor"}.private_${index}`,
      state: "unavailable",
      attributes: { restored: index < 6 },
    }));
    const registry = states.map((row, index) => ({
      entity_id: row.entity_id,
      config_entry_id: `entry-${index}`,
      platform: `platform_${index}`,
    }));
    const entries = states.map((_, index) => ({
      entry_id: `entry-${index}`,
      domain: `platform_${index}`,
      state: "loaded",
      title: `Private ${index}`,
    }));
    expect(
      summarizeAvailability({ states, registry, entries, devices: [] }),
    ).toContain("mostly restored or tracker-style inventory");
    states[5]!.entity_id = "sensor.private_5";
    states[5]!.attributes = { restored: false };
    registry[5]!.entity_id = "sensor.private_5";
    expect(
      summarizeAvailability({ states, registry, entries, devices: [] }),
    ).not.toContain("mostly restored or tracker-style inventory");

    // The #440 regression: CURRENT tracker rows are visible findings, never
    // harmless inventory.
    const currentTrackers = states.map((row, index) => ({
      ...row,
      entity_id: `device_tracker.current_${index}`,
      attributes: { restored: false },
    }));
    const currentReport = summarizeAvailability({
      states: currentTrackers,
      registry: currentTrackers.map((row, index) => ({
        entity_id: row.entity_id,
        config_entry_id: `entry-${index}`,
        platform: `platform_${index}`,
      })),
      entries,
      devices: [],
    });
    expect(currentReport).not.toContain(
      "mostly restored or tracker-style inventory",
    );
    expect(currentReport).toContain("tracker/presence 10");

    // #440: an 11-50 group in Private+Explained emits five prioritized
    // examples plus total/shown/omitted — never zero detail.
    const midGroup = summarizeAvailability(
      groupFixture([{ platform: "mid", count: 12, state: "loaded" }]),
    );
    expect(midGroup.match(/^ {2}- /gm) ?? []).toHaveLength(5);
    expect(midGroup).toContain("total 12, shown 5, omitted 7.");

    const broadStates = Array.from({ length: 10 }, (_, index) => ({
      entity_id: `sensor.broad_${index}`,
      state: "unavailable",
      attributes: { restored: index >= 6 },
    }));
    const broadRegistry = broadStates.map((row, index) => ({
      entity_id: row.entity_id,
      config_entry_id: `broad-${index}`,
      platform: `broad_${index}`,
    }));
    const broadEntries = broadStates.map((_, index) => ({
      entry_id: `broad-${index}`,
      domain: `broad_${index}`,
      state: "loaded",
      title: `Private ${index}`,
    }));
    expect(
      summarizeAvailability({
        states: broadStates,
        registry: broadRegistry,
        entries: broadEntries,
        devices: [],
      }),
    ).toContain("broad current availability problem");
    broadStates[5]!.attributes = { restored: true };
    expect(
      summarizeAvailability({
        states: broadStates,
        registry: broadRegistry,
        entries: broadEntries,
        devices: [],
      }),
    ).toContain("insufficient registry evidence");
  });

  it("sanitizes reasons and gives system-health failures a next step", () => {
    const report = summarizeAvailability(
      groupFixture([
        {
          platform: "safe",
          count: 1,
          state: "setup_error",
          error_reason_translation_key: "authentication_failed",
        },
        {
          platform: "secret",
          count: 1,
          state: "setup_retry",
          error_reason: "token secret at 192.0.2.44 private-host.local",
        },
      ]),
    );
    expect(report).toContain("authentication failed");
    expect(report).toContain("technical setup error");
    for (const secret of ["192.0.2.44", "private-host.local", "token secret"]) {
      expect(report).not.toContain(secret);
    }

    const hostileKey = summarizeAvailability(
      groupFixture([
        {
          platform: "private",
          count: 1,
          state: "setup_error",
          error_reason_translation_key: "192.0.2.44",
        },
      ]),
    );
    expect(hostileKey).toContain("technical setup error");
    expect(hostileKey).not.toContain("192.0.2.44");

    const systemHealth = summarizeAvailability({
      states: [],
      entries: [],
      failedSystemHealth: 1,
    });
    expect(systemHealth).toContain("Status: Attention.");
    expect(systemHealth).toContain("Next step: Review failed system health.");
  });

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

  it("replaces identities with deterministic per-type aliases in shareable mode", () => {
    const report = summarizeAvailability({
      ...groupFixture([{ platform: "one", count: 3, state: "loaded" }]),
      privacyMode: "shareable",
    });
    expect(report).toContain("  - sensor 1");
    expect(report).toContain("  - sensor 3");
    expect(report).not.toContain("sensor.private_0_0");
  });

  it("counts malformed state entity IDs separately and never renders them", () => {
    const fixture = groupFixture([
      { platform: "one", count: 2, state: "loaded" },
    ]);
    fixture.states.push({
      entity_id: "sensor.Secret-Host.local",
      state: "unavailable",
    });
    const report = summarizeAvailability(fixture);
    expect(report).not.toContain("Secret-Host");
    expect(report).toContain(
      "Invalid rows: 1 (excluded from reconciliation, never rendered).",
    );
    expect(report).toContain("2 entity states: unavailable 2");
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
    expect(manyGroups).toMatch(
      /group details omitted \(\d+ entity states\)\. Request a group's detail by name for its full rows; results may have changed \(fresh live read\)\./,
    );

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

  it("renders the safely renderable friendly name beside the exact ID in private mode", () => {
    const report = summarizeAvailability({
      states: [
        {
          entity_id: "sensor.private_named",
          state: "unavailable",
          attributes: { friendly_name: "Private kitchen sensor" },
        },
        {
          entity_id: "sensor.private_hostile",
          state: "unavailable",
          attributes: { friendly_name: "line\nbreak" },
        },
      ],
      registry: [
        {
          entity_id: "sensor.private_named",
          config_entry_id: "private-entry",
          platform: "private_platform",
        },
        {
          entity_id: "sensor.private_hostile",
          config_entry_id: "private-entry",
          platform: "private_platform",
        },
      ],
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
    expect(report).toContain("  - sensor.private_named (Private kitchen sensor)");
    // A control-character name is not safely renderable — ID only.
    expect(report).toContain("  - sensor.private_hostile\n");
    expect(report).not.toContain("line\nbreak");
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

  it("attributes nothing on a config_entry_id without a matching entry", () => {
    const report = summarizeAvailability({
      states: [{ entity_id: "sensor.private", state: "unavailable" }],
      registry: [
        {
          entity_id: "sensor.private",
          config_entry_id: "private-ghost-entry",
        },
      ],
      entries: [],
      devices: [],
    });
    expect(report).toContain("Registry match: 1/1; attribution: 0/1");
    expect(report).toContain("unattributed: 1");
  });

  it("uses code-point ordering for equal-count groups", () => {
    const report = summarizeAvailability(
      groupFixture([
        { platform: "zeta", count: 1, state: "loaded" },
        { platform: "alpha", count: 1, state: "loaded" },
      ]),
    );
    expect(report.indexOf("alpha:")).toBeLessThan(report.indexOf("zeta:"));
  });
});
