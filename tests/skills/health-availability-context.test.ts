import { describe, expect, it } from "vitest";

import {
  type AvailabilityFixture,
  type ConfigEntry,
  type DeviceRow,
  type RegistryRow,
  type StateRow,
  summarizeAvailability,
} from "./_health-availability-oracle.js";

function concentratedFixture(): AvailabilityFixture {
  const groups = [
    { platform: "hue", entry: "entry-z", count: 5, state: "setup_error" },
    { platform: "hue", entry: "entry-a", count: 2, state: "loaded" },
    { platform: "mqtt", entry: "entry-m", count: 2, state: "loaded" },
    { platform: "unifi", entry: "entry-u", count: 1, state: "loaded" },
    { platform: "automation", entry: "entry-b", count: 1, state: "loaded" },
    { platform: "zwave_js", entry: "entry-zv", count: 1, state: "loaded" },
    { platform: "esphome", entry: "entry-e", count: 1, state: "loaded" },
  ];
  const states: StateRow[] = [];
  const registry: RegistryRow[] = [];
  const entries: ConfigEntry[] = groups.map((group) => ({
    entry_id: group.entry,
    domain: group.platform,
    state: group.state,
    title: `Private account ${group.entry}`,
    error_reason: "token at 192.0.2.44",
  }));
  const devices: DeviceRow[] = [];

  for (const group of groups) {
    for (let index = 0; index < group.count; index += 1) {
      const domain =
        group.entry === "entry-u"
          ? "device_tracker"
          : group.entry === "entry-b"
            ? "button"
            : "sensor";
      const entityID = `${domain}.secret_${group.entry}_${index}`;
      const deviceID = `device-${group.entry}-${index % 2}`;
      states.push({
        entity_id: entityID,
        state: index % 3 === 0 ? "unknown" : "unavailable",
        attributes: {
          restored: group.entry === "entry-m",
          friendly_name: `Private room ${index}`,
        },
      });
      registry.push({
        entity_id: entityID,
        config_entry_id: group.entry,
        device_id: deviceID,
        platform: group.platform,
        name: `Private entity ${index}`,
      });
      devices.push({
        id: deviceID,
        name: `Private device ${index}`,
        configuration_url: "http://private-host.local",
      });
    }
  }
  states.push({
    entity_id: "sensor.unattributed_secret",
    state: "unavailable",
    attributes: { friendly_name: "Unattributed private sensor" },
  });
  return { states, registry, entries, devices };
}

