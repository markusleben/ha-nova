import { existsSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-nova contract suite compatibility shim", () => {
  it("keeps split contract suites present", () => {
    const files = [
      "tests/skills/ha-nova-contract.test.ts",
      "tests/skills/bulk-audit-contract.test.ts",
      "tests/skills/ha-entities-contract.test.ts",
      "tests/skills/ha-safety-contract.test.ts",
      "tests/skills/ha-cross-skill-integration.test.ts",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected split suite file to exist: ${file}`).toBe(true);
    }
  });
});
