import { describe, expect, it } from "vitest";

import { groupFixture } from "./_health-availability-fixtures.js";
import {
  type RegistryRow,
  type StateRow,
  summarizeAvailability,
} from "./_health-availability-oracle.js";

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
    // Codex R6: sibling truncation paths carry Progressive Detail too.
    expect(report).toContain(
      "2 device clusters omitted (3 entity states). Request the full device-subcluster list for all clusters; results may have changed (fresh live read).",
    );
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
