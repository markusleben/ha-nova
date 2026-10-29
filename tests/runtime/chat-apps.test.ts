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
let entries: { id: string; uuid: string }[]; // Home Assistant's ha_nova config entries
let entriesStatus: number;
let entriesMade: number;
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
  entries = [];
  entriesStatus = 200;
  entriesMade = 0;
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
        const message = JSON.parse(body) as { config: { secret?: unknown } };
        discovery.set(UUID, message); // one message per App and service
        // Home Assistant sets up an entry from a valid config (the integration refuses one without a secret).
        if (typeof message.config.secret === "string" && !entries.some((e) => e.uuid === UUID)) entries.push({ id: `E${++entriesMade}`, uuid: UUID });
        return reply(200, { uuid: UUID });
      }
      if (request.method === "DELETE" && request.url === `/discovery/${UUID}`) {
        const existed = discovery.delete(UUID);
        if (deleteStatus === 200 && existed) entries = entries.filter((e) => e.uuid !== UUID); // Home Assistant drops the entry made from it
        return reply(deleteStatus === 200 && !existed ? 404 : deleteStatus, {});
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
        if (input.path === "/api/config/config_entries/entry?domain=ha_nova") return { status: entriesStatus, body: entries.map((e) => ({ entry_id: e.id, domain: "ha_nova" })) };
        const entry = /^\/api\/config\/config_entries\/entry\/(\w+)$/.exec(input.path)?.[1];
        if (input.method === "DELETE" && entry !== undefined) {
          const found = entries.some((e) => e.id === entry);
          entries = entries.filter((e) => e.id !== entry);
          return { status: found ? 200 : 404, body: {} };
        }
        return { status: 200, body: [] };
      },
    },
    now: () => 1_000_000,
    logger: { info: () => {}, warn: () => {}, error: () => {} },
    respawnMs: 5,
    owner: "this-app",
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
    expect(added).toEqual(["custom_components", "custom_components/ha_nova", "custom_components/ha_nova/.nova_app", "custom_components/ha_nova/__init__.py", "custom_components/ha_nova/manifest.json", "custom_components/ha_nova/translations", "custom_components/ha_nova/translations/en.json"]);
    expect(readFileSync(join(root, "config", "custom_components", "ha_nova", ".nova_app"), "utf8")).toBe("this-app"); // whose folder it is
    expect(readFileSync(join(root, "config", "secrets.yaml"), "utf8")).toBe("x: y\n");

    expect(secretOf()).toMatch(/^[0-9a-f]{64}$/);
    expect(statSync(data("secret")).mode & 0o777).toBe(0o600);
    expect(discovery.get(UUID)).toEqual({ service: "ha_nova", config: { host: "abcd1234-ha-nova-relay", port: 8794, secret: secretOf() } });
    expect(JSON.parse(readFileSync(data("state.json"), "utf8"))).toEqual({ enabled: true, discovery_uuid: UUID, made_custom_components: true });

    // serve: internal port, the bridge's own files, and none of the App's tokens.
    expect(spawned).toHaveLength(1);
    expect(spawned[0]!.args).toEqual(["serve", "--listen", "0.0.0.0:8794", "--data-dir", data("bridge"), "--secret-file", data("secret"), "--relay-file", data("relay.json"),
      "--control-listen", "127.0.0.1:8795", "--control-secret-file", data("control-secret")]);
    // The control channel's own secret: loopback only, never the one Home Assistant holds.
    const control = readFileSync(data("control-secret"), "utf8");
    expect(control).toMatch(/^[0-9a-f]{64}$/);
    expect(control).not.toBe(secretOf());
    expect(statSync(data("control-secret")).mode & 0o777).toBe(0o600);
    expect(JSON.stringify([...discovery.values()])).not.toContain(control);
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
    expect(existsSync(join(root, "config", "custom_components"))).toBe(false); // the App made it, and it is empty
    expect(registry.list().some((d) => d.deviceId === bridgeId)).toBe(false);
    expect(existsSync(data("secret"))).toBe(false);
    expect(existsSync(data("control-secret"))).toBe(false);
    expect(existsSync(data("relay.json"))).toBe(false);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    await expect(apps.phone.phones(OWNER)).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(spawned).toHaveLength(1); // not respawned once off
    await apps.turnOff(); // idempotent, also with the message already gone
  });

  it("keeps a custom_components folder it didn't make, or one with anything else in it", async () => {
    mkdirSync(join(root, "config", "custom_components"));
    let apps = chatApps();
    await apps.turnOn(true);
    expect(JSON.parse(readFileSync(data("state.json"), "utf8")).made_custom_components).toBeUndefined();
    await apps.turnOff();
    expect(tree(join(root, "config", "custom_components"))).toEqual([]); // empty, but the owner's

    rmSync(join(root, "config", "custom_components"), { recursive: true });
    apps = chatApps();
    await apps.turnOn(true);
    mkdirSync(join(root, "config", "custom_components", "hacs")); // installed later
    await apps.turnOff();
    expect(tree(join(root, "config", "custom_components"))).toEqual(["hacs"]);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
  });

  it("removes the empty folder it made after a failed turn-on too", async () => {
    postStatus = 403;
    await expect(chatApps().turnOn(true)).rejects.toThrow(/403/);
    expect(existsSync(join(root, "config", "custom_components"))).toBe(false);
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
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    expect(spawned[0]!.child.killed).toBe("SIGTERM");
    // Nothing is left that the owner could no longer see or remove.
    expect(existsSync(join(root, "config", "custom_components", "ha_nova"))).toBe(false);
    expect(existsSync(data("secret"))).toBe(false);
    expect(registry.list()).toHaveLength(0);
    postStatus = 200;
    expect(await apps.turnOn(true)).toEqual({ restart: true });
    expect(apps.status(OWNER).enabled).toBe(true);
  });

  it("refuses Home Assistant before 2026.10 before it changes anything", async () => {
    haVersion = "2026.9.3";
    await expect(chatApps().turnOn(true)).rejects.toMatchObject({ code: "HA_TOO_OLD" });
    expect(existsSync(data())).toBe(false);
  });

  it("refuses an unreadable Home Assistant version", async () => {
    haVersion = "dev";
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

  it("revokes the bridge even when its files are damaged or gone", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    writeFileSync(data("state.json"), "{broken");
    rmSync(data("relay.json"));
    await expect(apps.turnOff()).rejects.toThrow();
    expect(registry.list()).toHaveLength(0);
  });

  it("cancels a restart that has not been sent when chat apps turn off", async () => {
    const apps = chatApps();
    await apps.turnOn(true);
    vi.useFakeTimers();
    apps.restartSoon();
    await apps.turnOff();
    await vi.advanceTimersByTimeAsync(2_000);
    expect(coreCalls).not.toContain("POST /api/services/homeassistant/restart");
  });

  it("retries a bridge that could not start on boot", async () => {
    await chatApps().turnOn(true);
    for (const d of registry.list()) registry.revoke(d.deviceId); // forces a re-mint
    let fail = true;
    const flakyRegistry: DeviceRegistry = { ...registry, createPending: (record, now) => {
      if (fail) throw new Error("disk full");
      registry.createPending(record, now);
    } };
    const flaky = chatApps({ registry: flakyRegistry });
    await flaky.boot();
    expect(spawned).toHaveLength(1);
    fail = false;
    await vi.waitFor(() => expect(spawned).toHaveLength(2));
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
    const control = readFileSync(data("control-secret"), "utf8");
    const relay = readFileSync(data("relay.json"), "utf8");
    discovery.clear(); // Supervisor lost it: boot publishes again

    const restarted = chatApps();
    await restarted.boot();
    expect(secretOf()).toBe(secret);
    expect(readFileSync(data("control-secret"), "utf8")).toBe(control);
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

// Lane R1: a Supervisor backup restore rewinds /data (state, secret, saved
// message id) but not what Home Assistant and the Supervisor hold.
describe("chat apps: leftovers after a restore rewound /data", () => {
  const folder = () => join(root, "config", "custom_components", "ha_nova");
  async function onThenRewound(): Promise<void> {
    await chatApps().turnOn(true);
    expect(entries).toEqual([{ id: "E1", uuid: UUID }]);
    rmSync(data(), { recursive: true }); // the App's /data from before chat apps were on
  }

  it("shows the leftovers as not fully off, and off removes the message, every entry, and the folder", async () => {
    await onThenRewound();
    entries.push({ id: "E9", uuid: "f".repeat(32) }); // an older entry Home Assistant kept
    const apps = chatApps();
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: true });
    await apps.turnOff();
    expect(discovery.size).toBe(0);
    expect(entries).toEqual([]);
    expect(existsSync(folder())).toBe(false);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    expect(coreCalls).toContain("DELETE /api/config/config_entries/entry/E9");
  });

  it("finishes the cleanup on its own when the App starts again", async () => {
    await onThenRewound();
    await chatApps().boot();
    expect(discovery.size).toBe(0);
    expect(entries).toEqual([]);
    expect(existsSync(folder())).toBe(false);
    expect(spawned).toHaveLength(1); // nothing started
  });

  it("starts clean when turned on again instead: one entry, from a new message", async () => {
    components = ["http", "ha_nova"]; // Home Assistant still runs the old integration
    await onThenRewound();
    entries.push({ id: "E9", uuid: "f".repeat(32) });
    expect(await chatApps().turnOn(false)).toEqual({ restart: false });
    expect(entries.map((e) => e.uuid)).toEqual([UUID]);
    expect(entries[0]!.id).not.toBe("E1");
    expect((discovery.get(UUID)?.config as { secret: string }).secret).toBe(secretOf());
  });

  it("stays not fully off when Home Assistant can't be read, and a retry finishes", async () => {
    await onThenRewound();
    entriesStatus = 502;
    const apps = chatApps();
    await expect(apps.turnOff()).rejects.toThrow(/502/);
    await expect(apps.boot()).resolves.toBeUndefined(); // logged, never fatal
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: true });
    entriesStatus = 200;
    await apps.turnOff();
    expect(entries).toEqual([]);
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
  });

  it("keeps retrying at start until Home Assistant answers (the App starts before it)", async () => {
    await onThenRewound();
    entriesStatus = 502;
    await chatApps().boot();
    expect(entries).toHaveLength(1);
    entriesStatus = 200;
    await vi.waitFor(() => expect(entries).toEqual([]));
    expect(existsSync(folder())).toBe(false);
  });

  it("turns off at start when /data says on but Home Assistant has no HA NOVA folder", async () => {
    await chatApps().turnOn(true);
    rmSync(folder(), { recursive: true });
    const apps = chatApps();
    await apps.boot();
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    expect(registry.list()).toHaveLength(0); // the bridge credential is revoked
    expect(discovery.size).toBe(0);
    expect(spawned).toHaveLength(1); // serve not started again
  });

  it("never takes another NOVA App's folder for its own leftover", async () => {
    await chatApps({ owner: "other-app" }).turnOn(true);
    for (const d of registry.list()) registry.revoke(d.deviceId); // the other App's registry is its own
    rmSync(data(), { recursive: true });
    const apps = chatApps();
    expect(apps.status(OWNER)).toMatchObject({ enabled: false, unfinished: false });
    await apps.boot();
    await apps.turnOff(); // also when asked: this App has nothing there
    expect(entries).toHaveLength(1);
    expect(discovery.size).toBe(1);
    expect(existsSync(folder())).toBe(true);
  });

  it("finds the message by its folder before Home Assistant made an entry from it", async () => {
    await onThenRewound();
    entries = []; // e.g. the owner just removed it, or Home Assistant hasn't set it up yet
    await chatApps().turnOff();
    expect(discovery.size).toBe(0);
    expect(existsSync(folder())).toBe(false);
  });

  it("never asks the Supervisor or Home Assistant to change anything when nothing is left", async () => {
    const apps = chatApps();
    await apps.boot();
    await apps.turnOff();
    expect(discovery.size).toBe(0);
    expect(coreCalls.filter((c) => !c.startsWith("GET "))).toEqual([]);
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

  it("stores the owner's switches for the bridge, atomically and private, and reads the phone the bridge linked", () => {
    const apps = chatApps();
    apps.setSwitches({ readOnly: true });
    apps.setSwitches({ chatCodes: false });
    expect(JSON.parse(readFileSync(data("bridge", "chat-apps.json"), "utf8"))).toEqual({ read_only: true, chat_codes: false });
    expect(statSync(data("bridge", "chat-apps.json")).mode & 0o777).toBe(0o600);
    expect(readdirSync(data("bridge")).sort()).toEqual(["chat-apps.json"]); // no temp files left
    expect(apps.status(OWNER)).toMatchObject({ readOnly: true, chatCodes: false, ownerPhone: null, allowedUsers: null });
    // Only the bridge writes phone-links.json (its control channel); the App reads the name.
    writeFileSync(data("bridge", "phone-links.json"), JSON.stringify({ [OWNER]: { service: "mobile_app_pixel", device_id: "d".repeat(32), user_id: OWNER, name: "Pixel" } }));
    expect(apps.status(OWNER)).toMatchObject({ ownerPhone: "Pixel", ownerPhoneId: "d".repeat(32) });
  });

  it("makes the control secret at start-up when chat apps were on before it existed (an update)", async () => {
    await chatApps().turnOn(true);
    rmSync(data("control-secret"));
    const restarted = chatApps();
    await restarted.boot();
    expect(readFileSync(data("control-secret"), "utf8")).toMatch(/^[0-9a-f]{64}$/);
    expect(spawned.at(-1)!.args.slice(-4)).toEqual(["--control-listen", "127.0.0.1:8795", "--control-secret-file", data("control-secret")]);
  });
});