describe("health availability context acceptance fixtures", () => {
  it("keeps high-fan-out failures aggregate, deterministic, capped, and private", () => {
    const fixture = concentratedFixture();
    const report = summarizeAvailability(fixture);

    expect(report).toContain(
      "Classification: broad current availability problem",
    );
    expect(report).toMatch(
      /hue entry 2: setup_error; technical setup error; impact 5 entity states/,
    );
    expect(report.match(/hue entry 2:/g)).toHaveLength(1);
    expect(report).toContain("Status: Attention.");
    expect(report).toContain("Next step: Review failed integrations.");
    expect(report).toContain(
      "Low-signal/stateless: 1 entity states, retained in the raw total.",
    );
    expect(report).toContain("Registry match: 13/14; attribution: 13/14");
    expect(report).toContain(
      "10 known device-registry records; device attribution 13/14 entity states.",
    );
    expect(report).toContain("unattributed: 1");
    expect(report).toContain("displayed group details cover");
    expect(report).toContain("group details omitted");
    expect(report).not.toContain("sensor.unattributed_secret:");
    for (const secret of [
      ...fixture.states.map((row) => row.entity_id),
      ...(fixture.registry ?? []).flatMap((row) => [
        row.config_entry_id ?? "",
        row.device_id ?? "",
        row.name ?? "",
      ]),
      ...fixture.entries.flatMap((entry) => [
        entry.entry_id,
        entry.title,
        entry.error_reason ?? "",
      ]),
      ...(fixture.devices ?? []).flatMap((device) => [
        device.id,
        device.name,
        device.configuration_url ?? "",
      ]),
    ].filter(Boolean)) {
      expect(report).not.toContain(secret);
    }
    for (const secretAtom of [
      "192.0.2.44",
      "private-host.local",
      "token",
      "Private account",
      "entry-z",
      "device-entry-z",
    ]) {
      expect(report).not.toContain(secretAtom);
    }
  });

  it("classifies restored tracker inventory before concentration", () => {
    const fixture: AvailabilityFixture = {
      states: Array.from({ length: 5 }, (_, index) => ({
        entity_id: `device_tracker.private_${index}`,
        state: "unavailable",
        attributes: { restored: index < 3 },
      })),
      registry: Array.from({ length: 5 }, (_, index) => ({
        entity_id: `device_tracker.private_${index}`,
        config_entry_id: "entry-private",
        platform: "mobile_app",
      })),
      entries: [
        {
          entry_id: "entry-private",
          domain: "mobile_app",
          state: "loaded",
          title: "Private Phone",
        },
      ],
      devices: [],
    };
    expect(summarizeAvailability(fixture)).toContain(
      "Classification: mostly restored or tracker-style inventory;",
    );
  });

  it("distinguishes broad current problems from insufficient attribution", () => {
    const broad: AvailabilityFixture = {
      states: Array.from({ length: 10 }, (_, index) => ({
        entity_id: `sensor.private_${index}`,
        state: "unavailable",
      })),
      registry: Array.from({ length: 10 }, (_, index) => ({
        entity_id: `sensor.private_${index}`,
        config_entry_id: `entry-${index}`,
        platform: `platform_${index}`,
      })),
      entries: Array.from({ length: 10 }, (_, index) => ({
        entry_id: `entry-${index}`,
        domain: `platform_${index}`,
        state: "loaded",
        title: `Private ${index}`,
      })),
      devices: [],
    };
    expect(summarizeAvailability(broad)).toContain(
      "Classification: broad current availability problem;",
    );
    expect(
      summarizeAvailability({
        states: broad.states,
        entries: broad.entries,
      }),
    ).toContain("Classification: insufficient registry evidence;");
  });

  it("uses exact integer 60/80 boundaries and excludes unattributed rows from group numerators", () => {
    const states = Array.from({ length: 10 }, (_, index) => ({
      entity_id: `sensor.boundary_${index}`,
      state: "unavailable",
    }));
    const registry = Array.from({ length: 8 }, (_, index) => ({
      entity_id: `sensor.boundary_${index}`,
      config_entry_id: `entry-${Math.floor(index / 2)}`,
      platform: `platform_${Math.floor(index / 2)}`,
    }));
    const entries = Array.from({ length: 4 }, (_, index) => ({
      entry_id: `entry-${index}`,
      domain: `platform_${index}`,
      state: "loaded",
      title: `Private ${index}`,
    }));
    const report = summarizeAvailability({
      states,
      registry,
      entries,
      devices: [],
    });
    expect(report).toContain(
      "Classification: concentrated integration/device clusters",
    );
    expect(report).toContain("Top three cover 6/10 (60%)");
    expect(report).toContain("attribution: 8/10");

    expect(
      summarizeAvailability({
        states,
        registry: registry.slice(0, 7),
        entries,
        devices: [],
      }),
    ).toContain("Classification: insufficient registry evidence");
  });

  it("does not let a failed tracker cluster masquerade as benign inventory", () => {
    const fixture: AvailabilityFixture = {
      states: Array.from({ length: 10 }, (_, index) => ({
        entity_id:
          index < 6
            ? `device_tracker.failed_${index}`
            : `sensor.loaded_${index}`,
        state: "unavailable",
      })),
      registry: Array.from({ length: 10 }, (_, index) => ({
        entity_id:
          index < 6
            ? `device_tracker.failed_${index}`
            : `sensor.loaded_${index}`,
        config_entry_id: index < 6 ? "entry-failed" : `entry-${index}`,
        platform: index < 6 ? "mobile_app" : `platform_${index}`,
      })),
      entries: [
        {
          entry_id: "entry-failed",
          domain: "mobile_app",
          state: "setup_retry",
          title: "Private Phone",
        },
        ...Array.from({ length: 4 }, (_, index) => ({
          entry_id: `entry-${index + 6}`,
          domain: `platform_${index + 6}`,
          state: "loaded",
          title: `Private ${index}`,
        })),
      ],
      devices: [],
    };
    const report = summarizeAvailability(fixture);
    expect(report).not.toContain(
      "Classification: mostly restored or tracker-style inventory",
    );
    expect(report).toContain("population 4/10");
    expect(report).toContain("setup_retry; impact 6 entity states");
  });

  it("disambiguates platform-only collisions and reports device row coverage", () => {
    const fixture: AvailabilityFixture = {
      states: ["a", "b", "c"].map((suffix) => ({
        entity_id: `sensor.${suffix}`,
        state: "unavailable",
      })),
      registry: [
        {
          entity_id: "sensor.a",
          config_entry_id: "entry-b",
          device_id: "shared-device",
          platform: "hue",
        },
        {
          entity_id: "sensor.b",
          config_entry_id: "entry-a",
          device_id: "shared-device",
          platform: "hue",
        },
        {
          entity_id: "sensor.c",
          device_id: "missing-device",
          platform: "hue",
        },
      ],
      entries: [
        {
          entry_id: "entry-a",
          domain: "hue",
          state: "loaded",
          title: "Private A",
        },
        {
          entry_id: "entry-b",
          domain: "hue",
          state: "loaded",
          title: "Private B",
        },
      ],
      devices: [{ id: "shared-device", name: "Private Device" }],
    };
    const report = summarizeAvailability(fixture);
    expect(report).toContain("hue entry 1:");
    expect(report).toContain("hue entry 2:");
    expect(report).toContain("hue (no config-entry attribution):");
    expect(report).toContain(
      "1 known device-registry records; device attribution 2/3 entity states.",
    );
  });

  it("covers failed states, disabled entries, missing metadata, and source limits", () => {
    const states = [
      "error",
      "retry",
      "migration",
      "not_loaded",
      "disabled",
      "missing",
    ].map((suffix) => ({
      entity_id: `sensor.${suffix}`,
      state: "unavailable",
    }));
    const registry = states.map((row, index) => ({
      entity_id: row.entity_id,
      config_entry_id:
        index === states.length - 1 ? "entry-missing" : `entry-${index}`,
      platform: `platform_${index}`,
    }));
    const entries: ConfigEntry[] = [
      { state: "setup_error", disabled_by: null },
      { state: "setup_retry", disabled_by: null },
      { state: "migration_error", disabled_by: null },
      { state: "not_loaded", disabled_by: null },
      { state: "not_loaded", disabled_by: "user" },
    ].map((entry, index) => ({
      entry_id: `entry-${index}`,
      domain: `platform_${index}`,
      title: `Private ${index}`,
      ...entry,
    }));
    const complete = summarizeAvailability({
      states,
      registry,
      entries,
      devices: [],
    });
    for (const state of [
      "setup_error",
      "setup_retry",
      "migration_error",
      "not_loaded",
      "intentionally disabled",
    ]) {
      expect(complete).toContain(state);
    }
    expect(complete).toContain("Status: Attention.");
    expect(
      summarizeAvailability({
        states: [states[states.length - 1]!],
        registry: [registry[registry.length - 1]!],
        entries,
        devices: [],
      }),
    ).toContain("entry metadata unavailable");

    const limited = summarizeAvailability({ states, entries });
    expect(limited).toContain("Status: Limited.");
    expect(limited).toContain("Device registry unavailable");
    expect(limited).toContain("Next step: Review failed integrations.");
  });
});
