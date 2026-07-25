import { createServer, type Server } from "node:http";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { SupervisorClient } from "../../nova/src/ha/supervisor-client.js";
import {
  createIngressListener,
  type IngressListenerDeps,
} from "../../nova/src/runtime/ingress-listener.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import type { PairingV1Manager } from "../../nova/src/security/pairing-v1.js";
import { RELAY_ID, RELAY_VERSION } from "./cloud-ingress-fixture.js";

const NOW = 1_000_000;

let dir: string;
let registry: DeviceRegistry;
let servers: Server[];

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-revoke-disabled-"));
  registry = openDeviceRegistry(dir);
  servers = [];
});

afterEach(async () => {
  await Promise.all(
    servers.map(
      (server) => new Promise<void>((resolve) => server.close(() => resolve())),
    ),
  );
  rmSync(dir, { recursive: true, force: true });
});

describe("Cloud ingress revocation while remote access is disabled", () => {
  it("keeps verified self-revocation available while setup and runtime routes are disabled", async () => {
    const credential = generateCredential();
    registry.createPending(
      {
        deviceId: credential.deviceId,
        secretDigest: credential.secretDigest,
        clientInstallId: "remote-install",
        name: "MacBook",
        platform: "darwin",
        client: "codex",
        createdAtMs: NOW,
      },
      NOW,
    );
    registry.activate(credential.deviceId, NOW);
    const bound = registry.bindCloudUser(
      credential.deviceId,
      credential.secretDigest,
      "user-1",
      RELAY_ID,
    );
    expect(bound.ok).toBe(true);

    const base = await start();
    expect(
      (
        await post(
          base,
          "/cloud/v1/device/bind",
          "user-1",
          credential.credential,
        )
      ).status,
    ).toBe(404);
    const health = await fetch(`${base}/health`, {
      headers: ingressHeaders("user-1", credential.credential),
    });
    expect(health.status).toBe(404);

    const revoked = await post(
      base,
      "/cloud/v1/device/revoke-self",
      "user-1",
      credential.credential,
    );
    expect(revoked.status).toBe(200);
    await expect(revoked.json()).resolves.toMatchObject({
      data: { revoked: true, changed: true },
    });
    expect(registry.list()).toEqual([]);
  });
});

async function start(): Promise<string> {
  const listener = createIngressListener(deps());
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

function deps(): IngressListenerDeps {
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
  const wsClient = {
    sendMessage: async () => [],
    collectMessageEvents: async () => ({ events: [], truncated: false }),
    isConnected: () => true,
    getConnectionStatus: () => ({ connected: true, disconnectReason: null }),
  } as unknown as IngressListenerDeps["wsClient"];
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
    cloudRemoteEnabled: false,
    wsClient,
    supervisor,
    registryCorrupt: () => false,
    resetRegistry: () => {},
    now: () => NOW,
    logger: { warn: () => {}, error: () => {} },
  };
}

function ingressHeaders(
  userId: string,
  credential: string,
): Record<string, string> {
  return {
    "x-ingress-path": "/api/hassio_ingress/session",
    "x-remote-user-id": userId,
    authorization: `Bearer ${credential}`,
  };
}

async function post(
  base: string,
  path: string,
  userId: string,
  credential: string,
): Promise<Response> {
  return await fetch(`${base}${path}`, {
    method: "POST",
    headers: {
      ...ingressHeaders(userId, credential),
      "content-type": "application/json",
    },
    body: JSON.stringify({ relay_instance_id: RELAY_ID }),
  });
}
