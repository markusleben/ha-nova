import { EventEmitter } from "node:events";
import { createServer as createHttp, type IncomingMessage, type Server } from "node:http";
import { createServer as createHttps, request as httpsRequest } from "node:https";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { HttpError } from "../../nova/src/http/errors.js";
import { gated, type GateEndpoint } from "../../nova/src/http/gate.js";
import { parseCoreRequest } from "../../nova/src/http/handlers/core-proxy.js";
import type { RouteHandler } from "../../nova/src/http/router.js";
import { createGateWorker, mintGateDevice, type GateWorker } from "../../nova/src/runtime/gate-worker.js";
import { createBootstrapListener, createDeviceListener } from "../../nova/src/runtime/listeners.js";
import { digestSecret, generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { BRIDGE_INSTALL_ID, GATE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import type { PairingV1Manager } from "../../nova/src/security/pairing-v1.js";
import { loadOrCreateTlsIdentity, type TlsIdentity } from "../../nova/src/security/tls-identity.js";

// The gate in front of the device-facing handlers (layer 3 of
// docs/work/2026-11-03-relay-typed-gate-spec.md): the real listeners and
// request pipeline, the real worker client, a fake `ha-nova gate` worker.

type Reply = { status?: number; json?: unknown; text?: string } | "hang";
interface Seen { path: string; auth: string | undefined; body: Record<string, string> }

let dir: string;
let registry: DeviceRegistry;
let tls: TlsIdentity;
let servers: Server[];
let fakeWorker: Server;
let seen: Seen[];
let reply: (path: string, body: Record<string, string>) => Reply;
let children: (EventEmitter & { kill: () => boolean })[];
let gate: GateWorker;
let forwarded: { endpoint: GateEndpoint; body: unknown }[];
let answer: unknown;
let bootBase: string;
let devicePort: number;
let devicePeer: string | undefined;

const decode = (b64: string): string => Buffer.from(b64, "base64").toString("utf8");
const verdict = (v: string, kind = "write"): Reply => ({ json: { verdict: v, kind } });
const redactAll = (body: Record<string, string>): Reply => ({ json: { response_b64: Buffer.from(decode(body.response_b64!).replaceAll("SECRET", "HIDDEN")).toString("base64") } });

async function listen(server: Server): Promise<number> {
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return (server.address() as { port: number }).port;
}

function device(installId: string): string {
  const c = generateCredential();
  registry.createPending({ deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: installId, name: "n", platform: "p", client: "c", createdAtMs: 1 }, 1);
  registry.activate(c.deviceId, 1);
  return c.credential;
}

function upstream(endpoint: GateEndpoint): RouteHandler {
  return ({ body }) => {
    forwarded.push({ endpoint, body });
    if (answer instanceof HttpError) {
      throw answer;
    }
    return answer;
  };
}

beforeEach(async () => {
  // A short folder: a Unix socket path holds at most 104 bytes (macOS).
  dir = mkdtempSync(join(process.platform === "win32" ? tmpdir() : "/tmp", "hn-gate-"));
  registry = openDeviceRegistry(dir);
  tls = await loadOrCreateTlsIdentity(dir);
  servers = [];
  seen = [];
  forwarded = [];
  children = [];
  answer = { result: "SECRET" };
  devicePeer = undefined;
  reply = (path, body) => (path === "/check" ? verdict("pass") : redactAll(body));
  fakeWorker = createHttp((request, response) => {
    let raw = "";
    request.on("data", (chunk) => (raw += chunk));
    request.on("end", () => {
      const body = JSON.parse(raw) as Record<string, string>;
      seen.push({ path: request.url ?? "", auth: request.headers.authorization, body });
      const r = reply(request.url ?? "", body);
      if (r === "hang") {
        return;
      }
      response.statusCode = r.status ?? 200;
      response.end(r.text ?? JSON.stringify(r.json));
    });
  });
  servers.push(fakeWorker);
  mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
  await new Promise<void>((resolve) => fakeWorker.listen(join(dir, "gate", "worker.sock"), resolve));
  gate = createGateWorker({
    dataDir: dir,
    binary: process.execPath,
    mint: mintGateDevice(registry, () => 1, "https://127.0.0.1:8792", tls.spkiPin),
    logger: { warn: () => {}, error: () => {} },
    timeoutMs: 300,
    respawnMs: 20,
    spawn: (() => {
      const child = Object.assign(new EventEmitter(), { kill: () => true });
      children.push(child);
      return child;
    }) as never,
  });
  gate.start();
  const functional = {
    health: () => ({}),
    ws: gated("ws", upstream("ws"), gate),
    core: gated("core", upstream("core"), gate, parseCoreRequest),
    files: gated("files", upstream("files"), gate),
    backups: gated("backups", upstream("backups"), gate),
    supervisor: gated("supervisor", upstream("supervisor"), gate),
  };
  const deps = { registry, pairingManager: { getStatus: () => ({ phase: "inactive" }) } as unknown as PairingV1Manager, functional, relayVersion: "0.29.0", now: () => 1 };
  bootBase = `http://127.0.0.1:${await listen(createHttp(createBootstrapListener(deps)))}`;
  const deviceListener = createDeviceListener(deps);
  devicePort = await listen(createHttps({ key: tls.keyPem, cert: tls.certPem }, (request: IncomingMessage, response) => {
    if (devicePeer !== undefined) {
      Object.defineProperty(request.socket, "remoteAddress", { configurable: true, value: devicePeer });
    }
    deviceListener(request, response);
  }) as unknown as Server);
  registry.importLegacy(digestSecret("legacy-token"), 1);
});

afterEach(async () => {
  for (const server of servers) {
    server.closeAllConnections?.();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
  rmSync(dir, { recursive: true, force: true });
});

async function viaBootstrap(path: string, body: unknown): Promise<{ status: number; text: string }> {
  const response = await fetch(`${bootBase}${path}`, { method: "POST", headers: { authorization: "Bearer legacy-token", "content-type": "application/json" }, body: JSON.stringify(body) });
  return { status: response.status, text: await response.text() };
}

function viaDevice(path: string, body: unknown, credential: string): Promise<{ status: number; text: string }> {
  return new Promise((resolve, reject) => {
    const request = httpsRequest(
      { host: "127.0.0.1", port: devicePort, path, method: "POST", ca: tls.certPem, checkServerIdentity: () => undefined, headers: { authorization: `Bearer ${credential}`, "content-type": "application/json" } },
      (response) => {
        let text = "";
        response.on("data", (chunk) => (text += chunk));
        response.on("end", () => resolve({ status: response.statusCode ?? 0, text }));
      },
    );
    request.on("error", reject);
    request.end(JSON.stringify(body));
  });
}

const code = (text: string): string => (JSON.parse(text) as { error: { code: string } }).error.code;
const checks = (): Seen[] => seen.filter((s) => s.path === "/check");

describe("relay gate: verdicts", () => {
  it("pass: forwards exactly the rated bytes, and the answer comes back redacted", async () => {
    const body = { type: "call_service", domain: "light", service: "turn_on", service_data: { entity_id: "light.kitchen" } };
    const res = await viaBootstrap("/ws", body);
    expect(res.status).toBe(200);
    expect(JSON.parse(res.text)).toEqual({ ok: true, data: { result: "HIDDEN" } });
    expect(forwarded).toEqual([{ endpoint: "ws", body }]);
    const [check, redact] = seen;
    expect(check).toMatchObject({ path: "/check", body: { principal: { kind: "legacy" }, endpoint: "ws" } });
    expect(decode(check!.body.body_b64!)).toBe(JSON.stringify(body));
    expect(check!.auth).toMatch(/^Bearer [0-9a-f]{64}$/);
    expect(redact).toMatchObject({ path: "/redact", auth: check!.auth, body: { endpoint: "ws", request_b64: check!.body.body_b64 } });
    expect(decode(redact!.body.response_b64!)).toBe(JSON.stringify({ ok: true, data: { result: "SECRET" } }));
  });

  it("rates every endpoint under its own name, reads included", async () => {
    for (const endpoint of ["ws", "files", "backups", "supervisor"] as const) {
      expect((await viaBootstrap(`/${endpoint}`, { action: "list_dir", path: "." })).status).toBe(200);
    }
    expect(checks().map((s) => s.body.endpoint)).toEqual(["ws", "files", "backups", "supervisor"]);
    expect(forwarded.map((f) => f.endpoint)).toEqual(["ws", "files", "backups", "supervisor"]);
  });

  it("core: every method is rated after the Relay's own normalization, and that request is forwarded", async () => {
    for (const method of ["get", "POST", "DELETE"]) {
      expect((await viaBootstrap("/core", { method, path: "/api/%73tates/light.x?y=1", body: { a: 1 } })).status).toBe(200);
    }
    const rated = checks().map((s) => JSON.parse(decode(s.body.body_b64!)) as unknown);
    expect(rated).toEqual([
      { method: "GET", path: "/api/states/light.x?y=1", body: { a: 1 } },
      { method: "POST", path: "/api/states/light.x?y=1", body: { a: 1 } },
      { method: "DELETE", path: "/api/states/light.x?y=1", body: { a: 1 } },
    ]);
    expect(forwarded.map((f) => f.body)).toEqual(rated);
  });

  it("typed: 428 with the update line, nothing forwarded, on every method", async () => {
    reply = () => verdict("typed");
    for (const method of ["GET", "POST", "DELETE"]) {
      const res = await viaBootstrap("/core", { method, path: "/api/services/lock/unlock", body: { entity_id: "lock.front" } });
      expect(res.status).toBe(428);
      expect(JSON.parse(res.text).error).toEqual({ code: "GATE_APPROVAL_REQUIRED", message: "Only a person may confirm this, on their phone or in Home Assistant. Update HA NOVA on this computer: ha-nova update" });
    }
    const envelope = await viaBootstrap("/ws", { message: { type: "call_service", domain: "lock", service: "unlock" }, collect_events: {} });
    expect(envelope.status).toBe(428);
    expect(forwarded).toEqual([]);
  });

  it("refuse: 422 GATE_REFUSED, nothing forwarded", async () => {
    reply = () => ({ json: { verdict: "refuse", kind: "write", why: "fire_event" } });
    const res = await viaBootstrap("/ws", { type: "fire_event", event_type: "x" });
    expect(res.status).toBe(422);
    expect(JSON.parse(res.text).error).toEqual({ code: "GATE_REFUSED", message: "HA NOVA's safety check refused this request (fire_event); nothing was sent." });
    reply = () => ({ json: { verdict: "refuse", kind: "write", why: "webhook abc123 SECRET" } });
    expect((await viaBootstrap("/ws", { type: "fire_event" })).text).not.toContain("SECRET");
    expect(forwarded).toEqual([]);
  });

  it("an answer from a worker that stopped meanwhile does not count", async () => {
    reply = (path) => {
      children[0]!.emit("exit", 1, null);
      return path === "/check" ? verdict("pass") : { json: {} };
    };
    const res = await viaBootstrap("/ws", { type: "get_states" });
    expect(res.status).toBe(503);
    expect(forwarded).toEqual([]);
    await new Promise((resolve) => setTimeout(resolve, 100)); // its restart lands inside this test
    expect(children).toHaveLength(2);
  });

  it.each<[string, Reply]>([
    ["an unknown verdict", verdict("maybe")],
    ["an unknown kind", { json: { verdict: "pass", kind: "both" } }],
    ["a missing kind", { json: { verdict: "pass" } }],
    ["a missing verdict", { json: { kind: "read" } }],
    ["an array", { json: [] }],
    ["text", { text: "pass" }],
    ["a non-200 pass", { status: 500, json: { verdict: "pass", kind: "read" } }],
    ["a 401", { status: 401, json: {} }],
    ["no answer within the timeout", "hang"],
  ])("%s: 503 GATE_UNAVAILABLE, nothing forwarded", async (_name, bad) => {
    reply = () => bad;
    const res = await viaBootstrap("/files", { action: "read_file", path: "configuration.yaml" });
    expect(res.status).toBe(503);
    expect(code(res.text)).toBe("GATE_UNAVAILABLE");
    expect(forwarded).toEqual([]);
  });

  it("a refused connection: 503, nothing forwarded", async () => {
    fakeWorker.closeAllConnections();
    await new Promise<void>((resolve) => fakeWorker.close(() => resolve()));
    servers = servers.filter((s) => s !== fakeWorker);
    const res = await viaBootstrap("/ws", { type: "get_states" });
    expect(res.status).toBe(503);
    expect(forwarded).toEqual([]);
  });

  it("a crashed worker: 503 without asking anyone, and it is started again", async () => {
    children[0]!.emit("exit", 1, null);
    const res = await viaBootstrap("/ws", { type: "get_states" });
    expect(res.status).toBe(503);
    expect(seen).toEqual([]);
    expect(forwarded).toEqual([]);
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(children).toHaveLength(2);
    expect((await viaBootstrap("/ws", { type: "get_states" })).status).toBe(200);
  });
});

describe("relay gate: redaction", () => {
  it.each<[string, Reply]>([
    ["a 500", { status: 500, json: { response_b64: "" } }],
    ["no response_b64", { json: {} }],
    ["invalid base64", { json: { response_b64: "%%%" } }],
    ["bytes that are not JSON", { json: { response_b64: Buffer.from("SECRET").toString("base64") } }],
    ["no answer within the timeout", "hang"],
  ])("%s from /redact: 502 GATE_ANSWER_WITHHELD and none of the answer", async (_name, bad) => {
    reply = (path) => (path === "/check" ? verdict("pass") : bad);
    const res = await viaBootstrap("/ws", { type: "config/entity_registry/list" });
    expect(res.status).toBe(502);
    expect(JSON.parse(res.text).error).toEqual({ code: "GATE_ANSWER_WITHHELD", message: "Home Assistant may have carried this out; HA NOVA withheld the answer" });
    expect(res.text).not.toContain("SECRET");
    expect(forwarded).toHaveLength(1);
  });

  it("a worker that stops between the check and the redact: 502, the request was forwarded", async () => {
    reply = (path) => {
      if (path === "/redact") {
        children[0]!.emit("exit", 1, null);
      }
      return path === "/check" ? verdict("pass") : redactAll({ response_b64: "" });
    };
    const res = await viaBootstrap("/ws", { type: "get_states" });
    expect(res.status).toBe(502);
    expect(code(res.text)).toBe("GATE_ANSWER_WITHHELD");
    expect(forwarded).toHaveLength(1);
    await new Promise((resolve) => setTimeout(resolve, 100)); // its restart lands inside this test
  });

  it("redacts an upstream error answer with its status", async () => {
    answer = new HttpError(502, "UPSTREAM_WS_ERROR", "SECRET in an error");
    const res = await viaBootstrap("/ws", { type: "get_states" });
    expect(res.status).toBe(502);
    expect(JSON.parse(res.text)).toEqual({ ok: false, error: { code: "UPSTREAM_WS_ERROR", message: "HIDDEN in an error" } });
  });

  it("passes a binary core answer (camera frame) unchanged", async () => {
    answer = { status: 200, body: "U0VDUkVU", body_encoding: "base64", content_type: "image/jpeg" };
    const res = await viaBootstrap("/core", { method: "GET", path: "/api/camera_proxy/camera.door" });
    expect(JSON.parse(res.text)).toEqual({ ok: true, data: answer });
    expect(seen.map((s) => s.path)).toEqual(["/check"]);
  });
});

describe("relay gate: the App's own credentials", () => {
  it("the chat bridge from 127.0.0.1 skips the check and redaction", async () => {
    const bridge = device(BRIDGE_INSTALL_ID);
    const res = await viaDevice("/ws", { type: "call_service", domain: "lock", service: "unlock" }, bridge);
    expect(res.status).toBe(200);
    expect(JSON.parse(res.text)).toEqual({ ok: true, data: { result: "SECRET" } });
    expect(seen).toEqual([]);
  });

  it("the chat bridge's credential from a LAN peer is refused and nothing is forwarded", async () => {
    const bridge = device(BRIDGE_INSTALL_ID);
    devicePeer = "192.168.1.20";
    const res = await viaDevice("/ws", { type: "get_states" }, bridge);
    expect(res.status).toBe(401);
    expect(forwarded).toEqual([]);
  });

  it("a paired device from 127.0.0.1 is gated like any other", async () => {
    reply = () => verdict("typed");
    const res = await viaDevice("/ws", { type: "call_service", domain: "lock", service: "unlock" }, device("laptop-1"));
    expect(res.status).toBe(428);
    expect(checks()[0]!.body.principal).toMatchObject({ kind: "device" });
    expect(forwarded).toEqual([]);
  });

  it("a device revoked while the worker checks sends nothing", async () => {
    const credential = device("laptop-2");
    const id = registry.list().find((d) => d.clientInstallId === "laptop-2")!.deviceId;
    reply = (path, body) => {
      registry.revoke(id);
      return path === "/check" ? verdict("pass") : redactAll(body);
    };
    const res = await viaDevice("/ws", { type: "call_service", domain: "light", service: "turn_on" }, credential);
    expect(res.status).toBe(401);
    expect(forwarded).toEqual([]);
  });

  it("the gate worker: its phone ask and the phone's answer go out unchecked", async () => {
    const own = device(GATE_INSTALL_ID);
    const notify = { method: "POST", path: "/api/services/notify/mobile_app_pixel", body: { message: "Unlock?" } };
    const collect = { message: { type: "subscribe_events", event_type: "mobile_app_notification_action" }, collect_events: { max_events: 1 } };
    expect((await viaDevice("/core", notify, own)).status).toBe(200);
    expect((await viaDevice("/ws", collect, own)).status).toBe(200);
    expect(seen).toEqual([]);
    expect(forwarded.map((f) => f.body)).toEqual([notify, collect]);
  });

  it("the gate worker: anything else only as a passed read, unredacted", async () => {
    const own = device(GATE_INSTALL_ID);
    reply = () => verdict("pass", "read");
    const read = await viaDevice("/ws", { type: "config/entity_registry/list" }, own);
    expect(JSON.parse(read.text)).toEqual({ ok: true, data: { result: "SECRET" } });
    expect(seen.map((s) => s.path)).toEqual(["/check"]);
    for (const [v, kind] of [["pass", "write"], ["typed", "read"], ["typed", "write"], ["refuse", "read"]]) {
      reply = () => verdict(v!, kind);
      const res = await viaDevice("/ws", { type: "call_service", domain: "notify", service: "persistent_notification" }, own);
      expect(res.status).toBe(422);
    }
    reply = () => ({ text: "garbage" });
    expect((await viaDevice("/core", { method: "GET", path: "/api/states" }, own)).status).toBe(503);
    expect(forwarded).toHaveLength(1);
  });

  it("the gate worker: a notify to something else, or the answer in another wrapper, is rated", async () => {
    const own = device(GATE_INSTALL_ID);
    reply = () => verdict("refuse");
    const bodies = [
      { type: "call_service", domain: "notify", service: "mobile_app_x" },
      { message: { type: "call_service", domain: "notify", service: "mobile_app_x" } },
      { message: { type: "subscribe_events", event_type: "mobile_app_notification_action", extra: 1 }, collect_events: {} },
      { message: { type: "subscribe_events", event_type: "state_changed" }, collect_events: {} },
      { message: { type: "subscribe_events" }, collect_events: {} },
    ];
    for (const body of bodies) {
      expect((await viaDevice("/ws", body, own)).status).toBe(422);
    }
    const cores = [
      { method: "POST", path: "/api/services/notify/notify", body: {} },
      { method: "POST", path: "/api/services/lock/mobile_app_x", body: {} },
      { method: "POST", path: "/api/services/notify/mobile_app_x?return_response", body: {} },
      { method: "POST", path: "/api/services/notify/mobile_app_x" },
      { method: "GET", path: "/api/services/notify/mobile_app_x", body: {} },
    ];
    for (const body of cores) {
      expect((await viaDevice("/core", body, own)).status).toBe(422);
    }
    expect(checks()).toHaveLength(bodies.length + cores.length);
    expect(forwarded).toEqual([]);
  });

  it("the gate worker's credential from a LAN peer is refused", async () => {
    const own = device(GATE_INSTALL_ID);
    devicePeer = "192.168.1.20";
    const notify = { method: "POST", path: "/api/services/notify/mobile_app_pixel", body: {} };
    expect((await viaDevice("/core", notify, own)).status).toBe(401);
    expect(forwarded).toEqual([]);
  });
});
