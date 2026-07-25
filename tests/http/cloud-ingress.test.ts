import { createServer, type Server } from "node:http";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { SupervisorClient } from "../../nova/src/ha/supervisor-client.js";
import {
  createIngressListener,
  type IngressListenerDeps,
} from "../../nova/src/runtime/ingress-listener.js";
import {
  digestSecret,
  generateCredential,
} from "../../nova/src/security/device-credential.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import {
  createPairingV1Manager,
  type PairingV1Manager,
} from "../../nova/src/security/pairing-v1.js";
import { RELAY_ID, RELAY_VERSION } from "./cloud-ingress-fixture.js";

let dir: string;
let registry: DeviceRegistry;
let pairing: PairingV1Manager;
let clock: number;
let servers: Server[];

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-ingress-"));
  registry = openDeviceRegistry(dir);
  clock = 1_000_000;
  pairing = createPairingV1Manager({
    registry,
    secureEndpoint: () => ({ spkiPin: "local-pin", securePort: 8792 }),
    cloudPairing: true,
    now: () => clock,
    generateCodeNumber: () => 473921,
  });
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

describe("Cloud ingress listener", () => {
  it("serves identity only through genuine ingress and versions success and error responses", async () => {
    const direct = await start(false);
    const spoofed = await request(direct, "GET", "/cloud/v1/info", "user-1");
    expect(spoofed.status).toBe(403);
    expect(spoofed.headers.get("x-ha-nova-relay-version")).toBe(RELAY_VERSION);

    const ingress = await start(true);
    const missingUser = await fetch(`${ingress}/cloud/v1/info`, {
      headers: { "x-ingress-path": "/api/hassio_ingress/session" },
    });
    expect(missingUser.status).toBe(403);

    const info = await request(ingress, "GET", "/cloud/v1/info", "user-1");
    expect(info.status).toBe(200);
    expect(info.headers.get("cache-control")).toBe("no-store");
    expect(info.headers.get("x-ha-nova-relay-version")).toBe(RELAY_VERSION);
    await expect(info.json()).resolves.toEqual({
      ok: true,
      data: {
        protocol_version: "v1",
        relay_instance_id: RELAY_ID,
        relay_version: RELAY_VERSION,
        capabilities: {
          device_user_binding: true,
          pairing_v2: true,
          functional_routes: ["health", "ws", "core", "files", "backups"],
          cleanup_routes: ["device_revoke_self"],
        },
      },
    });
  });

  it("applies an explicit fail-closed policy to every registered ingress route", async () => {
    const iconPath = join(dir, "icon.png");
    writeFileSync(iconPath, Buffer.from([0x89, 0x50, 0x4e, 0x47]));

    const direct = await start(false, iconPath);
    expect((await request(direct, "GET", "/icon", "user-1")).status).toBe(403);

    const ingress = await start(true, iconPath);
    const icon = await request(ingress, "GET", "/icon", "user-1");
    expect(icon.status).toBe(200);
    expect(Buffer.from(await icon.arrayBuffer())).toEqual(
      Buffer.from([0x89, 0x50, 0x4e, 0x47]),
    );

    const unknown = await request(
      ingress,
      "POST",
      "/future-machine-route",
      "user-1",
      undefined,
      {},
    );
    expect(unknown.status).toBe(404);
    await expect(unknown.json()).resolves.toMatchObject({
      error: { code: "NOT_FOUND" },
    });
  });

  it("binds an existing active device once, rejects mismatch/rebind/legacy, and gates every functional route", async () => {
    const base = await start(true);
    const device = activeDevice();

    expect(
      (await request(base, "GET", "/health", "user-1", device.credential))
        .status,
    ).toBe(401);

    const mismatch = await request(
      base,
      "POST",
      "/cloud/v1/device/bind",
      "user-1",
      device.credential,
      { relay_instance_id: "hanova-relay-v1.BBBBBBBBBBBBBBBBBBBBBB" },
    );
    expect(mismatch.status).toBe(401);
    const mismatchBody = await mismatch.json();
    expect(registry.list()[0]?.cloudUserId).toBeUndefined();

    const first = await request(
      base,
      "POST",
      "/cloud/v1/device/bind",
      "user-1",
      device.credential,
      { relay_instance_id: RELAY_ID },
    );
    expect(first.status).toBe(200);
    await expect(first.json()).resolves.toMatchObject({
      data: { bound: true, changed: true },
    });
    expect(registry.list()[0]).toMatchObject({
      cloudUserId: "user-1",
      cloudRelayInstanceId: RELAY_ID,
    });

    const retry = await request(
      base,
      "POST",
      "/cloud/v1/device/bind",
      "user-1",
      device.credential,
      { relay_instance_id: RELAY_ID },
    );
    await expect(retry.json()).resolves.toMatchObject({
      data: { bound: true, changed: false },
    });

    for (const [method, path] of [
      ["GET", "/health"],
      ["POST", "/ws"],
      ["POST", "/core"],
      ["POST", "/files"],
      ["POST", "/backups"],
    ] as const) {
      expect(
        (
          await request(
            base,
            method,
            path,
            "user-1",
            device.credential,
            method === "POST" ? {} : undefined,
          )
        ).status,
      ).toBe(200);
      expect(
        (
          await request(
            base,
            method,
            path,
            "user-2",
            device.credential,
            method === "POST" ? {} : undefined,
          )
        ).status,
      ).toBe(401);
    }

    const conflict = await request(
      base,
      "POST",
      "/cloud/v1/device/bind",
      "user-2",
      device.credential,
      { relay_instance_id: RELAY_ID },
    );
    expect(conflict.status).toBe(401);
    expect(await conflict.json()).toEqual(mismatchBody);

    registry.importLegacy(digestSecret("legacy-token"), clock);
    expect(
      (await request(base, "GET", "/health", "user-1", "legacy-token")).status,
    ).toBe(401);
  });
});

