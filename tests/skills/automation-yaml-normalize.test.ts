import { describe, expect, it } from "vitest";

import {
  normalizeAutomationYamlShape,
  toCanonicalAutomationYamlShape,
} from "../../src/skills/contracts/automation-yaml-normalize.js";

describe("automation yaml normalize", () => {
  it("normalizes legacy singular top-level keys", () => {
    const normalized = normalizeAutomationYamlShape({
      alias: "Legacy",
      trigger: [{ platform: "state", entity_id: "binary_sensor.motion" }],
      condition: [{ condition: "state", entity_id: "light.demo", state: "off" }],
      action: [{ service: "light.turn_on", target: { entity_id: "light.demo" } }],
      mode: "single",
    });

    expect(normalized.triggers).toHaveLength(1);
    expect(normalized.conditions).toHaveLength(1);
    expect(normalized.actions).toHaveLength(1);
  });

  it("normalizes canonical plural top-level keys", () => {
    const normalized = normalizeAutomationYamlShape({
      alias: "Canonical",
      triggers: [{ trigger: "state", entity_id: ["binary_sensor.motion"] }],
      conditions: [{ condition: "state", entity_id: "light.demo", state: "off" }],
      actions: [{ action: "light.turn_on", target: { entity_id: "light.demo" } }],
      mode: "single",
    });

    expect(normalized.triggers).toHaveLength(1);
    expect(normalized.conditions).toHaveLength(1);
    expect(normalized.actions).toHaveLength(1);
  });

  it("converts singular/plural variants to the same canonical shape", () => {
    const legacy = toCanonicalAutomationYamlShape({
      alias: "A",
      trigger: [{ platform: "state", entity_id: "binary_sensor.motion" }],
      condition: [{ condition: "state", entity_id: "light.demo", state: "off" }],
      action: [{ service: "light.turn_on", target: { entity_id: "light.demo" } }],
      mode: "single",
    });

    const modern = toCanonicalAutomationYamlShape({
      alias: "A",
      triggers: [{ platform: "state", entity_id: "binary_sensor.motion" }],
      conditions: [{ condition: "state", entity_id: "light.demo", state: "off" }],
      actions: [{ service: "light.turn_on", target: { entity_id: "light.demo" } }],
      mode: "single",
    });

    expect(legacy).toEqual(modern);
    expect(legacy.trigger).toBeUndefined();
    expect(legacy.condition).toBeUndefined();
    expect(legacy.action).toBeUndefined();
    expect(legacy.triggers).toHaveLength(1);
    expect(legacy.conditions).toHaveLength(1);
    expect(legacy.actions).toHaveLength(1);
  });
});
