import { describe, expect, it } from "vitest";

import { resolveUpstreamToken } from "../../src/security/token-resolver.js";

describe("resolveUpstreamToken", () => {
  it("prefers HA_LLAT env over all other sources", () => {
    const result = resolveUpstreamToken({
      envHaLlat: "env-token",
      appOptionHaLlat: "app-token",
      supervisorToken: "supervisor-token"
    });

    expect(result).toEqual({
      token: "env-token",
      source: "env_ha_llat",
      capability: "full",
      warnings: []
    });
  });

  it("uses app option LLAT when env LLAT is missing", () => {
    const result = resolveUpstreamToken({
      appOptionHaLlat: "app-token",
      supervisorToken: "supervisor-token"
    });

    expect(result).toEqual({
      token: "app-token",
      source: "app_option_ha_llat",
      capability: "full",
      warnings: []
    });
  });

  it("falls back to supervisor token in limited mode when no LLAT is available", () => {
    const result = resolveUpstreamToken({
      supervisorToken: "supervisor-token"
    });

    expect(result).toEqual({
      token: "supervisor-token",
      source: "supervisor_token",
      capability: "limited",
      warnings: ["LLAT missing. Falling back to SUPERVISOR_TOKEN with limited WebSocket scope."]
    });
  });

  it("returns none mode when no token source is available", () => {
    const result = resolveUpstreamToken({});

    expect(result).toEqual({
      token: null,
      source: "none",
      capability: "none",
      warnings: ["No upstream token available. Configure HA_LLAT or app option 'ha_llat'."]
    });
  });

  it("trims values and ignores empty token strings", () => {
    const result = resolveUpstreamToken({
      envHaLlat: "   ",
      appOptionHaLlat: "  app-token  ",
      supervisorToken: " supervisor-token "
    });

    expect(result).toEqual({
      token: "app-token",
      source: "app_option_ha_llat",
      capability: "full",
      warnings: []
    });
  });
});
