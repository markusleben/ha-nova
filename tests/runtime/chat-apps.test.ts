import { EventEmitter } from "node:events";
import { createServer, type Server } from "node:http";
import { existsSync, lstatSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createSupervisorClient } from "../../nova/src/ha/supervisor-client.js";
import { createChatApps, type ChatApps, type ChatAppsDeps } from "../../nova/src/runtime/chat-apps.js";
import { digestSecret, parseCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import type { CoreProxyRequest } from "../../nova/src/types/api.js";

// The App side of chat apps (nova/src/runtime/chat-apps.ts) against a fake
// Supervisor (real HTTP client), a fake Home Assistant core, and a fake
// `ha-nova serve` child.

const UUID = "0123456789abcdef0123456789abcdef";
const OWNER = "1111111111111111111111111111111a";

interface FakeChild extends EventEmitter {
  kill: (signal?: string) => boolean;
  killed?: string;
}

let root: string;
let registry: DeviceRegistry;
let supervisor: Server;
let base: string;
let discovery: Map<string, Record<string, unknown>>;
let deleteStatus: number;
let postStatus: number;
let components: string[];
let configValid: boolean;
let haVersion: string;
let coreCalls: string[];
let spawned: { args: string[]; env: NodeJS.ProcessEnv; child: FakeChild }[];

beforeEach(async () => {
  root = mkdtempSync(join(tmpdir(), "ha-nova-chat-apps-"));
  mkdirSync(join(root, "data"));
  mkdirSync(join(root, "config"));
  writeFileSync(join(root, "config", "configuration.yaml"), "default_config:\n");
  writeFileSync(join(root, "config", "secrets.yaml"), "x: y\n");
  mkdirSync(join(root, "integration", "translations"), { recursive: true });
  writeFileSync(join(root, "integration", "manifest.json"), '{"domain":"ha_nova"}');
  writeFileSync(join(root, "integration", "__init__.py"), "# proxy\n");
  writeFileSync(join(root, "integration", "translations", "en.json"), "{}");
  writeFileSync(join(root, "ha-nova"), "#!/bin/sh\n");
  registry = openDeviceRegistry(join(root, "data"));
  discovery = new Map();
  deleteStatus = 200;
  postStatus = 200;
  components = ["http"];
  configValid = true;
  haVersion = "2026.10.1";
  coreCalls = [];
  spawned = [];
  supervisor = createServer((request, response) => {
    let body = "";
    request.on("data", (chunk) => (body += chunk));
    request.on("end", () => {
      const reply = (status: number, data: unknown) => {
        response.statusCode = status;
        response.end(JSON.stringify({ result: status === 200 ? "ok" : "error", data }));
      };
      if (request.headers.authorization !== "Bearer sup-token") return reply(401, {});
      if (request.method === "GET" && request.url === "/addons/self/info") return reply(200, { version: "0.11.0", hostname: "abcd1234-ha-nova-relay" });
      if (request.method === "POST" && request.url === "/discovery") {
        if (postStatus !== 200) return reply(postStatus, {});
        discovery.set(UUID, JSON.parse(body) as Record<string, unknown>); // one message per App and service
        return reply(200, { uuid: UUID });
      }
      if (request.method === "DELETE" && request.url === `/discovery/${UUID}`) {
        discovery.delete(UUID);
        return reply(deleteStatus, {});
      }
      reply(404, {});
    });
  });
  await new Promise<void>((resolve) => supervisor.listen(0, "127.0.0.1", resolve));
  const address = supervisor.address();
  base = `http://127.0.0.1:${typeof address === "object" && address ? address.port : 0}`;
});

afterEach(async () => {
  vi.useRealTimers();
  await new Promise<void>((resolve) => supervisor.close(() => resolve()));
  rmSync(root, { recursive: true, force: true });
});

function chatApps(overrides: Partial<ChatAppsDeps> = {}): ChatApps {
  return createChatApps({
    dataDir: join(root, "data"),
    configRoot: join(root, "config"),
    binary: join(root, "ha-nova"),
    integrationSource: join(root, "integration"),
    securePort: 8792,
    spkiPin: "p".repeat(43),
    registry,
    supervisor: createSupervisorClient("sup-token", base),
    core: {
      request: async (input: CoreProxyRequest) => {
        coreCalls.push(`${input.method} ${input.path}`);
        if (input.path === "/api/config") return { status: 200, body: { components, version: haVersion } };
        if (input.path === "/api/config/core/check_config") return { status: 200, body: { result: configValid ? "valid" : "invalid" } };
        return { status: 200, body: [] };
      },
    },
    now: () => 1_000_000,
    logger: { info: () => {}, warn: () => {}, error: () => {} },
    respawnMs: 5,
    spawn: ((_cmd: string, args: string[], options: { env: NodeJS.ProcessEnv }) => {
      const child = new EventEmitter() as FakeChild;
      child.kill = (signal) => {
        child.killed = signal ?? "SIGTERM";
        setImmediate(() => child.emit("exit", null, child.killed));
        return true;
      };
      spawned.push({ args, env: options.env, child });
      return child;
    }) as unknown as NonNullable<ChatAppsDeps["spawn"]>,
    ...overrides,
  });
}

function tree(dir: string): string[] {
  return readdirSync(dir, { recursive: true, withFileTypes: true }).map((e) => relative(dir, join(e.parentPath, e.name))).sort();
}

const data = (...path: string[]) => join(root, "data", "chat-apps", ...path);
const secretOf = () => readFileSync(data("secret"), "utf8");

describe("chat apps: turning on and off", () => {
  it("asks for restart consent first and changes nothing without it", async () => {
    await expect(chatApps().turnOn(false)).rejects.toMatchObject({ status: 409, code: "RESTART_CONSENT_REQUIRED" });
    expect(existsSync(data())).toBe(false);
    expect(existsSync(join(root, "config", "custom_components"))).toBe(false);
    expect(discovery.size).toBe(0);
    configValid = false;
    await expect(chatApps().turnOn(true)).rejects.toMatchObject({ code: "HA_CONFIG_INVALID" });
    expect(existsSync(data())).toBe(false);
  });

  it("writes only custom_components/ha_nova, publishes discovery, starts serve, and asks for one restart", async () => {
    const before = tree(join(root, "config"));
    const apps = chatApps();
    expect(await apps.turnOn(true)).toEqual({ restart: true });
    const added = tree(join(root, "config")).filter((p) => !before.includes(p));
    expect(added).toEqual(["custom_components", "custom_components/ha_nova", "custom_components/ha_nova/__init__.py", "custom_components/ha_nova/manifest.json", "custom_components/ha_nova/translations", "custom_components/ha_nova/translations/en.json"]);
    expect(readFileSync(join(root, "config", "secrets.yaml"), "utf8")).toBe("x: y\n");

    expect(secretOf()).toMatch(/^[0-9a-f]{64}$/);
    expect(statSync(data("secret")).mode & 0o777).toBe(0o600);
    expect(discovery.get(UUID)).toEqual({ service: "ha_nova", config: { host: "abcd1234-ha-nova-relay", port: 8794, secret: secretOf() } });
    expect(JSON.parse(readFileSync(data("state.json"), "utf8"))).toEqual({ enabled: true, discovery_uuid: UUID });

    // serve: internal port, the bridge's own files, and none of the App's tokens.
    expect(spawned).toHaveLength(1);
    expect(spawned[0]!.args).toEqual(["serve", "--listen", "0.0.0.0:8794", "--data-dir", data("bridge"), "--secret-file", data("secret"), "--relay-file", data("relay.json")]);
    expect(Object.keys(spawned[0]!.env).sort()).toEqual(expect.arrayContaining(["HA_NOVA_NO_BROWSER", "HOME", "PATH"]));
    expect(JSON.stringify(spawned[0]!.env)).not.toMatch(/SUPERVISOR_TOKEN|HA_LLAT|RELAY_AUTH_TOKEN|sup-token/);

    // The restart comes after the answer, through homeassistant.restart.
    vi.useFakeTimers();
    apps.restartSoon();
    expect(coreCalls).not.toContain("POST /api/services/homeassistant/restart");
    await vi.advanceTimersByTimeAsync(1_000);
    expect(coreCalls).toContain("POST /api/services/homeassistant/restart");
  });

  it("needs no restart once Home Assistant runs the integration, and is idempotent", async () => {
    components = ["http", "ha_nova"];
    const apps = chatApps();
    expect(await apps.turnOn(false)).toEqual({ restart: false });
    const secret = secretOf();
    expect(await apps.turnOn(false)).toEqual({ restart: false });
    expect(secretOf()).toBe(secret);
    expect(spawned).toHaveLength(1);
    expect(registry.list().filter((d) => d.state === "active")).toHaveLength(1);
    expect(coreCalls).not.toContain("POST /api/config/core/check_config");
  });

  it("turns off: serve stops, discovery and folder go, credential and secret are revoked", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    const bridgeId = apps.bridgeDeviceId();
    await apps.turnOff();
    expect(spawned[0]!.child.killed).toBe("SIGTERM");
    expect(discovery.size).toBe(0);
    expect(existsSync(join(root, "config", "custom_components", "ha_nova"))).toBe(false);
    expect(existsSync(join(root, "config", "custom_components"))).toBe(true);
    expect(registry.list().some((d) => d.deviceId === bridgeId)).toBe(false);
    expect(existsSync(data("secret"))).toBe(false);
    expect(existsSync(data("relay.json"))).toBe(false);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(spawned).toHaveLength(1); // not respawned once off
    await apps.turnOff(); // idempotent, also with the message already gone
  });

  it("never interleaves turning on and off", async () => {
    const apps = chatApps();
    await Promise.all([apps.turnOn(true), apps.turnOff()]);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    expect(discovery.size).toBe(0);
    expect(existsSync(join(root, "config", "custom_components", "ha_nova"))).toBe(false);
    expect(spawned.every((s) => s.child.killed === "SIGTERM")).toBe(true);
  });

  it("stays off after a failed turn-on, so [Try again] turns on again", async () => {
    postStatus = 403; // e.g. config.yaml without discovery: [ha_nova]
    const apps = chatApps();
    await expect(apps.turnOn(true)).rejects.toThrow(/403/);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false });
    expect(spawned[0]!.child.killed).toBe("SIGTERM");
    postStatus = 200;
    expect(await apps.turnOn(true)).toEqual({ restart: true });
    expect(apps.status(OWNER).enabled).toBe(true);
  });

  it("refuses Home Assistant before 2026.10 before it changes anything", async () => {
    haVersion = "2026.9.3";
    await expect(chatApps().turnOn(true)).rejects.toMatchObject({ code: "HA_TOO_OLD" });
    expect(existsSync(data())).toBe(false);
  });

  it("revokes the bridge first, even when the folder can't be removed", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    const bridgeId = apps.bridgeDeviceId();
    rmSync(join(root, "config", "custom_components"), { recursive: true });
    writeFileSync(join(root, "config", "custom_components"), "not a folder");
    await expect(apps.turnOff()).rejects.toMatchObject({ code: "CONFIG_UNSAFE" });
    expect(registry.list().some((d) => d.deviceId === bridgeId)).toBe(false);
    expect(existsSync(data("secret"))).toBe(false);
    expect(discovery.size).toBe(0);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: true });
  });

  it("keeps an unfinished turn-off visible and finishes it on retry", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    deleteStatus = 500;
    await expect(apps.turnOff()).rejects.toThrow();
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: true });
    deleteStatus = 404; // Supervisor already dropped it
    await apps.turnOff();
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
  });
});

