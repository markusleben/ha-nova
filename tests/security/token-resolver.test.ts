import { describe, expect, it } from "vitest";

import { resolveUpstreamToken } from "../../nova/src/security/token-resolver.js";

describe("resolveUpstreamToken", () => {
  it("prefers the App Supervisor token over a legacy LLAT", () => {
    const result = resolveUpstreamToken({
      supervisorToken: "supervisor-token",
      envHaLlat: "legacy-llat"
    });

    expect(result).toEqual({
      token: "supervisor-token",
      source: "supervisor_token",
      capability: "full",
      warnings: []
    });
  });

  it("uses HA_LLAT env when available", () => {
    const result = resolveUpstreamToken({
      envHaLlat: "env-token"
    });

    expect(result).toEqual({
      token: "env-token",
      source: "env_ha_llat",
      capability: "full",
      warnings: []
    });
  });

  it("returns none mode when no token source is available", () => {
    const result = resolveUpstreamToken({});

    expect(result).toEqual({
      token: null,
      source: "none",
      capability: "none",
      warnings: [
        "No upstream token available. Configure SUPERVISOR_TOKEN for the App or HA_LLAT for standalone use."
      ]
    });
  });

  it("trims values and ignores empty token strings", () => {
    const result = resolveUpstreamToken({
      envHaLlat: "  env-token  "
    });

    expect(result).toEqual({
      token: "env-token",
      source: "env_ha_llat",
      capability: "full",
      warnings: []
    });
  });
});
