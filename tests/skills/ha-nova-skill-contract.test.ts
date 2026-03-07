import { existsSync } from "node:fs";

import { describe, expect, it } from "vitest";
import { validateAndConsumeConfirmToken } from "../../nova/src/skills/contracts/confirm-token.js";
import { resolveIntentLoadPlan } from "../../nova/src/skills/contracts/intent-dispatcher.js";

describe("ha-nova contract suite compatibility shim", () => {
  it("keeps split contract suites present", () => {
    const files = [
      "tests/skills/ha-nova-contract.test.ts",
      "tests/skills/ha-entities-contract.test.ts",
      "tests/skills/ha-safety-contract.test.ts",
      "tests/skills/ha-cross-skill-integration.test.ts",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected split suite file to exist: ${file}`).toBe(true);
    }
  });

  it("keeps semantic smoke behavior for intent dispatch and confirm token contracts", () => {
    const matrix = new Map([
      [
        "automation.create",
        {
          companions: ["$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md"],
          modules: ["$NOVA_REPO_ROOT/skills/ha-nova/agents/resolve-agent.md"],
        },
      ],
    ]);

    const createPlan = resolveIntentLoadPlan("automation.create", matrix);
    expect(createPlan.companions).toEqual(["$NOVA_REPO_ROOT/skills/ha-nova/relay-api.md"]);
    expect(createPlan.modules).toEqual(["$NOVA_REPO_ROOT/skills/ha-nova/agents/resolve-agent.md"]);

    const staleToken = validateAndConsumeConfirmToken(
      "confirm:shim-token",
      {
        tokenId: "shim-token",
        issuedAtMs: 10_000,
        method: "POST",
        path: "/api/config/automation/config/demo",
        target: "automation.demo",
        previewDigest: "sha256:demo",
      },
      {
        nowMs: 10_000 + 600_001,
        ttlMs: 600_000,
        method: "POST",
        path: "/api/config/automation/config/demo",
        target: "automation.demo",
        previewDigest: "sha256:demo",
        usedTokenIds: new Set<string>(),
      }
    );

    expect(staleToken).toEqual({
      ok: false,
      reason: "stale",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
  });
});
