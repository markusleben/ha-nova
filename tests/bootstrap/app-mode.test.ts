import { afterEach, describe, expect, it } from "vitest";
import { createServer, type Server } from "node:http";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  buildAppMode,
  type AppModeInput,
  type AppModeRuntime,
} from "../../nova/src/runtime/app-mode.js";
import { registerCode } from "../../nova/src/security/opaque-server.js";

// This file deliberately does NOT call opaqueReady() itself: the OPAQUE WASM must
// be initialized by buildAppMode, so a worker where nothing else touched OPAQUE
// is what proves the startup await actually runs.

interface MockSupervisor {
  server: Server;
  base: string;
  optionWrites: Array<Record<string, unknown>>;
}

async function startMockSupervisor(
  mappedSecurePort: number | null,
  failInfo = false,
): Promise<MockSupervisor> {
  const optionWrites: Array<Record<string, unknown>> = [];
  const server = createServer((req, res) => {
    if (req.method === "GET" && req.url === "/addons/self/info") {
      if (failInfo) {
        res.writeHead(500);
        res.end();
        return;
      }
      const network: Record<string, number> = {};
      if (mappedSecurePort !== null) {
        network["8792/tcp"] = mappedSecurePort;
      }
      res.writeHead(200, { "content-type": "application/json" });
      res.end(
        JSON.stringify({
          data: {
            version: "0.7.0",
            version_latest: "0.7.0",
            update_available: false,
            network,
          },
        }),
      );
      return;
    }
    if (req.method === "POST" && req.url === "/addons/self/options") {
      let body = "";
      req.on("data", (chunk) => {
        body += chunk;
      });
      req.on("end", () => {
        try {
          const parsed = JSON.parse(body) as {
            options?: Record<string, unknown>;
          };
          if (parsed.options) {
            optionWrites.push(parsed.options);
          }
        } catch {
          // ignore malformed body in the mock
        }
        res.writeHead(200, { "content-type": "application/json" });
        res.end(JSON.stringify({ result: "ok" }));
      });
      return;
    }
    res.writeHead(404);
    res.end();
  });
  const port = await new Promise<number>((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      resolve(typeof addr === "object" && addr ? addr.port : 0);
    });
  });
  return { server, base: `http://127.0.0.1:${port}`, optionWrites };
}

function stubWsClient(): AppModeInput["wsClient"] {
  return {
    sendMessage: async (message: { type: string }) =>
      message.type === "config/auth/list"
        ? [
            {
              id: "owner-user",
              name: "Owner",
              is_owner: true,
              is_active: true,
              system_generated: false,
            },
          ]
        : [],
    collectMessageEvents: async () => ({ events: [], truncated: false }),
    isConnected: () => true,
    getConnectionStatus: () => ({ connected: true, disconnectReason: null }),
  } as unknown as AppModeInput["wsClient"];
}

function baseInput(dir: string): AppModeInput {
  return {
    supervisorToken: "supervisor-token",
    relayVersion: "0.7.0",
    cloudRemoteEnabled: false,
    wsClient: stubWsClient(),
    coreClient: { request: async () => ({ status: 200, body: { ok: true } }) },
    fileAccess: { mode: "off", configRoot: "", warnings: [] },
    snapshotRoot: join(dir, "snapshots"),
    appOptionsPath: join(dir, "options.json"),
    startedAtMs: 1_000,
    now: () => 5_000,
    logger: { info: () => {}, warn: () => {}, error: () => {} },
  };
}

async function closeServers(runtime: AppModeRuntime): Promise<void> {
  for (const server of Object.values(runtime.servers)) {
    await new Promise<void>((resolve) => {
      // Servers were never listen()ed here; close() still fires its callback.
      server.close(() => resolve());
    });
  }
}

