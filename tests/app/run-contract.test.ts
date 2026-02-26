import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app run contract", () => {
  it("provides an executable thin runner script", () => {
    const stats = statSync("app/run");
    const content = readFileSync("app/run", "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/with-contenv bashio")).toBe(true);
    expect(content).toContain("exec node /app/dist/src/runtime/main.js");
  });

  it("maps options to env without re-implementing token precedence logic", () => {
    const content = readFileSync("app/run", "utf8");

    expect(content).toContain("BRIDGE_AUTH_TOKEN");
    expect(content).toContain("HA_LLAT");
    expect(content).toContain("WS_ALLOWLIST_APPEND");

    expect(content).not.toContain("resolveUpstreamToken");
    expect(content).not.toContain("SUPERVISOR_TOKEN");
  });

  it("uses explicit run.sh entrypoint in Dockerfile", () => {
    const dockerfile = readFileSync("app/Dockerfile", "utf8");

    expect(dockerfile).toContain("COPY app/run /run.sh");
    expect(dockerfile).toContain('CMD ["/run.sh"]');
  });
});
