import { EventEmitter } from "node:events";
import { createServer, type Server } from "node:http";
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createApp } from "../../nova/src/index.js";
import { createGateWorker } from "../../nova/src/runtime/gate-worker.js";
import { bootstrapRuntime, type RuntimeBootstrapResult } from "../../nova/src/runtime/start.js";

// The standalone container (single listener, shared token) gates the same
// handlers through the same worker: the shared token is a legacy principal,
// and the worker reaches the Relay with its own loopback-only token.

let dir: string;
let servers: Server[];
let forwarded: string[];
let checks: Record<string, unknown>[];
let verdict: string;
let children: (EventEmitter & { kill: () => boolean })[];
let peer: string | undefined;

async function listen(server: Server): Promise<string> {
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return `http://127.0.0.1:${(server.address() as { port: number }).port}`;
}

const upstream = {
  wsClient: {
    isConnected: () => true,
    sendMessage: async (message: { type: string }) => {
      forwarded.push(`ws ${message.type}`);
      return { secret: "SECRET" };
    },
    collectMessageEvents: async () => {
      forwarded.push("ws collect");
      return { events: [], truncated: false };
    },
  },
  coreClient: {
    request: async (request: { method: string; path: string }) => {
      forwarded.push(`core ${request.method} ${request.path}`);
      return { status: 200, body: {} };
    },
  },
};

beforeEach(() => {
  // A short folder: a Unix socket path holds at most 104 bytes (macOS).
  dir = mkdtempSync(join(process.platform === "win32" ? tmpdir() : "/tmp", "hn-sgate-"));
  servers = [];
  forwarded = [];
  checks = [];
  children = [];
  verdict = "pass";
  peer = undefined;
});

afterEach(async () => {
  for (const server of servers) {
    server.closeAllConnections?.();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
  rmSync(dir, { recursive: true, force: true });
});

async function post(base: string, path: string, body: unknown, token: string): Promise<{ status: number; text: string }> {
  const response = await fetch(`${base}${path}`, { method: "POST", headers: { authorization: `Bearer ${token}`, "content-type": "application/json" }, body: JSON.stringify(body) });
  return { status: response.status, text: await response.text() };
}

describe("standalone Relay: the safety check", () => {
  it("without a worker every device-facing request is 503 and nothing is sent; /health stays", async () => {
    const app = createApp({ authToken: "relay-token", version: "0.29.0", fileAccess: { mode: "readwrite", configRoot: dir, warnings: [] }, snapshotRoot: join(dir, "snapshots"), ...upstream });
    const base = await listen(app.server);
    const bodies: [string, unknown][] = [
      ["/ws", { type: "get_states" }],
      ["/core", { method: "GET", path: "/api/states" }],
      ["/core", { method: "POST", path: "/api/services/lock/unlock" }],
      ["/core", { method: "DELETE", path: "/api/config/automation/config/x" }],
      ["/files", { action: "read_file", path: "configuration.yaml" }],
      ["/backups", { action: "list" }],
    ];
    for (const [path, body] of bodies) {
      const res = await post(base, path, body, "relay-token");
      expect([path, res.status, (JSON.parse(res.text) as { error: { code: string } }).error.code]).toEqual([path, 503, "GATE_UNAVAILABLE"]);
    }
    expect(forwarded).toEqual([]);
    expect((await fetch(`${base}/health`, { headers: { authorization: "Bearer relay-token" } })).status).toBe(200);
  });

  describe("with its worker", () => {
    let runtime: RuntimeBootstrapResult;
    let base: string;

    beforeEach(async () => {
      const worker = createServer((request, response) => {
        let raw = "";
        request.on("data", (chunk) => (raw += chunk));
        request.on("end", () => {
          const body = JSON.parse(raw) as Record<string, string>;
          if (request.url === "/check") {
            checks.push(body);
            response.end(JSON.stringify({ verdict, kind: "write" }));
            return;
          }
          response.end(JSON.stringify({ response_b64: Buffer.from(Buffer.from(body.response_b64!, "base64").toString().replaceAll("SECRET", "HIDDEN")).toString("base64") }));
        });
      });
      servers.push(worker);
      mkdirSync(join(dir, "gate"), { recursive: true, mode: 0o700 });
      await new Promise<void>((resolve) => worker.listen(join(dir, "gate", "worker.sock"), resolve));
      runtime = bootstrapRuntime({
        loadEnv: () => ({ relayAuthToken: "relay-token", haLlat: "llat", haUrl: "http://ha", relayVersion: "0.29.0", cloudRemoteEnabled: false, appOptionsPath: join(dir, "options.json"), relayPort: 8791, logLevel: "info", snapshotDir: join(dir, "snapshots") }),
        readAppOptions: () => ({}),
        createWsClient: () => upstream.wsClient as never,
        createRestClient: () => upstream.coreClient as never,
        createGate: (deps) =>
          createGateWorker({
            ...deps,
            binary: process.execPath,
            respawnMs: 10,
            spawn: (() => {
              const child = Object.assign(new EventEmitter(), { kill: () => true });
              children.push(child);
              return child;
            }) as never,
          }),
      });
      runtime.app.server.prependListener("request", (request) => {
        if (peer !== undefined) {
          Object.defineProperty(request.socket, "remoteAddress", { configurable: true, value: peer });
        }
      });
      base = await listen(runtime.app.server);
      runtime.gate!.start();
    });

    const gateToken = () => (JSON.parse(readFileSync(join(dir, "gate", "relay.json"), "utf8")) as { credential: string }).credential;

    it("rates the shared token as a legacy principal and redacts its answer", async () => {
      const res = await post(base, "/ws", { type: "get_states" }, "relay-token");
      expect(JSON.parse(res.text)).toEqual({ ok: true, data: { secret: "HIDDEN" } });
      expect(checks[0]).toMatchObject({ principal: { kind: "legacy" }, endpoint: "ws" });
      verdict = "typed";
      expect((await post(base, "/core", { method: "POST", path: "/api/services/lock/unlock" }, "relay-token")).status).toBe(428);
      expect(forwarded).toEqual(["ws get_states"]);
    });

    it("lets the worker reach this listener from loopback only, with a token replaced on every start", async () => {
      expect(JSON.parse(readFileSync(join(dir, "gate", "relay.json"), "utf8"))).toEqual({ relay_url: "http://127.0.0.1:8791", credential: gateToken() });
      const first = gateToken();
      const ask = { method: "POST", path: "/api/services/notify/mobile_app_pixel", body: { message: "Unlock?" } };
      const res = await post(base, "/core", ask, first);
      expect(JSON.parse(res.text)).toEqual({ ok: true, data: { status: 200, body: {} } });
      expect(checks).toEqual([]);
      verdict = "typed";
      expect((await post(base, "/ws", { type: "call_service", domain: "lock", service: "unlock" }, first)).status).toBe(422);
      peer = "192.168.1.20";
      expect((await post(base, "/core", ask, first)).status).toBe(401);
      peer = undefined;
      children[0]!.emit("exit", 1, null);
      await new Promise((resolve) => setTimeout(resolve, 50));
      expect(gateToken()).not.toBe(first);
      expect((await post(base, "/core", ask, first)).status).toBe(401);
      expect(forwarded).toEqual(["core POST /api/services/notify/mobile_app_pixel"]);
    });
  });
});
