import { describe, expect, it } from "vitest";

import { summarizeAvailability } from "./_health-availability-oracle.js";
import {
  type RawHealthSources,
  unavailableHealthSources,
} from "./_health-source-oracle.js";

function rawHealthSources(withAvailabilityRows = false): RawHealthSources {
  return {
    config: { ok: true, data: { status: 200, body: {} } },
    components: { ok: true, data: { status: 200, body: [] } },
    states: {
      ok: true,
      data: {
        status: 200,
        body: withAvailabilityRows
          ? [{ entity_id: "sensor.private", state: "unavailable" }]
          : [],
      },
    },
    repairs: { ok: true, data: { issues: [] } },
    integrations: { ok: true, data: [] },
    "entity registry": { ok: true, data: [] },
    "device registry": { ok: true, data: [] },
    "system health": { ok: true, data: { events: [] } },
  };
}

describe("health source shape normalization", () => {
  it("distinguishes malformed required envelopes and conditional registries", () => {
    for (const source of [
      "config",
      "components",
      "states",
      "repairs",
      "integrations",
      "system health",
    ] as const) {
      const unavailableSources = unavailableHealthSources({
        ...rawHealthSources(),
        [source]: { data: { unexpected: true } },
      });
      expect(unavailableSources).toContain(source);
      const report = summarizeAvailability({
        states: [],
        entries: [],
        unavailableSources,
      });
      expect(report, source).toContain("Status: Limited.");
      expect(report, source).toContain(`Coverage unavailable: ${source}.`);
      expect(report, source).toContain(
        "Next step: Restore the unavailable source.",
      );
    }

    const candidate = [{ entity_id: "sensor.private", state: "unavailable" }];
    const entityRegistrySources = unavailableHealthSources({
      ...rawHealthSources(true),
      "entity registry": { data: { unexpected: true } },
    });
    const entityRegistryMissing = summarizeAvailability({
      states: candidate,
      entries: [],
      devices: [],
      unavailableSources: entityRegistrySources,
    });
    expect(entityRegistryMissing).toContain("Status: Limited.");
    expect(entityRegistryMissing).toContain(
      "Coverage unavailable: entity registry.",
    );

    const deviceRegistrySources = unavailableHealthSources({
      ...rawHealthSources(true),
      "device registry": { data: null },
    });
    const deviceRegistryMissing = summarizeAvailability({
      states: candidate,
      entries: [],
      registry: [],
      unavailableSources: deviceRegistrySources,
    });
    expect(deviceRegistryMissing).toContain("Device registry unavailable");
    expect(deviceRegistryMissing).toContain(
      "Coverage unavailable: device registry.",
    );
  });

  it("requires a successful standard envelope and REST status", () => {
    expect(
      unavailableHealthSources({
        ...rawHealthSources(),
        config: { ok: false, data: { status: 200, body: {} } },
      }),
    ).toContain("config");
    expect(
      unavailableHealthSources({
        ...rawHealthSources(),
        config: { ok: true, data: { status: 500, body: {} } },
      }),
    ).toContain("config");
  });

  it("rejects valid envelopes whose required rows have malformed shapes", () => {
    const cases: Array<[string, RawHealthSources]> = [
      [
        "states",
        {
          ...rawHealthSources(),
          states: {
            ok: true,
            data: {
              status: 200,
              body: [{ entity_id: "sensor.private", state: 42 }],
            },
          },
        },
      ],
      [
        "integrations",
        {
          ...rawHealthSources(),
          integrations: {
            ok: true,
            data: [
              {
                entry_id: "entry",
                domain: "private-host.local",
                state: "loaded",
              },
            ],
          },
        },
      ],
      [
        "entity registry",
        {
          ...rawHealthSources(true),
          "entity registry": {
            ok: true,
            data: [
              {
                entity_id: "sensor.private",
                platform: "private-host.local",
              },
            ],
          },
        },
      ],
      [
        "device registry",
        {
          ...rawHealthSources(true),
          "device registry": { ok: true, data: [{ unexpected: true }] },
        },
      ],
    ];
    for (const [source, raw] of cases) {
      expect(unavailableHealthSources(raw), source).toContain(source);
    }
  });
});
