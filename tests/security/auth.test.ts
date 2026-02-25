import { describe, expect, it } from "vitest";

import { loadEnv } from "../../src/config/env.js";
import { authorizeRequest } from "../../src/security/auth.js";

describe("auth", () => {
  it("returns 401 when authorization header is missing", () => {
    const result = authorizeRequest(undefined, "secret");

    expect(result).toEqual({
      ok: false,
      status: 401,
      code: "UNAUTHORIZED",
      message: "Missing authorization header"
    });
  });

  it("returns 401 when bearer token is invalid", () => {
    const result = authorizeRequest("Bearer wrong", "secret");

    expect(result).toEqual({
      ok: false,
      status: 401,
      code: "UNAUTHORIZED",
      message: "Invalid bearer token"
    });
  });

  it("allows request when bearer token is valid", () => {
    const result = authorizeRequest("Bearer secret", "secret");

    expect(result).toEqual({
      ok: true
    });
  });
});

describe("loadEnv", () => {
  it("parses required and optional values", () => {
    const env = loadEnv({
      HA_TOKEN: "llt-token",
      BRIDGE_PORT: "9000",
      LOG_LEVEL: "debug"
    });

    expect(env).toEqual({
      haToken: "llt-token",
      bridgePort: 9000,
      logLevel: "debug",
      wsAllowlistExtra: []
    });
  });
});
