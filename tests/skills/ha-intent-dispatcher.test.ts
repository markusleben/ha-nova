import { describe, expect, it } from "vitest";

import { resolveIntentLoadPlan } from "../../src/skills/contracts/intent-dispatcher.js";
import { expectedIntentMatrix } from "./helpers/expected-intent-matrix.js";
import { parseIntentMatrix } from "./helpers/intent-matrix.js";

function buildOrderedLoadList(companions: string[], modules: string[]): string[] {
  return [...new Set<string>([...companions, ...modules])];
}

describe("ha intent dispatcher", () => {
  it("resolves exact companion/module load sets for all supported intents", () => {
    const matrix = parseIntentMatrix();

    expect([...matrix.keys()].sort()).toEqual([...expectedIntentMatrix.keys()].sort());

    for (const [intent, expectedPlan] of expectedIntentMatrix.entries()) {
      const plan = resolveIntentLoadPlan(intent, matrix);

      expect(plan.intent).toBe(intent);
      expect(plan.companions).toEqual(expectedPlan.companions);
      expect(plan.modules).toEqual(expectedPlan.modules);
      expect(plan.orderedLoadList).toEqual(
        buildOrderedLoadList(expectedPlan.companions, expectedPlan.modules)
      );
    }
  });

  it("preserves read/list separation and rejects unknown intents", () => {
    const matrix = parseIntentMatrix();

    const automationRead = resolveIntentLoadPlan("automation.read", matrix);
    const automationList = resolveIntentLoadPlan("automation.list", matrix);
    const scriptRead = resolveIntentLoadPlan("script.read", matrix);
    const scriptList = resolveIntentLoadPlan("script.list", matrix);

    expect(automationRead.modules).not.toEqual(automationList.modules);
    expect(scriptRead.modules).not.toEqual(scriptList.modules);
    expect(automationList.modules).toEqual(["$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/read.md"]);
    expect(scriptList.modules).toEqual(["$NOVA_REPO_ROOT/skills/ha-nova/modules/script/read.md"]);

    expect(() => resolveIntentLoadPlan("scene.create", matrix)).toThrowError("Unknown intent: scene.create");
  });
});
