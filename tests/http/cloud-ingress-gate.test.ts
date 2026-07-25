import { createServer, type Server } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import type { SupervisorClient } from "../../nova/src/ha/supervisor-client.js";
import {
  createIngressListener,
  type IngressListenerDeps,
} from "../../nova/src/runtime/ingress-listener.js";
import type { DeviceRegistry } from "../../nova/src/security/device-registry.js";
import type { PairingV1Manager } from "../../nova/src/security/pairing-v1.js";

const RELAY_ID = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
const RELAY_VERSION = "0.8.0";
const MACHINE_ROUTES = [
  {
    method: "GET",
    path: "/cloud/v1/info",
    disabledStatus: 200,
    enabledStatus: 200,
  },
  {
    method: "POST",
    path: "/cloud/v1/device/revoke-self",
    disabledStatus: 400,
    enabledStatus: 400,
  },
  {
    method: "POST",
    path: "/cloud/v1/device/bind",
    disabledStatus: 404,
    enabledStatus: 400,
  },
  {
    method: "POST",
    path: "/cloud/v1/device/activate",
    disabledStatus: 404,
    enabledStatus: 400,
  },
  {
    method: "GET",
    path: "/pair/v2/info",
    disabledStatus: 404,
    enabledStatus: 200,
  },
  {
    method: "POST",
    path: "/pair/v2/start",
    disabledStatus: 404,
    enabledStatus: 400,
  },
  {
    method: "POST",
    path: "/pair/v2/finish",
    disabledStatus: 404,
    enabledStatus: 400,
  },
  { method: "GET", path: "/health", disabledStatus: 404, enabledStatus: 401 },
  { method: "POST", path: "/ws", disabledStatus: 404, enabledStatus: 401 },
  { method: "POST", path: "/core", disabledStatus: 404, enabledStatus: 401 },
  { method: "POST", path: "/files", disabledStatus: 404, enabledStatus: 401 },
  { method: "POST", path: "/backups", disabledStatus: 404, enabledStatus: 401 },
] as const;

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(
    servers
      .splice(0)
      .map(
        (server) =>
          new Promise<void>((resolve) => server.close(() => resolve())),
      ),
  );
});

describe("Cloud ingress release gate", () => {
  it("keeps cleanup routes while gating setup and functional routes", async () => {
    const disabled = await start(false);
    const enabled = await start(true);

    expect((await request(disabled, "GET", "/")).status).toBe(200);
    expect((await request(enabled, "GET", "/")).status).toBe(200);

    for (const route of MACHINE_ROUTES) {
      const disabledResponse = await request(
        disabled,
        route.method,
        route.path,
      );
      expect(
        disabledResponse.status,
        `${route.method} ${route.path} disabled`,
      ).toBe(route.disabledStatus);

      const enabledResponse = await request(enabled, route.method, route.path);
      expect(
        enabledResponse.status,
        `${route.method} ${route.path} enabled`,
      ).toBe(route.enabledStatus);
    }

    const disabledInfo = await request(disabled, "GET", "/cloud/v1/info");
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
  });
});

async function start(cloudRemoteEnabled: boolean): Promise<string> {
  const listener = createIngressListener(deps(cloudRemoteEnabled));
  const server = createServer((request, response) => {
    Object.defineProperty(request.socket, "remoteAddress", {
      configurable: true,
      value: "172.30.32.2",
    });
    listener(request, response);
  });
  servers.push(server);
  const port = await new Promise<number>((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      resolve((server.address() as { port: number }).port);
    });
  });
  return `http://127.0.0.1:${port}`;
}

function deps(cloudRemoteEnabled: boolean): IngressListenerDeps {
  const registry = {
    list: () => [],
    hasLegacy: () => false,
  } as unknown as DeviceRegistry;
  const pairing = {
    getStatus: () => ({ phase: "inactive" }),
  } as unknown as PairingV1Manager;
  const supervisor: SupervisorClient = {
    getSelfInfo: async () => ({
      version: RELAY_VERSION,
      versionLatest: RELAY_VERSION,
      updateAvailable: false,
      ingressPanel: true,
      network: {},
    }),
    getMappedHostPort: async () => null,
    setOptions: async () => {},
    setIngressPanel: async () => {},
  };
  const wsClient: IngressListenerDeps["wsClient"] = {
    sendMessage: async <T>() =>
      [
        {
          id: "owner-user",
          name: "Owner",
          is_owner: true,
          is_active: true,
          system_generated: false,
        },
      ] as T,
    collectMessageEvents: async <T>() => ({
      events: [] as T[],
      truncated: false,
    }),
    isConnected: () => true,
    getConnectionStatus: () => ({ connected: true, disconnect_reason: null }),
  };

  return {
    registry,
    pairing,
    functional: {
      health: () => ({ route: "health" }),
      ws: () => ({ route: "ws" }),
      core: () => ({ route: "core" }),
      files: () => ({ route: "files" }),
      backups: () => ({ route: "backups" }),
    },
    relayInstanceId: RELAY_ID,
    relayVersion: RELAY_VERSION,
    cloudRemoteEnabled,
    wsClient,
    supervisor,
    registryCorrupt: () => false,
    resetRegistry: () => {},
    now: () => 1_000,
    logger: { warn: () => {}, error: () => {} },
  };
}

async function request(
  base: string,
  method: "GET" | "POST",
  path: string,
): Promise<Response> {
  return await fetch(`${base}${path}`, {
    method,
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": "owner-user",
      ...(method === "POST" ? { "content-type": "application/json" } : {}),
    },
    ...(method === "POST" ? { body: "{}" } : {}),
  });
}
