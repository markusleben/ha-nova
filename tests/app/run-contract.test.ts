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

  it("maps options to env without re-implementing token precedence logic", () => {
    const content = readFileSync("nova/run", "utf8");

    expect(content).toContain("normalize_token()");
    expect(content).toContain('"$value" == "null"');
    expect(content).toContain('HA_URL="http://supervisor/core"');
    expect(content).toContain('HA_URL="http://homeassistant:8123"');
    expect(content).toContain("RELAY_AUTH_TOKEN");
    expect(content).toContain('RELAY_AUTH_TOKEN_FILE="/data/relay_auth_token"');
    expect(content).not.toContain("PRODUCT_VERSION");
    expect(content).toContain("MIN_RELAY_VERSION");
    expect(content).toContain("/app/version.json");
    expect(content).toContain("Version metadata is missing");
    expect(content).toContain("metadata.cloud_remote_enabled !== true");
    expect(content).toContain("metadata.cloud_remote_enabled !== false");
    expect(content).toContain(
      "process.stdout.write(String(metadata.cloud_remote_enabled))",
    );
    expect(content).toContain("export CLOUD_REMOTE_ENABLED");
    expect(content).toContain("HA_LLAT");
    expect(content).not.toContain("RELAY_AUTH_TOKEN is required");

    expect(content).not.toContain("resolveUpstreamToken");
    expect(content).toContain("SUPERVISOR_TOKEN");
    expect(content).not.toContain("WS_ALLOWLIST_APPEND");
  });

  it("uses explicit run.sh entrypoint in Dockerfile", () => {
    const dockerfile = readFileSync("nova/Dockerfile", "utf8");

    expect(dockerfile).toContain("COPY run /run.sh");
    expect(dockerfile).toContain("COPY version.json ./version.json");
    expect(dockerfile).toContain('CMD ["/run.sh"]');
  });
});
