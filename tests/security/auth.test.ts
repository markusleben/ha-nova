import { describe, expect, it } from "vitest";

import { loadEnv } from "../../nova/src/config/env.js";
import { authorizeRequest } from "../../nova/src/security/auth.js";

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
  it("parses required values", () => {
    const env = loadEnv({
      RELAY_AUTH_TOKEN: "llt-token",
      HA_LLAT: "ha-llat-token",
      RELAY_PORT: "9000",
      LOG_LEVEL: "debug"
    });

    expect(env).toEqual({
      relayAuthToken: "llt-token",
      haLlat: "ha-llat-token",
      haUrl: "http://homeassistant:8123",
      relayVersion: "dev",
      appOptionsPath: "/data/options.json",
      relayPort: 9000,
      logLevel: "debug"
    });
  });

  it("reads and trims required HA_LLAT", () => {
    const env = loadEnv({
      RELAY_AUTH_TOKEN: "relay-token",
      HA_LLAT: "  user-llat  "
    });

    expect(env).toEqual({
      relayAuthToken: "relay-token",
      haLlat: "user-llat",
      haUrl: "http://homeassistant:8123",
      relayVersion: "dev",
      appOptionsPath: "/data/options.json",
      relayPort: 8791,
      logLevel: "info"
    });
  });

  it("uses RELAY_AUTH_TOKEN and HA_LLAT as required tokens", () => {
    const env = loadEnv({
      RELAY_AUTH_TOKEN: "relay-auth",
      HA_LLAT: "ha-llat"
    });

    expect(env).toEqual({
      relayAuthToken: "relay-auth",
      haLlat: "ha-llat",
      haUrl: "http://homeassistant:8123",
      relayVersion: "dev",
      appOptionsPath: "/data/options.json",
      relayPort: 8791,
      logLevel: "info"
    });
  });

  it("throws when HA_LLAT is missing", () => {
    expect(() =>
      loadEnv({
        RELAY_AUTH_TOKEN: "relay-auth"
      })
    ).toThrowError("HA_LLAT is required");
  });

  it("treats literal null tokens as missing", () => {
    expect(() =>
      loadEnv({
        RELAY_AUTH_TOKEN: "null",
        HA_LLAT: "ha-llat"
      })
    ).toThrowError("RELAY_AUTH_TOKEN is required");

    expect(() =>
      loadEnv({
        RELAY_AUTH_TOKEN: "relay-auth",
        HA_LLAT: "null"
      })
    ).toThrowError("HA_LLAT is required");
  });
});
