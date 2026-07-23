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

  it("probes every passive or stale false state before diagnosis", () => {
    const falseState = skill.indexOf("`ha_ws_connected=false`");
    const ping = skill.indexOf('Send WS `{"type":"ping"}`');
    const postHealth = skill.indexOf("re-read health");
    const diagnosis = skill.indexOf("Otherwise classify the ping error and post-health reason");

    expect(falseState).toBeGreaterThan(-1);
    expect(ping).toBeGreaterThan(falseState);
    expect(postHealth).toBeGreaterThan(ping);
    expect(diagnosis).toBeGreaterThan(postHealth);
    expect(skill).toContain("any `auth`, `network`, or `never_connected` value");
    expect(skill).toContain(
      "Ping `ok: true` plus post-ping `ha_ws_connected: true` proves readiness.",
    );
    expect(skill).toContain(
      "`never_connected` alone never proves a bad `HA_URL` or network path.",
    );
  });
});
