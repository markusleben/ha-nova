import {
  createServer,
  request as sendHttpRequest,
  type Server,
} from "node:http";
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

const OTHER_RELAY_ID = "hanova-relay-v1.BBBBBBBBBBBBBBBBBBBBBB";
const NOW = 1_000_000;

let dir: string;
let registry: DeviceRegistry;
let servers: Server[];

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-revoke-"));
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

describe("Cloud ingress device revocation", () => {
  it("binds revocation to the exact user, Relay, and bearer, then accepts a restart replay", async () => {
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

    const base = await start();
    expect(
      (
        await post(
          base,
          "/cloud/v1/device/bind",
          "user-1",
          credential.credential,
          RELAY_ID,
        )
      ).status,
    ).toBe(200);

    const wrongUser = await post(
      base,
      "/cloud/v1/device/revoke-self",
      "user-2",
      credential.credential,
      RELAY_ID,
    );
    expect(wrongUser.status).toBe(401);
    const genericUnauthorized = await wrongUser.json();
    expect(genericUnauthorized).toEqual({
      ok: false,
      error: {
        code: "UNAUTHORIZED",
        message: "Unknown or inactive device credential",
      },
    });
    for (const [name, authorizationHeaders] of [
      ["missing", []],
      ["malformed", ["Basic malformed"]],
      [
        "duplicate",
        [`Bearer ${credential.credential}`, `Bearer ${credential.credential}`],
      ],
    ] as const) {
      const rejected = await rawRequest(
        base,
        "POST",
        "/cloud/v1/device/revoke-self",
        "user-1",
        [...authorizationHeaders],
        { relay_instance_id: RELAY_ID },
      ).catch((error: unknown) => {
        throw new Error(`${name} Authorization request failed`, {
          cause: error,
        });
      });
      expect(rejected.status).toBe(401);
      expect(rejected.body).toEqual(genericUnauthorized);
    }

    const wrongRelay = await post(
      base,
      "/cloud/v1/device/revoke-self",
      "user-1",
      credential.credential,
      OTHER_RELAY_ID,
    );
    expect(wrongRelay.status).toBe(401);
    expect(await wrongRelay.json()).toEqual(genericUnauthorized);

    const wrongSecret = replaceSecret(
      credential.credential,
      generateCredential().credential,
    );
    const wrongBearer = await post(
      base,
      "/cloud/v1/device/revoke-self",
      "user-1",
      wrongSecret,
      RELAY_ID,
    );
    expect(wrongBearer.status).toBe(401);
    expect(await wrongBearer.json()).toEqual(genericUnauthorized);
    expect(registry.list()).toHaveLength(1);

    const functionalMissing = await rawRequest(
      base,
      "GET",
      "/health",
      "user-1",
      [],
      undefined,
    );
    expect(functionalMissing.status).toBe(401);
    expect(functionalMissing.body).toEqual(genericUnauthorized);
    const functionalWrongUser = await rawRequest(
      base,
      "GET",
      "/health",
      "user-2",
      [`Bearer ${credential.credential}`],
      undefined,
    );
    expect(functionalWrongUser.status).toBe(401);
    expect(functionalWrongUser.body).toEqual(genericUnauthorized);

    const revoked = await post(
      base,
      "/cloud/v1/device/revoke-self",
      "user-1",
      credential.credential,
      RELAY_ID,
    );
    expect(revoked.status).toBe(200);
    await expect(revoked.json()).resolves.toMatchObject({
      data: {
        device_id: credential.deviceId,
        revoked: true,
        changed: true,
      },
    });
    expect(registry.list()).toEqual([]);
    const functionalRevoked = await rawRequest(
      base,
      "GET",
      "/health",
      "user-1",
      [`Bearer ${credential.credential}`],
      undefined,
    );
    expect(functionalRevoked.status).toBe(401);
    expect(functionalRevoked.body).toEqual(genericUnauthorized);

    registry = openDeviceRegistry(dir);
    const restarted = await start();
    const replay = await post(
      restarted,
      "/cloud/v1/device/revoke-self",
      "user-1",
      credential.credential,
      RELAY_ID,
    );
    expect(replay.status).toBe(200);
    await expect(replay.json()).resolves.toMatchObject({
      data: {
        device_id: credential.deviceId,
        revoked: true,
        changed: false,
      },
    });

    const replayWrongUser = await post(
      restarted,
      "/cloud/v1/device/revoke-self",
      "user-2",
      credential.credential,
      RELAY_ID,
    );
    expect(replayWrongUser.status).toBe(401);
    expect(await replayWrongUser.json()).toEqual(genericUnauthorized);
  });
});

async function start(cloudRemoteEnabled = true): Promise<string> {
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
    cloudRemoteEnabled,
    wsClient,
    supervisor,
    registryCorrupt: () => false,
    resetRegistry: () => {},
    now: () => NOW,
    logger: { warn: () => {}, error: () => {} },
  };
}

async function post(
  base: string,
  path: string,
  userId: string,
  credential: string,
  relayInstanceId: string,
): Promise<Response> {
  return await fetch(`${base}${path}`, {
    method: "POST",
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": userId,
      authorization: `Bearer ${credential}`,
      "content-type": "application/json",
    },
    body: JSON.stringify({ relay_instance_id: relayInstanceId }),
  });
}

function replaceSecret(credential: string, replacement: string): string {
  const sourceParts = credential.split(".");
  const replacementParts = replacement.split(".");
  return `${sourceParts[0]}.${sourceParts[1]}.${replacementParts[2]}`;
}

async function rawRequest(
  base: string,
  method: string,
  path: string,
  userId: string,
  authorizationHeaders: string[],
  body: unknown,
): Promise<{ status: number; body: unknown }> {
  const payload = body === undefined ? "" : JSON.stringify(body);
  const target = new URL(base);
  const headers = [
    "host",
    target.host,
    "x-ingress-path",
    "/api/hassio_ingress/session",
    "x-remote-user-id",
    userId,
    "accept",
    "application/json",
  ];
  for (const authorization of authorizationHeaders) {
    headers.push("authorization", authorization);
  }
  if (payload !== "") {
    headers.push(
      "content-type",
      "application/json",
      "content-length",
      String(Buffer.byteLength(payload)),
    );
  }
  return await new Promise((resolve, reject) => {
    const request = sendHttpRequest(
      `${base}${path}`,
      {
        method,
        headers,
      },
      (response) => {
        const chunks: Buffer[] = [];
        response.on("data", (chunk: Buffer) => chunks.push(chunk));
        response.on("end", () => {
          const raw = Buffer.concat(chunks).toString("utf8");
          try {
            resolve({
              status: response.statusCode ?? 0,
              body: JSON.parse(raw) as unknown,
            });
          } catch (error) {
            reject(
              new Error(
                `invalid JSON response (HTTP ${response.statusCode ?? 0}): ${JSON.stringify(raw)}`,
                { cause: error },
              ),
            );
          }
        });
      },
    );
    request.on("error", reject);
    if (payload !== "") {
      request.end(payload);
    } else {
      request.end();
    }
  });
}
