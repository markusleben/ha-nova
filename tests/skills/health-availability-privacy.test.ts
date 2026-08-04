import { describe, expect, it } from "vitest";

import { groupFixture } from "./_health-availability-fixtures.js";
import {
  type ConfigEntry,
  type RegistryRow,
  type StateRow,
  summarizeAvailability,
} from "./_health-availability-oracle.js";

// Privacy modes, identity rendering, and malformed-source handling —
// split from health-availability-edge-context per the file-size guideline.
describe("health availability privacy and source validity", () => {
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

  it("drops display names carrying forbidden privacy patterns", () => {
    const forbiddenNames = [
      "Cam at http://192.168.1.20/stream",
      "nas.local share",
      "10.0.0.7 probe",
      "pipe|delimiter",
      "api_key holder",
    ];
    const states: StateRow[] = forbiddenNames.map((name, index) => ({
      entity_id: `sensor.private_f${index}`,
      state: "unavailable",
      attributes: { friendly_name: name },
    }));
    const registry: RegistryRow[] = states.map((row) => ({
      entity_id: row.entity_id,
      config_entry_id: "private-entry",
      platform: "private_platform",
    }));
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
    for (const name of forbiddenNames) {
      expect(report).not.toContain(name);
    }
    expect(report).toContain("  - sensor.private_f0\n");
  });

  it("invalidates the whole device registry on one malformed row", () => {
    const report = summarizeAvailability({
      states: [{ entity_id: "sensor.private", state: "unavailable" }],
      registry: [
        {
          entity_id: "sensor.private",
          device_id: "private-device",
        },
      ],
      entries: [],
      devices: [
        { id: "private-device", name: "Private" },
        { id: "", name: "Malformed" },
      ],
    });
    expect(report).toContain("Status: Limited.");
    expect(report).toContain("Coverage unavailable: device registry");
    expect(report).not.toContain("1 known device-registry records");
  });
});