describe("chat apps: confinement to custom_components/ha_nova", () => {
  it("refuses a symlinked custom_components and never follows one at ha_nova", async () => {
    const outside = join(root, "outside");
    mkdirSync(outside);
    writeFileSync(join(outside, "keep.txt"), "keep");
    symlinkSync(outside, join(root, "config", "custom_components"));
    await expect(chatApps().turnOn(true)).rejects.toMatchObject({ code: "CONFIG_UNSAFE" });
    expect(readdirSync(outside)).toEqual(["keep.txt"]);

    rmSync(join(root, "config", "custom_components"));
    mkdirSync(join(root, "config", "custom_components"));
    symlinkSync(outside, join(root, "config", "custom_components", "ha_nova"));
    await chatApps().turnOn(true);
    expect(readdirSync(outside)).toEqual(["keep.txt"]);
    expect(lstatSync(join(root, "config", "custom_components", "ha_nova")).isDirectory()).toBe(true);
  });

  it("refuses without a Home Assistant configuration folder", async () => {
    await expect(chatApps({ configRoot: "" }).turnOn(true)).rejects.toMatchObject({ code: "CONFIG_UNAVAILABLE" });
    rmSync(join(root, "integration", "manifest.json"));
    await expect(chatApps().turnOn(true)).rejects.toMatchObject({ code: "CHAT_APPS_UNAVAILABLE" });
  });
});

