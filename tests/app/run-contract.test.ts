import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app run contract", () => {
  it("provides an executable thin runner script", () => {
    const stats = statSync("nova/run");
    const content = readFileSync("nova/run", "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/with-contenv bashio")).toBe(true);
    expect(content).not.toContain("HA_LLAT is required");
    expect(content).toContain("exec node /app/dist/src/runtime/main.js");
  });

  it("passes only the App version; the Relay reads everything else itself", () => {
    const content = readFileSync("nova/run", "utf8");

    expect(content).toContain('RELAY_VERSION="$(bashio::addon.version');
    expect(content).toContain("export RELAY_VERSION");
    for (const moved of [
      "HA_URL",
      "HA_LLAT",
      "RELAY_AUTH_TOKEN",
      "MIN_RELAY_VERSION",
      "CLOUD_REMOTE_ENABLED",
      "FILE_ACCESS",
      "APP_OPTIONS_PATH",
      "node -e",
      "node -p",
    ]) {
      expect(content).not.toContain(moved);
    }
  });

  it("uses explicit run.sh entrypoint in Dockerfile", () => {
    const dockerfile = readFileSync("nova/Dockerfile", "utf8");

    expect(dockerfile).toContain("COPY run /run.sh");
    expect(dockerfile).toContain("COPY version.json ./version.json");
    expect(dockerfile).toContain('CMD ["/run.sh"]');
  });
});
