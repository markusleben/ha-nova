import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("addon run contract", () => {
  it("provides an executable thin runner script", () => {
    const stats = statSync("addon/run");
    const content = readFileSync("addon/run", "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/with-contenv bashio")).toBe(true);
    expect(content).toContain("exec node /app/dist/runtime/main.js");
  });

  it("maps options to env without re-implementing token precedence logic", () => {
    const content = readFileSync("addon/run", "utf8");

    expect(content).toContain("BRIDGE_AUTH_TOKEN");
    expect(content).toContain("HA_LLAT");
    expect(content).toContain("WS_ALLOWLIST_APPEND");

    expect(content).not.toContain("resolveUpstreamToken");
    expect(content).not.toContain("SUPERVISOR_TOKEN");
  });
});
