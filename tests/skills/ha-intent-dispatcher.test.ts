import { describe, expect, it } from "vitest";

import { resolveIntentLoadPlan } from "../../nova/src/skills/contracts/intent-dispatcher.js";

describe("ha intent dispatcher", () => {
  it("builds deterministic ordered load list from companions + modules", () => {
    const matrix = new Map([
      [
        "automation.create",
        {
          companions: [
            "$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md",
            "$NOVA_REPO_ROOT/skills/ha-nova/best-practices.md",
          ],
          modules: [
            "$NOVA_REPO_ROOT/skills/ha-nova/agents/resolve-agent.md",
            "$NOVA_REPO_ROOT/skills/ha-nova/agents/apply-agent.md",
            "$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md",
          ],
        },
      ],
    ]);

    const plan = resolveIntentLoadPlan("automation.create", matrix);

    expect(plan.intent).toBe("automation.create");
    expect(plan.companions).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/best-practices.md",
    ]);
    expect(plan.modules).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-nova/agents/resolve-agent.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/agents/apply-agent.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md",
    ]);
    expect(plan.orderedLoadList).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/best-practices.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/agents/resolve-agent.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/agents/apply-agent.md",
    ]);
  });

  it("rejects unknown intents", () => {
    const matrix = new Map([
      ["automation.create", { companions: [], modules: [] }],
    ]);

    expect(() => resolveIntentLoadPlan("script.delete", matrix)).toThrowError(
      "Unknown intent: script.delete"
    );
  });
});
