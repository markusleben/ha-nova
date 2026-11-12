import { EventEmitter } from "node:events";
import { createServer } from "node:http";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createGateWorker, mintGateDevice, type GateWorkerDeps } from "../../nova/src/runtime/gate-worker.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { MAX_ACTIVE_DEVICES, MAX_PENDING_DEVICES } from "../../nova/src/security/device-registry-mutations.js";
import { GATE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import { resolvePrincipal } from "../../nova/src/security/principal.js";

// The `ha-nova gate` child (nova/src/runtime/gate-worker.ts): how it starts,
// what it gets, and the internal credential it reaches the Relay with.

let dir: string;
let registry: DeviceRegistry;
let spawned: { binary: string; args: string[]; env: NodeJS.ProcessEnv; child: EventEmitter & { kill: (signal?: string) => boolean } }[];
let errors: string[];

beforeEach(() => {
  // A short folder: a Unix socket path holds at most 104 bytes (macOS).
  dir = mkdtempSync(join(process.platform === "win32" ? tmpdir() : "/tmp", "hn-gw-"));
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
    expect(spawned[0]!.args).toEqual(["gate", "--socket", join(gateDir, "worker.sock"), "--secret-file", join(gateDir, "secret"), "--relay-file", join(gateDir, "relay.json")]);
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
    expect(await gate.check({ kind: "shared" }, "ws", Buffer.from("{}"))).toBeNull();
    expect(await gate.redact("ws", Buffer.from("{}"), Buffer.from("{}"))).toBeNull();
  });

  it("starts nothing through a gate folder that is a link, and makes a loose one owner-only", () => {
    mkdirSync(join(dir, "elsewhere"));
    symlinkSync(join(dir, "elsewhere"), join(dir, "gate"));
    worker().start();
    expect(spawned).toEqual([]);
    rmSync(join(dir, "gate"));
    mkdirSync(join(dir, "gate"), { mode: 0o755 });
    chmodSync(join(dir, "gate"), 0o755);
    worker().start();
    expect(spawned).toHaveLength(1);
    expect(statSync(join(dir, "gate")).mode & 0o777).toBe(0o700);
  });

  it("an answer that never ends within the size limit is no answer", async () => {
    const server = createServer((_request, response) => {
      response.writeHead(200, { "content-type": "application/json" });
      const chunk = Buffer.alloc(1 << 20, 0x20);
      const pump = (): void => {
        while (!response.destroyed && response.write(chunk)) {}
        if (!response.destroyed) {
          response.once("drain", pump);
        }
      };
      pump();
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker({ timeoutMs: 60_000 });
      gate.start();
      expect(await gate.check({ kind: "shared" }, "ws", Buffer.from("{}"))).toBeNull();
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  }, 60_000);

  it("a redaction of a full entity registry (several MB) comes back intact, and one that is not base64 is no answer", async () => {
    const registry = Buffer.from(JSON.stringify({ ok: true, data: Array.from({ length: 9000 }, (_, i) => ({ entity_id: `light.room_${i}`, name: "x".repeat(800) })) }));
    let reply = registry.toString("base64");
    const server = createServer((request, response) => {
      request.resume();
      request.on("end", () => response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify({ response_b64: reply })));
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker({ timeoutMs: 20_000 });
      gate.start();
      expect(registry.byteLength).toBeGreaterThan(7 << 20);
      expect(await gate.redact("ws", Buffer.from("{}"), registry)).toEqual(registry);
      for (const bad of ["e30", "e30=\n", "%%%", "e3 0="]) {
        reply = bad;
        expect(await gate.redact("ws", Buffer.from("{}"), Buffer.from("{}"))).toBeNull();
      }
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  }, 30_000);

  it("caps each answer at its own route's limit: a check's is small, a redaction's is not", async () => {
    const big = Buffer.alloc(100 << 10, 0x61);
    const server = createServer((request, response) => {
      request.resume();
      request.on("end", () => {
        const answer = request.url === "/check" ? { verdict: "pass", kind: "read", pad: big.toString() } : { response_b64: big.toString("base64") };
        response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(answer));
      });
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker();
      gate.start();
      expect(await gate.check({ kind: "shared" }, "ws", Buffer.from("{}"))).toBeNull();
      expect(await gate.redact("ws", Buffer.from("{}"), Buffer.from("{}"))).toEqual(big);
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  });

  it("an ask's answer is used only in the worker's own shape", async () => {
    let answer: unknown = {};
    let asked: unknown;
    const server = createServer((request, response) => {
      const chunks: Buffer[] = [];
      request.on("data", (c: Buffer) => chunks.push(c));
      request.on("end", () => {
        asked = JSON.parse(Buffer.concat(chunks).toString("utf8"));
        response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(answer));
      });
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker();
      gate.start();
      const requests = [{ endpoint: "core" as const, body: Buffer.from('{"method":"GET","path":"/api/states"}') }];
      const typed = { verdict: "typed", lang: "en", title: "Unlock?", body: "Requested by “Mac”.", from: "Requested by “Mac”.", button: "Unlock", items: [{ verdict: "typed", text: "Unlock “Front”" }] };
      answer = typed;
      expect(await gate.ask!("Mac", requests)).toEqual(typed);
      expect(asked).toEqual({ device: "Mac", requests: [{ endpoint: "core", body_b64: requests[0]!.body.toString("base64") }] });
      answer = { verdict: "refuse", items: [{ verdict: "refuse", why: "code" }] };
      expect(await gate.ask!("Mac", requests)).toEqual(answer);
      const bad = [
        { ...typed, title: "" },
        { ...typed, items: [] },
        { ...typed, verdict: "maybe" },
        { verdict: "pass" },
        { ...typed, items: [null] },
        { ...typed, items: [{ verdict: "typed" }] }, // not worded
        { ...typed, items: [{ verdict: "maybe", text: "x" }] },
        { ...typed, items: [{ verdict: "typed", text: "x", entity_ids: [1] }] },
        { ...typed, items: [{ verdict: "pass" }] }, // the answer says typed, no item is
        { verdict: "pass", items: [{ verdict: "typed" }] },
      ];
      for (const shape of bad) {
        answer = shape;
        expect(await gate.ask!("Mac", requests)).toBeNull();
      }
    } finally {
      await new Promise((r) => server.close(r));
    }
  });

  it("asks the linked phone through the worker, hangs up on stop, and takes only its answer shapes", async () => {
    let asked: unknown;
    let answer: unknown = {};
    let hold = false;
    let hungUp = 0;
    const server = createServer((request, response) => {
      const chunks: Buffer[] = [];
      request.on("data", (c: Buffer) => chunks.push(c));
      request.on("end", () => {
        asked = JSON.parse(Buffer.concat(chunks).toString("utf8"));
        if (hold) {
          response.on("close", () => (hungUp += 1));
          return;
        }
        response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(answer));
      });
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker();
      gate.start();
      const link = { service: "mobile_app_iphone", device_id: "ha-iphone", user_id: "u1" };
      const text = { lang: "en", title: "Unlock “Front”?", body: "Requested by “Mac”.", button: "Unlock" };
      const go = () => new AbortController().signal;
      answer = { answer: "approved", user_id: "u1" };
      expect(await gate.phone!(link, text, 5_000, go())).toEqual(answer);
      expect(asked).toEqual({ ...text, link, wait_ms: 5_000 });
      for (const [shape, want] of [
        [{ answer: "declined" }, { answer: "declined" }],
        [{ answer: "none", user_id: "u1" }, { answer: "none" }],
        [{ answer: "approved" }, null],
        [{ answer: "approved", user_id: "" }, null],
        [{ answer: "maybe" }, null],
      ] as const) {
        answer = shape;
        expect(await gate.phone!(link, text, 5_000, go())).toEqual(want);
      }
      hold = true;
      const stop = new AbortController();
      const pending = gate.phone!(link, text, 5_000, stop.signal);
      await new Promise((r) => setTimeout(r, 50));
      stop.abort();
      expect(await pending).toBeNull();
      await new Promise((r) => setTimeout(r, 50));
      expect(hungUp).toBe(1);
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  });

  it("a nested check is answered while the outer one is still open (the worker's own read under the gate)", async () => {
    const held: (() => void)[] = [];
    const kinds: string[] = [];
    const server = createServer((request, response) => {
      request.resume();
      request.on("end", () => {
        const answer = (kind: string) => () => response.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify({ verdict: "pass", kind }));
        if (held.length === 0) {
          held.push(answer("write")); // the outer check waits for its nested read's check
          return;
        }
        answer(kinds.shift() ?? "read")();
      });
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker({ timeoutMs: 5_000 });
      gate.start();
      let outerDone = false;
      const outer = gate.check({ kind: "device", deviceId: "d1" }, "core", Buffer.from('{"method":"POST","path":"/api/services/climate/set_temperature","body":{}}')).finally(() => (outerDone = true));
      while (held.length === 0) {
        await new Promise((r) => setTimeout(r, 5));
      }
      const inner = await gate.check({ kind: "device", deviceId: "gate", internal: "gate", peer: "127.0.0.1" }, "core", Buffer.from('{"method":"GET","path":"/api/states"}'));
      expect(inner).toEqual({ verdict: "pass", kind: "read" });
      kinds.push("probe");
      const probe = await gate.check({ kind: "device", deviceId: "gate", internal: "gate", peer: "127.0.0.1" }, "core", Buffer.from('{"method":"DELETE","path":"/api/config/config_entries/options/flow/f1"}'));
      expect(probe).toEqual({ verdict: "pass", kind: "probe" });
      expect(outerDone).toBe(false);
      held[0]!();
      expect(await outer).toEqual({ verdict: "pass", kind: "write" });
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  });

  it("a redirect from the worker is no answer, and nothing follows it", async () => {
    const followed: string[] = [];
    const server = createServer((request, response) => {
      if (request.url === "/elsewhere") {
        followed.push(request.url);
        response.writeHead(200, { "content-type": "application/json" }).end('{"verdict":"pass","kind":"read","response_b64":"e30="}');
        return;
      }
      response.writeHead(307, { location: "/elsewhere" }).end();
    });
    mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
    await new Promise<void>((r) => server.listen(join(dir, "gate", "worker.sock"), r));
    try {
      const gate = worker();
      gate.start();
      expect(await gate.check({ kind: "shared" }, "ws", Buffer.from("{}"))).toBeNull();
      expect(await gate.redact("ws", Buffer.from("{}"), Buffer.from("{}"))).toBeNull();
      expect(followed).toEqual([]);
    } finally {
      await new Promise((r) => server.close(r));
    }
  });

  it.each([
    ["every person's slot is taken", MAX_ACTIVE_DEVICES, true],
    ["every pairing slot is waiting", MAX_PENDING_DEVICES, false],
  ])("the worker still gets its credential when %s, and takes no slot itself", (_name, count, activate) => {
    const pair = (i: number) => {
      const c = generateCredential();
      registry.createPending({ deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: `install-${i}`, name: "Phone", platform: "iOS", client: "x", createdAtMs: 1 }, 1);
      if (activate) {
        registry.activate(c.deviceId, 1);
      }
    };
    for (let i = 0; i < count; i++) {
      pair(i);
    }
    worker().start();
    expect(spawned).toHaveLength(1);
    expect(gateDevices().filter((d) => d.state === "active")).toHaveLength(1);
    expect(() => pair(count)).toThrow(/limit reached/); // still a person's cap
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
