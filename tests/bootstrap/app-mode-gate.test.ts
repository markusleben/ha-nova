import { createServer, type Server } from "node:http";
import { request as httpsRequest } from "node:https";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { buildAppMode, type AppModeInput, type AppModeRuntime } from "../../nova/src/runtime/app-mode.js";
import { digestSecret, generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry } from "../../nova/src/security/device-registry.js";
import { loadOrCreateRelayInstanceId } from "../../nova/src/storage/relay-instance.js";

// The App's real assembly puts the safety check in front of every
// device-facing handler on all three listeners. Its worker never started here
// (listen() is not called), the state a missing or dead worker leaves: every
// gated request is 503 and nothing reaches Home Assistant or the Supervisor.

let dir: string;
let supervisor: Server;
let runtime: AppModeRuntime;
let upstream: string[];
let ports: { bootstrap: number; device: number; ingress: number };
const prevBase = process.env.HA_NOVA_SUPERVISOR_BASE;

async function listen(server: Server): Promise<number> {
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return (server.address() as { port: number }).port;
}

beforeEach(async () => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-app-gate-"));
  upstream = [];
  supervisor = createServer((request, response) => {
    if (request.url !== "/addons/self/info" && request.url !== "/addons/self/options") {
      upstream.push(`supervisor ${request.method} ${request.url}`);
    }
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify({ result: "ok", data: { network: { "8792/tcp": 8792 } } }));
  });
  process.env.HA_NOVA_SUPERVISOR_BASE = `http://127.0.0.1:${await listen(supervisor)}`;
});

afterEach(async () => {
  for (const server of [supervisor, ...Object.values(runtime?.servers ?? {})]) {
    server.closeAllConnections?.();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
  if (prevBase === undefined) {
    delete process.env.HA_NOVA_SUPERVISOR_BASE;
  } else {
    process.env.HA_NOVA_SUPERVISOR_BASE = prevBase;
  }
  rmSync(dir, { recursive: true, force: true });
});

function seedDevice(registry: ReturnType<typeof openDeviceRegistry>, installId: string) {
  const c = generateCredential();
  registry.createPending({ deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: installId, name: "n", platform: "p", client: "c", createdAtMs: 1 }, 1);
  registry.activate(c.deviceId, 1);
  return c;
}

async function build(): Promise<{ local: string; link: string }> {
  const relayInstanceId = loadOrCreateRelayInstanceId(dir);
  const registry = openDeviceRegistry(dir);
  registry.importLegacy(digestSecret("legacy-token"), 1);
  const local = seedDevice(registry, "laptop-1");
  const link = seedDevice(registry, "laptop-2");
  expect(registry.bindCloudUser(link.deviceId, link.secretDigest, "user-1", relayInstanceId).ok).toBe(true);
  const input: AppModeInput = {
    supervisorToken: "supervisor-token",
    relayVersion: "0.29.0",
    cloudRemoteEnabled: true,
    wsClient: {
      sendMessage: async (message: { type: string }) => {
        upstream.push(`ws ${message.type}`);
        return [];
      },
      collectMessageEvents: async () => {
        upstream.push("ws collect");
        return { events: [], truncated: false };
      },
      isConnected: () => true,
      getConnectionStatus: () => ({ connected: true, disconnectReason: null }),
    } as unknown as AppModeInput["wsClient"],
    coreClient: {
      request: async (request) => {
        upstream.push(`core ${request.method} ${request.path}`);
        return { status: 200, body: {} };
      },
    },
    fileAccess: { mode: "readwrite", configRoot: dir, warnings: [] },
    snapshotRoot: join(dir, "snapshots"),
    appOptionsPath: join(dir, "options.json"),
    startedAtMs: 1,
    now: () => 5_000,
    logger: { info: () => {}, warn: () => {}, error: () => {} },
  };
  runtime = await buildAppMode(input);
  runtime.servers.ingress.prependListener("request", (request) => {
    Object.defineProperty(request.socket, "remoteAddress", { configurable: true, value: "172.30.32.2" });
  });
  ports = {
    bootstrap: await listen(runtime.servers.bootstrap),
    device: await listen(runtime.servers.device),
    ingress: await listen(runtime.servers.ingress),
  };
  return { local: local.credential, link: link.credential };
}

function viaDevice(path: string, body: unknown, credential: string): Promise<{ status: number; text: string }> {
  return new Promise((resolve, reject) => {
    const request = httpsRequest(
      { host: "127.0.0.1", port: ports.device, path, method: "POST", rejectUnauthorized: false, headers: { authorization: `Bearer ${credential}`, "content-type": "application/json" } },
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

async function viaHttp(port: number, path: string, body: unknown, credential: string, ingress = false): Promise<{ status: number; text: string }> {
  const response = await fetch(`http://127.0.0.1:${port}${path}`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${credential}`,
      "content-type": "application/json",
      ...(ingress ? { "x-ingress-path": "/api/hassio_ingress/session", "x-remote-user-id": "user-1" } : {}),
    },
    body: JSON.stringify(body),
  });
  return { status: response.status, text: await response.text() };
}

const requests: [string, unknown][] = [
  ["/ws", { type: "get_states" }],
  ["/ws", { message: { type: "subscribe_events", event_type: "state_changed" }, collect_events: { max_events: 1 } }],
  ["/core", { method: "GET", path: "/api/states" }],
  ["/core", { method: "POST", path: "/api/services/lock/unlock", body: {} }],
  ["/core", { method: "DELETE", path: "/api/config/automation/config/x" }],
  ["/files", { action: "read_file", path: "configuration.yaml" }],
  ["/backups", { action: "list" }],
  ["/supervisor", { method: "GET", path: "/addons" }],
];

describe("app mode: the safety check fronts every device-facing handler", () => {
  it("answers 503 GATE_UNAVAILABLE on every listener while the worker isn't running, and sends nothing", async () => {
    const { local, link } = await build();
    const answers: { where: string; status: number; code: string }[] = [];
    const record = (where: string, res: { status: number; text: string }) =>
      answers.push({ where, status: res.status, code: (JSON.parse(res.text) as { error?: { code: string } }).error?.code ?? "" });
    for (const [path, body] of requests) {
      record(`bootstrap ${path}`, await viaHttp(ports.bootstrap, path, body, "legacy-token"));
      record(`device ${path}`, await viaDevice(path, body, local));
      if (path !== "/supervisor") {
        record(`ingress ${path}`, await viaHttp(ports.ingress, path, body, link, true));
      }
    }
    expect(answers).toHaveLength(requests.length * 3 - 1);
    expect(answers.filter((a) => a.status !== 503 || a.code !== "GATE_UNAVAILABLE")).toEqual([]);
    expect(upstream).toEqual([]);
  });

  it("leaves /health ungated", async () => {
    const { local } = await build();
    const response = await new Promise<number>((resolve, reject) => {
      const request = httpsRequest({ host: "127.0.0.1", port: ports.device, path: "/health", rejectUnauthorized: false, headers: { authorization: `Bearer ${local}` } }, (res) => {
        res.resume();
        resolve(res.statusCode ?? 0);
      });
      request.on("error", reject);
      request.end();
    });
    expect(response).toBe(200);
  });
});
