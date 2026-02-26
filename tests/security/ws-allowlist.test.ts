import { describe, expect, it } from "vitest";

import { WsAllowlistError, createWsAllowlist } from "../../src/security/ws-allowlist.js";

describe("ws allowlist", () => {
  it("allows known default ws type", () => {
    const allowlist = createWsAllowlist();

    expect(allowlist.isAllowed("config/area_registry/list")).toBe(true);
  });

  it("blocks unknown ws type with WS_TYPE_NOT_ALLOWED", () => {
    const allowlist = createWsAllowlist();

    expect(() => allowlist.assertAllowed("evil/type")).toThrowError(WsAllowlistError);
    expect(() => allowlist.assertAllowed("evil/type")).toThrowError(
      expect.objectContaining({
        code: "WS_TYPE_NOT_ALLOWED"
      } satisfies Partial<WsAllowlistError>)
    );
  });

  it("supports wildcard entries and explicit extension", () => {
    const allowlist = createWsAllowlist({
      extraPatterns: ["lovelace/*", "energy/*"]
    });

    expect(allowlist.isAllowed("lovelace/config")).toBe(true);
    expect(allowlist.isAllowed("energy/get_prefs")).toBe(true);
    expect(allowlist.isAllowed("unknown/thing")).toBe(false);
  });
});
