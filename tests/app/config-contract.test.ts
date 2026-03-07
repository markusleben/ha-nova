import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";
import YAML from "yaml";

describe("app config contract", () => {
  it("includes required metadata, security flags, and option schema", () => {
    const raw = readFileSync("config.yaml", "utf8");
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
      ha_llat: ""
    });

    expect(parsed.schema).toEqual({
      relay_auth_token: "password",
      ha_llat: "password?"
    });
  });

  it("has relay version >= min_relay_version from version.json", () => {
    const raw = readFileSync("config.yaml", "utf8");
    const parsed = YAML.parse(raw) as Record<string, unknown>;
    const relayVersion = parsed.version as string;

    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    const minRelay = versionJson.min_relay_version as string;

    const semverRe = /^\d+\.\d+\.\d+$/;
    expect(relayVersion).toMatch(semverRe);
    expect(minRelay).toMatch(semverRe);

    const toNum = (v: string): [number, number, number] => {
      const parts = v.split(".").map(Number);
      return [parts[0]!, parts[1]!, parts[2]!];
    };
    const [rMaj, rMin, rPat] = toNum(relayVersion);
    const [mMaj, mMin, mPat] = toNum(minRelay);
    const gte = rMaj > mMaj || (rMaj === mMaj && (rMin > mMin || (rMin === mMin && rPat >= mPat)));
    expect(gte, `config.yaml ${relayVersion} must be >= min_relay_version ${minRelay}`).toBe(true);
  });
});
