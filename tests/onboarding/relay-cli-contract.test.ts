import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

import { createMockHome, REPO_ROOT } from "./_helpers.js";

describe("relay cli contract", () => {
  it("guides the user to setup when onboarding config is missing", () => {
    const home = createMockHome();

    const result = spawnSync(
      "bash",
      ["scripts/relay.sh", "health"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 15000,
        env: { ...process.env, HOME: home },
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(result.status).not.toBe(0);
    expect(output).toContain("HA NOVA is not set up yet");
    expect(output).toContain("Run: ha-nova setup");
    expect(output).not.toContain("missing ~/.config/ha-nova/onboarding.env");
  });
});
