import { existsSync } from "node:fs";

import { describe, expect, it } from "vitest";
import { validateAndConsumeConfirmToken } from "../../src/skills/contracts/confirm-token.js";
import { resolveIntentLoadPlan } from "../../src/skills/contracts/intent-dispatcher.js";
import { expectedIntentMatrix } from "./helpers/expected-intent-matrix.js";
import { parseIntentMatrix } from "./helpers/intent-matrix.js";

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
    const matrix = parseIntentMatrix();

    expect([...matrix.keys()].sort()).toEqual([...expectedIntentMatrix.keys()].sort());

    const createPlan = resolveIntentLoadPlan("automation.create", matrix);
    expect(createPlan.companions).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-automation-best-practices.md",
      "$NOVA_REPO_ROOT/skills/ha-safety.md",
    ]);
    expect(createPlan.modules).toEqual([
      "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/resolve.md",
      "$NOVA_REPO_ROOT/skills/ha-nova/modules/automation/create-update.md",
    ]);

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
