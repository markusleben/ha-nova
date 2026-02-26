import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";
import YAML from "yaml";

describe("app config contract", () => {
  it("includes required metadata, security flags, and option schema", () => {
    const raw = readFileSync("app/config.yaml", "utf8");
    const parsed = YAML.parse(raw) as Record<string, unknown>;

    expect(parsed.name).toBeTypeOf("string");
    expect(parsed.slug).toBe("ha_nova_relay");
    expect(parsed.version).toBeTypeOf("string");
    expect(parsed.startup).toBe("services");
    expect(parsed.boot).toBe("auto");

    expect(parsed.homeassistant_api).toBe(true);
    expect(parsed.hassio_api).toBe(true);
    expect(parsed.hassio_role).toBe("default");
    expect(parsed.ingress).toBe(false);
    expect(parsed.ports).toMatchObject({
      "8791/tcp": 8791
    });
    expect(parsed.ports_description).toMatchObject({
      "8791/tcp": "Relay HTTP API"
    });

    expect(parsed.options).toEqual({
      relay_auth_token: null,
      ha_llat: null
    });
    expect(parsed.options).not.toHaveProperty("ws_allowlist_append");

    expect(parsed.schema).toEqual({
      relay_auth_token: "password",
      ha_llat: "password"
    });
    expect(parsed.schema).not.toHaveProperty("ws_allowlist_append");
  });
});
