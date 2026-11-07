import { EventEmitter } from "node:events";
import { mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createGateWorker, mintGateDevice, type GateWorkerDeps } from "../../nova/src/runtime/gate-worker.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { GATE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import { resolvePrincipal } from "../../nova/src/security/principal.js";

// The `ha-nova gate` child (nova/src/runtime/gate-worker.ts): how it starts,
// what it gets, and the internal credential it reaches the Relay with.

let dir: string;
let registry: DeviceRegistry;
let spawned: { binary: string; args: string[]; env: NodeJS.ProcessEnv; child: EventEmitter & { kill: (signal?: string) => boolean } }[];
let errors: string[];

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-gate-worker-"));
  registry = openDeviceRegistry(dir);
  spawned = [];
  errors = [];
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

function worker(overrides: Partial<GateWorkerDeps> = {}) {
  return createGateWorker({
    dataDir: dir,
    binary: process.execPath,
    mint: mintGateDevice(registry, () => 1, "https://127.0.0.1:8792", "pin"),
    logger: { warn: () => {}, error: (message) => errors.push(message) },
    respawnMs: 10,
    spawn: ((binary: string, args: string[], options: { env: NodeJS.ProcessEnv }) => {
      const child = Object.assign(new EventEmitter(), {
        kill: (signal?: string) => {
          queueMicrotask(() => child.emit("exit", null, signal ?? "SIGTERM"));
          return true;
        },
      });
      spawned.push({ binary, args, env: options.env, child });
      return child;
    }) as never,
    ...overrides,
  });
}

const resolve = (credential: string, peer: string) => resolvePrincipal(`Bearer ${credential}`, "secure", { registry, now: () => 1 }, peer);
const relayFile = () => JSON.parse(readFileSync(join(dir, "gate", "relay.json"), "utf8")) as { relay_url: string; spki_pin: string; credential: string };
const gateDevices = () => registry.list().filter((d) => d.clientInstallId === GATE_INSTALL_ID);

describe("gate worker child", () => {
  it("starts `ha-nova gate` on loopback with its files and a minimal environment", () => {
    process.env.SUPERVISOR_TOKEN = "never-passed";
    try {
      worker().start();
    } finally {
      delete process.env.SUPERVISOR_TOKEN;
    }
    expect(spawned).toHaveLength(1);
    const gateDir = join(dir, "gate");
    expect(spawned[0]!.args).toEqual(["gate", "--listen", "127.0.0.1:8796", "--secret-file", join(gateDir, "secret"), "--relay-file", join(gateDir, "relay.json")]);
    expect(Object.keys(spawned[0]!.env).sort()).toEqual(["HA_NOVA_NO_BROWSER", "HOME", "PATH", ...(process.env.TZ ? ["TZ"] : [])].sort());
    expect(readFileSync(join(gateDir, "secret"), "utf8")).toMatch(/^[0-9a-f]{64}$/);
    expect(statSync(join(gateDir, "secret")).mode & 0o777).toBe(0o600);
    expect(statSync(join(gateDir, "relay.json")).mode & 0o777).toBe(0o600);
  });

  it("reaches the Relay as the internal gate device, from loopback only", () => {
    worker().start();
    const relay = relayFile();
    expect(relay).toMatchObject({ relay_url: "https://127.0.0.1:8792", spki_pin: "pin" });
    expect(resolve(relay.credential, "127.0.0.1")).toMatchObject({ ok: true, principal: { kind: "device", internal: "gate", peer: "127.0.0.1" } });
    expect(resolve(relay.credential, "::1")).toMatchObject({ ok: true });
    for (const peer of ["192.168.1.20", "172.30.32.2", "::ffff:127.0.0.1"]) {
      expect(resolve(relay.credential, peer)).toMatchObject({ ok: false, status: 401 });
    }
    expect(resolvePrincipal(`Bearer ${relay.credential}`, "secure", { registry, now: () => 1 })).toMatchObject({ ok: false, status: 401 });
  });

  it("a stopped worker is started again with a new credential; the old one stops working", async () => {
    const gate = worker();
    gate.start();
    const first = relayFile().credential;
    spawned[0]!.child.emit("exit", 2, null);
    await new Promise((r) => setTimeout(r, 50));
    expect(spawned).toHaveLength(2);
    expect(relayFile().credential).not.toBe(first);
    expect(resolve(first, "127.0.0.1")).toMatchObject({ ok: false });
    expect(resolve(relayFile().credential, "127.0.0.1")).toMatchObject({ ok: true });
    expect(gateDevices()).toHaveLength(1);
    gate.restart(); // after a registry reset
    await new Promise((r) => setTimeout(r, 50));
    expect(spawned).toHaveLength(3);
    expect(gateDevices()).toHaveLength(1);
  });

  it("a missing binary starts nothing, and every check is unavailable", async () => {
    const gate = worker({ binary: join(dir, "missing") });
    gate.start();
    expect(spawned).toEqual([]);
    expect(errors).toEqual(["The safety-check worker is missing; every device request is refused"]);
    expect(await gate.check({ kind: "legacy" }, "ws", Buffer.from("{}"))).toBeNull();
    expect(await gate.redact("ws", Buffer.from("{}"), Buffer.from("{}"))).toBeNull();
  });

  it("a credential that can't be minted (corrupt registry) is retried, never fatal", async () => {
    let fail = true;
    const create = registry.createPending.bind(registry);
    registry.createPending = (...args) => {
      if (fail) {
        throw new Error("device registry is corrupt");
      }
      return create(...args);
    };
    worker().start();
    expect(spawned).toEqual([]);
    fail = false;
    await new Promise((r) => setTimeout(r, 50));
    expect(spawned).toHaveLength(1);
  });
});
