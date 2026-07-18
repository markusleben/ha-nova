import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";
import YAML from "yaml";

describe("app config contract", () => {
  it("includes required metadata, security flags, and option schema", () => {
    const raw = readFileSync("nova/config.yaml", "utf8");
    const parsed = YAML.parse(raw) as Record<string, unknown>;

    expect(parsed.name).toBeTypeOf("string");
    expect(parsed.slug).toBe("ha_nova_relay");
    expect(parsed.version).toBeTypeOf("string");
    expect(parsed.startup).toBe("services");
    expect(parsed.boot).toBe("auto");

    expect(parsed.homeassistant_api).toBe(true);
    expect(parsed.hassio_api).toBe(true);
    expect(parsed.hassio_role).toBe("default");
    expect(parsed.ingress).toBe(true);
    // Ingress moves to an internal, unmapped port so the owner console is never
    // LAN-reachable; the public transport ports are 8791 (bootstrap) + 8792 (TLS).
    expect(parsed.ingress_port).toBe(8793);
    expect(parsed.ingress_entry).toBe("/home");
    expect(parsed.panel_admin).toBe(true);
    expect(parsed.panel_title).toBe("NOVA");
    expect(parsed.panel_icon).toBe("mdi:star-four-points");
    expect(parsed.ports).toMatchObject({
      "8791/tcp": 8791,
      "8792/tcp": 8792
    });
    expect(parsed.ports_description).toMatchObject({
      "8791/tcp": "Relay pairing + legacy HTTP",
      "8792/tcp": "Relay secure device API (TLS)"
    });

    expect(parsed.options).toEqual({
      relay_auth_token: null,
      ha_llat: "",
      // File access is a capability, not a default: the App ships it OFF.
      file_access: "off"
    });

    expect(parsed.schema).toEqual({
      relay_auth_token: "password?",
      ha_llat: "password?",
      file_access: "list(off|read|readwrite)?"
    });

    // The config directory is mapped so file access CAN work, but mapping it
    // grants nothing on its own — the option above is the gate. The path is
    // pinned explicitly: a type-derived default mount would leave the relay
    // unable to find the directory, and file access would silently stay off.
    expect(parsed.map).toEqual([
      { type: "homeassistant_config", read_only: false, path: "/config" }
    ]);
  });

  // A security-sensitive option needs UI copy, or the user sees a bare dropdown
  // and cannot tell what they are turning on.
  it("explains every App option in the UI translations", () => {
    const config = YAML.parse(readFileSync("nova/config.yaml", "utf8")) as {
      options: Record<string, unknown>;
    };
    const translations = YAML.parse(readFileSync("nova/translations/en.yaml", "utf8")) as {
      configuration: Record<string, { name?: string; description?: string }>;
    };

    for (const option of Object.keys(config.options)) {
      const copy = translations.configuration[option];
      expect(copy?.name, `${option}: missing a UI name`).toBeTruthy();
      expect(copy?.description, `${option}: missing a UI description`).toBeTruthy();
    }

    // The file-access copy must state the default and the hard limits — that is
    // what makes the opt-in gate meaningful to a human.
    const fileAccess = translations.configuration.file_access?.description ?? "";
    expect(fileAccess).toContain("off");
    expect(fileAccess).toContain("Secrets");

    const relayToken = translations.configuration.relay_auth_token?.description ?? "";
    expect(relayToken).toContain("leave this empty");
    expect(relayToken).toContain("persists");
  });

  it("has relay version >= min_relay_version from version.json", () => {
    const raw = readFileSync("nova/config.yaml", "utf8");
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
