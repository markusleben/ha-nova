import { describe, expect, it } from "vitest";

import { resolveUpstreamToken } from "../../src/security/token-resolver.js";

describe("resolveUpstreamToken", () => {
  it("prefers HA_LLAT env over all other sources", () => {
    const result = resolveUpstreamToken({
      envHaLlat: "env-token",
      addonOptionHaLlat: "addon-token",
      legacyHaToken: "legacy-token",
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
      addonOptionHaLlat: "addon-token",
      legacyHaToken: "legacy-token",
      supervisorToken: "supervisor-token"
    });

    expect(result).toEqual({
      token: "addon-token",
      source: "addon_option_ha_llat",
      capability: "full",
      warnings: []
    });
  });

  it("falls back to legacy HA_TOKEN with deprecation warning", () => {
    const result = resolveUpstreamToken({
      legacyHaToken: "legacy-token",
      supervisorToken: "supervisor-token"
    });

    expect(result).toEqual({
      token: "legacy-token",
      source: "legacy_ha_token",
      capability: "full",
      warnings: ["HA_TOKEN is a legacy LLAT fallback. Prefer HA_LLAT or addon option 'ha_llat'."]
    });
  });

  it("falls back to supervisor token in limited mode", () => {
    const result = resolveUpstreamToken({
      supervisorToken: "supervisor-token"
    });

    expect(result).toEqual({
      token: "supervisor-token",
      source: "supervisor_token",
      capability: "limited",
      warnings: ["LLAT missing. Falling back to SUPERVISOR_TOKEN with limited API scope."]
    });
  });

  it("returns none mode when no token source is available", () => {
    const result = resolveUpstreamToken({});

    expect(result).toEqual({
      token: null,
      source: "none",
      capability: "none",
      warnings: ["No upstream token available. Configure HA_LLAT or addon option 'ha_llat'."]
    });
  });

  it("trims values and ignores empty token strings", () => {
    const result = resolveUpstreamToken({
      envHaLlat: "   ",
      addonOptionHaLlat: "  addon-token  ",
      legacyHaToken: "\t",
      supervisorToken: " supervisor-token "
    });

    expect(result).toEqual({
      token: "addon-token",
      source: "addon_option_ha_llat",
      capability: "full",
      warnings: []
    });
  });
});