describe("app mode assembly", () => {
  let cleanup: Array<() => void | Promise<void>> = [];
  const prevBase = process.env.HA_NOVA_SUPERVISOR_BASE;

  afterEach(async () => {
    for (const fn of cleanup.reverse()) {
      await fn();
    }
    cleanup = [];
    if (prevBase === undefined) {
      delete process.env.HA_NOVA_SUPERVISOR_BASE;
    } else {
      process.env.HA_NOVA_SUPERVISOR_BASE = prevBase;
    }
  });

  async function build(
    dir: string,
    mock: MockSupervisor,
    cloudRemoteEnabled = false,
  ): Promise<AppModeRuntime> {
    process.env.HA_NOVA_SUPERVISOR_BASE = mock.base;
    const runtime = await buildAppMode({
      ...baseInput(dir),
      cloudRemoteEnabled,
    });
    cleanup.push(() => closeServers(runtime));
    return runtime;
  }

  it("assembles three listeners and initializes OPAQUE before serving", async () => {
    const dir = mkdtempSync(join(tmpdir(), "ha-nova-appmode-"));
    cleanup.push(() => rmSync(dir, { recursive: true, force: true }));
    writeFileSync(
      join(dir, "options.json"),
      JSON.stringify({ file_access: "off" }),
    );
    const mock = await startMockSupervisor(18_792);
    cleanup.push(
      () => new Promise<void>((resolve) => mock.server.close(() => resolve())),
    );

    const runtime = await build(dir, mock);

    expect(runtime.registryCorrupt).toBe(false);
    expect(runtime.servers.bootstrap).toBeDefined();
    expect(runtime.servers.device).toBeDefined();
    expect(runtime.servers.ingress).toBeDefined();
    // Every listener must carry the stalled-client timeout guards, not Node's
    // long defaults (these servers bypass createHttpServer).
    for (const server of Object.values(runtime.servers)) {
      expect(server.requestTimeout).toBeGreaterThan(0);
      expect(server.headersTimeout).toBeGreaterThan(0);
    }
    // buildAppMode awaited opaqueReady(); a synchronous OPAQUE registration now
    // works with no separate init (in a fresh worker this throws without it).
    expect(() => registerCode("123456", "owner-user")).not.toThrow();
  });

  it("clears an unused legacy ha_llat from options during startup", async () => {
    const dir = mkdtempSync(join(tmpdir(), "ha-nova-appmode-llat-"));
    cleanup.push(() => rmSync(dir, { recursive: true, force: true }));
    writeFileSync(
      join(dir, "options.json"),
      JSON.stringify({ ha_llat: "old-full-token", file_access: "off" }),
    );
    const mock = await startMockSupervisor(18_792);
    cleanup.push(
      () => new Promise<void>((resolve) => mock.server.close(() => resolve())),
    );

    await build(dir, mock);

    const cleared = mock.optionWrites.find((options) => "ha_llat" in options);
    expect(cleared).toBeDefined();
    expect(cleared?.ha_llat).toBe("");
    expect(cleared?.file_access).toBe("off");
  });

  it("tolerates a transient Supervisor failure at startup instead of bricking pairing", async () => {
    const dir = mkdtempSync(join(tmpdir(), "ha-nova-appmode-info-"));
    cleanup.push(() => rmSync(dir, { recursive: true, force: true }));
    writeFileSync(
      join(dir, "options.json"),
      JSON.stringify({ file_access: "off" }),
    );
    const mock = await startMockSupervisor(18_792, true); // /addons/self/info fails
    cleanup.push(
      () => new Promise<void>((resolve) => mock.server.close(() => resolve())),
    );

    const runtime = await build(dir, mock);

    // Startup must complete despite the failed secure-port lookup (it is retried
    // lazily when the owner pairs), not crash before the console can start.
    expect(runtime.registryCorrupt).toBe(false);
    expect(runtime.servers.ingress).toBeDefined();
  });

  it("wires the Cloud gate into ingress routes and pairing behavior", async () => {
    const disabledDir = mkdtempSync(
      join(tmpdir(), "ha-nova-appmode-cloud-off-"),
    );
    const enabledDir = mkdtempSync(join(tmpdir(), "ha-nova-appmode-cloud-on-"));
    cleanup.push(() => rmSync(disabledDir, { recursive: true, force: true }));
    cleanup.push(() => rmSync(enabledDir, { recursive: true, force: true }));
    writeFileSync(
      join(disabledDir, "options.json"),
      JSON.stringify({ file_access: "off" }),
    );
    writeFileSync(
      join(enabledDir, "options.json"),
      JSON.stringify({ file_access: "off" }),
    );
    const mock = await startMockSupervisor(null);
    cleanup.push(
      () => new Promise<void>((resolve) => mock.server.close(() => resolve())),
    );

    const disabled = await build(disabledDir, mock, false);
    const enabled = await build(enabledDir, mock, true);
    const disabledBase = await startIngress(disabled.servers.ingress);
    const enabledBase = await startIngress(enabled.servers.ingress);

    const disabledInfo = await ingressRequest(
      disabledBase,
      "GET",
      "/cloud/v1/info",
    );
    expect(disabledInfo.status).toBe(200);
    await expect(disabledInfo.json()).resolves.toMatchObject({
      data: {
        capabilities: {
          device_user_binding: false,
          pairing_v2: false,
          functional_routes: [],
          cleanup_routes: ["device_revoke_self"],
        },
      },
    });
    expect(
      (await ingressRequest(enabledBase, "GET", "/cloud/v1/info")).status,
    ).toBe(200);

    const disabledPage = await ingressRequest(disabledBase, "GET", "/");
    const enabledPage = await ingressRequest(enabledBase, "GET", "/");
    expect(disabledPage.status).toBe(200);
    expect(enabledPage.status).toBe(200);

    const disabledAction = await generatePairingCode(
      disabledBase,
      await disabledPage.text(),
    );
    const enabledAction = await generatePairingCode(
      enabledBase,
      await enabledPage.text(),
    );
    expect(disabledAction.status).toBe(303);
    expect(disabledAction.headers.get("location")).toContain("?err=1");
    expect(enabledAction.status).toBe(303);
    expect(enabledAction.headers.get("location")).not.toContain("?err=1");
  });
});

async function startIngress(server: Server): Promise<string> {
  server.prependListener("request", (request) => {
    Object.defineProperty(request.socket, "remoteAddress", {
      configurable: true,
      value: "172.30.32.2",
    });
  });
  const port = await new Promise<number>((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      resolve(typeof address === "object" && address ? address.port : 0);
    });
  });
  return `http://127.0.0.1:${port}`;
}

async function ingressRequest(
  base: string,
  method: "GET" | "POST",
  path: string,
  body?: URLSearchParams,
): Promise<Response> {
  return await fetch(`${base}${path}`, {
    method,
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": "owner-user",
      ...(body ? { "content-type": "application/x-www-form-urlencoded" } : {}),
    },
    ...(body ? { body } : {}),
    redirect: "manual",
  });
}

async function generatePairingCode(
  base: string,
  page: string,
): Promise<Response> {
  const match = page.match(
    /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="generate_code"/,
  );
  if (!match?.[1]) {
    throw new Error("Pairing CSRF token is missing from the owner page");
  }
  return await ingressRequest(
    base,
    "POST",
    "/action",
    new URLSearchParams({ csrf: match[1], action: "generate_code" }),
  );
}