function activeDevice() {
  const credential = generateCredential();
  registry.createPending(
    {
      deviceId: credential.deviceId,
      secretDigest: credential.secretDigest,
      clientInstallId: "existing-install",
      name: "MacBook",
      platform: "darwin",
      client: "codex",
      createdAtMs: clock,
    },
    clock,
  );
  registry.activate(credential.deviceId, clock);
  return credential;
}

async function start(
  asSupervisorIngress: boolean,
  iconPath?: string,
): Promise<string> {
  const listener = createIngressListener({
    ...deps(),
    ...(iconPath === undefined ? {} : { iconPath }),
  });
  const server = createServer((request, response) => {
    if (asSupervisorIngress) {
      Object.defineProperty(request.socket, "remoteAddress", {
        configurable: true,
        value: "172.30.32.2",
      });
    }
    listener(request, response);
  });
  servers.push(server);
  const port = await new Promise<number>((resolve) => {
    server.listen(0, "127.0.0.1", () =>
      resolve((server.address() as { port: number }).port),
    );
  });
  return `http://127.0.0.1:${port}`;
}

function deps(): IngressListenerDeps {
  const functional = {
    health: () => ({ route: "health" }),
    ws: () => ({ route: "ws" }),
    core: () => ({ route: "core" }),
    files: () => ({ route: "files" }),
    backups: () => ({ route: "backups" }),
  };
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
    functional,
    relayInstanceId: RELAY_ID,
    relayVersion: RELAY_VERSION,
    cloudRemoteEnabled: true,
    wsClient,
    supervisor,
    registryCorrupt: () => false,
    resetRegistry: () => {},
    now: () => clock,
    logger: { warn: () => {}, error: () => {} },
  };
}

async function request(
  base: string,
  method: "GET" | "POST",
  path: string,
  userId: string,
  credential?: string,
  body?: unknown,
): Promise<Response> {
  return await fetch(`${base}${path}`, {
    method,
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": userId,
      ...(credential ? { authorization: `Bearer ${credential}` } : {}),
      ...(body !== undefined ? { "content-type": "application/json" } : {}),
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
}
