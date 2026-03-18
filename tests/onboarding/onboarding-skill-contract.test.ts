import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("onboarding skill contract", () => {
  const skill = readFileSync("skills/onboarding/SKILL.md", "utf8");

  it("does not treat ha_ws_connected=false as enough proof of an LLAT problem", () => {
    expect(skill).toContain("`ha_ws_connected=false`");
    expect(skill).toContain("/ws");
    expect(skill).toContain("LLAT");
    expect(skill).not.toContain("upstream HA websocket not healthy");
  });
});