describe("chat apps: across App restarts", () => {
  it("keeps the secret, the credential, and the discovery message", async () => {
    await chatApps().turnOn(true);
    const secret = secretOf();
    const relay = readFileSync(data("relay.json"), "utf8");
    discovery.clear(); // Supervisor lost it: boot publishes again

    const restarted = chatApps();
    await restarted.boot();
    expect(secretOf()).toBe(secret);
    expect(readFileSync(data("relay.json"), "utf8")).toBe(relay);
    expect((discovery.get(UUID)?.config as { secret: string }).secret).toBe(secret);
    expect(spawned).toHaveLength(2);

    await restarted.turnOff();
    await restarted.turnOn(true);
    expect(secretOf()).not.toBe(secret); // only off and on changes it
  });

  it("never lets a boot resume undo a turn-off, and survives a damaged state file", async () => {
    await chatApps().turnOn(true);
    const restarted = chatApps();
    await Promise.all([restarted.boot(), restarted.turnOff()]);
    expect(restarted.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    expect(discovery.size).toBe(0);
    writeFileSync(data("state.json"), "{broken");
    await expect(chatApps().boot()).resolves.toBeUndefined();
  });

  it("rewrites the pin for a new TLS identity and keeps the credential", async () => {
    await chatApps().turnOn(true);
    const before = JSON.parse(readFileSync(data("relay.json"), "utf8")) as Record<string, string>;
    await chatApps({ spkiPin: "q".repeat(43) }).boot();
    const after = JSON.parse(readFileSync(data("relay.json"), "utf8")) as Record<string, string>;
    expect(after).toEqual({ ...before, spki_pin: "q".repeat(43) });
  });

  it("does nothing on boot while off", async () => {
    await chatApps().boot();
    expect(spawned).toHaveLength(0);
    expect(discovery.size).toBe(0);
  });
});

describe("chat apps: the internal device credential and serve", () => {
  it("mints an active device for the bridge, pinned to the local secure port", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    const relay = JSON.parse(readFileSync(data("relay.json"), "utf8")) as Record<string, string>;
    expect(statSync(data("relay.json")).mode & 0o777).toBe(0o600);
    expect(Object.keys(relay).sort()).toEqual(["credential", "relay_url", "spki_pin"]);
    expect(relay.relay_url).toBe("https://127.0.0.1:8792");
    expect(relay.spki_pin).toBe("p".repeat(43));
    const cred = parseCredential(relay.credential)!;
    expect(registry.resolveDeviceSecret(cred.deviceId, digestSecret(cred.secret), 1_000_000)).not.toBeNull();
    expect(apps.bridgeDeviceId()).toBe(cred.deviceId);
  });

  it("restarts a crashed serve, and re-mints a revoked credential on the restart", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    const first = apps.bridgeDeviceId()!;
    registry.revoke(first); // e.g. a registry reset
    apps.restartBridge();
    await vi.waitFor(() => expect(spawned).toHaveLength(2));
    expect(apps.bridgeDeviceId()).not.toBe(first);
    spawned[1]!.child.emit("exit", 1, null);
    await vi.waitFor(() => expect(spawned).toHaveLength(3));
    expect(registry.list().filter((d) => d.state === "active")).toHaveLength(1);
  });

  it("stores the owner's switches and phone link for the bridge, atomically and private", () => {
    const apps = chatApps();
    apps.setSwitches({ readOnly: true });
    apps.setSwitches({ chatCodes: false });
    apps.setOwnerPhone(OWNER, { service: "mobile_app_pixel", device_id: "d".repeat(32), name: "Pixel" });
    expect(JSON.parse(readFileSync(data("bridge", "chat-apps.json"), "utf8"))).toEqual({ read_only: true, chat_codes: false });
    expect(JSON.parse(readFileSync(data("bridge", "phone-links.json"), "utf8"))).toEqual({
      [OWNER]: { service: "mobile_app_pixel", device_id: "d".repeat(32), user_id: OWNER, name: "Pixel" },
    });
    expect(statSync(data("bridge", "phone-links.json")).mode & 0o777).toBe(0o600);
    expect(apps.status(OWNER)).toMatchObject({ readOnly: true, chatCodes: false, ownerPhone: "Pixel", allowedUsers: null });
    apps.setOwnerPhone(OWNER, null);
    expect(apps.status(OWNER).ownerPhone).toBeNull();
    expect(readdirSync(data("bridge")).sort()).toEqual(["chat-apps.json", "phone-links.json"]); // no temp files left
  });
});
