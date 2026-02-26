import { describe, expect, it } from "vitest";

import { resolveUpstreamToken } from "../../src/security/token-resolver.js";

describe("resolveUpstreamToken", () => {
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
      warnings: ["No upstream token available. Configure HA_LLAT."]
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
