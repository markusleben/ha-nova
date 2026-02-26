import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";
import YAML from "yaml";

describe("addon config contract", () => {
  it("includes required metadata, security flags, and option schema", () => {
    const raw = readFileSync("addon/config.yaml", "utf8");
    const parsed = YAML.parse(raw) as Record<string, unknown>;

    expect(parsed.name).toBeTypeOf("string");
    expect(parsed.slug).toBe("ha_nova_bridge");
    expect(parsed.version).toBeTypeOf("string");
    expect(parsed.startup).toBe("services");
    expect(parsed.boot).toBe("auto");

    expect(parsed.homeassistant_api).toBe(true);
    expect(parsed.hassio_api).toBe(true);
    expect(parsed.hassio_role).toBe("default");
    expect(parsed.ingress).toBe(true);

    expect(parsed.options).toMatchObject({
      bridge_auth_token: null,
      ha_llat: null,
      ws_allowlist_append: ""
    });

    expect(parsed.schema).toMatchObject({
      bridge_auth_token: "password",
      ha_llat: "password?",
      ws_allowlist_append: "str?"
    });
  });
});
