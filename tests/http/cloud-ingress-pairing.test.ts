import { createServer, type Server } from "node:http";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import type { SupervisorClient } from "../../nova/src/ha/supervisor-client.js";
import {
  createIngressListener,
  type IngressListenerDeps,
} from "../../nova/src/runtime/ingress-listener.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import { opaqueReady } from "../../nova/src/security/opaque-server.js";
import {
  deriveDirectionKeys,
  open,
  seal,
} from "../../nova/src/security/pairing-crypto.js";
import {
  createPairingV1Manager,
  type PairingV1Manager,
} from "../../nova/src/security/pairing-v1.js";
import {
  IDENTIFIERS,
  KSF,
  META,
  RELAY_ID,
  RELAY_VERSION,
} from "./cloud-ingress-fixture.js";

let dir: string;
let registry: DeviceRegistry;
let pairing: PairingV1Manager;
let clock: number;
let servers: Server[];

beforeAll(async () => {
  await opaqueReady();
});

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-cloud-ingress-pairing-"));
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

describe("Cloud ingress pairing", () => {
  it("pairs v2 wholly through ingress, binds on activation, and never returns local TLS material", async () => {
    const base = await start();
    const { code } = pairing.generateCode();
    const client = opaque.client.startLogin({ password: code });

    const startedResponse = await request(
      base,
      "POST",
      "/pair/v2/start",
      "user-1",
      undefined,
      { ke1: client.startLoginRequest },
    );
    expect(startedResponse.status).toBe(200);
    const started = (await startedResponse.json()) as {
      data: { handshake_id: string; ke2: string };
    };
    const finishedClient = opaque.client.finishLogin({
      clientLoginState: client.clientLoginState,
      loginResponse: started.data.ke2,
      password: code,
      identifiers: IDENTIFIERS,
      keyStretching: KSF,
    })!;
    const handshakeBytes = Buffer.from(started.data.handshake_id, "base64url");
    const keys = deriveDirectionKeys(
      Buffer.from(finishedClient.sessionKey, "base64url"),
      handshakeBytes,
    );
    const metadata = seal(
      keys.c2s,
      handshakeBytes,
      "c2s",
      Buffer.from(JSON.stringify(META)),
    ).toString("base64url");
    const finishBody = {
      handshake_id: started.data.handshake_id,
      ke3: finishedClient.finishLoginRequest,
      metadata,
    };

    expect(
      (
        await request(
          base,
          "POST",
          "/pair/v2/finish",
          "user-2",
          undefined,
          finishBody,
        )
      ).status,
    ).toBe(401);
    const finishedResponse = await request(
      base,
      "POST",
      "/pair/v2/finish",
      "user-1",
      undefined,
      finishBody,
    );
    expect(finishedResponse.status).toBe(200);
    const finished = (await finishedResponse.json()) as {
      data: { response: string };
    };
    const plaintext = open(
      keys.s2c,
      handshakeBytes,
      "s2c",
      Buffer.from(finished.data.response, "base64url"),
    );
    expect(plaintext).not.toBeNull();
    const provisioned = JSON.parse(plaintext!.toString("utf8")) as Record<
      string,
      string
    >;
    expect(Object.keys(provisioned).sort()).toEqual([
      "credential",
      "device_id",
      "relay_instance_id",
    ]);
    expect(provisioned.relay_instance_id).toBe(RELAY_ID);
    expect(registry.list()[0]).toMatchObject({
      state: "pending",
      cloudUserId: "user-1",
      cloudRelayInstanceId: RELAY_ID,
    });
    const pending = registry.list()[0]!;
    expect(
      registry.activatePending(pending.deviceId, pending.secretDigest, clock),
    ).toBeNull();

    const staleActivation = await request(
      base,
      "POST",
      "/cloud/v1/device/activate",
      "user-1",
      provisioned.credential,
      { relay_instance_id: "hanova-relay-v1.BBBBBBBBBBBBBBBBBBBBBB" },
    );
    expect(staleActivation.status).toBe(401);
    const staleActivationBody = await staleActivation.json();
    expect(registry.list()[0]?.state).toBe("pending");

    const activated = await request(
      base,
      "POST",
      "/cloud/v1/device/activate",
      "user-1",
      provisioned.credential,
      { relay_instance_id: RELAY_ID },
    );
    expect(activated.status).toBe(200);
    expect(registry.list()[0]).toMatchObject({
      state: "active",
      cloudUserId: "user-1",
      cloudRelayInstanceId: RELAY_ID,
    });
    expect(
      (await request(base, "GET", "/health", "user-1", provisioned.credential))
        .status,
    ).toBe(200);
    expect(
      (
        await request(
          base,
          "POST",
          "/cloud/v1/device/activate",
          "user-1",
          provisioned.credential,
          { relay_instance_id: RELAY_ID },
        )
      ).status,
    ).toBe(200);
    const wrongUserActivation = await request(
      base,
      "POST",
      "/cloud/v1/device/activate",
      "user-2",
      provisioned.credential,
      { relay_instance_id: RELAY_ID },
    );
    expect(wrongUserActivation.status).toBe(401);
    expect(await wrongUserActivation.json()).toEqual(staleActivationBody);
    expect(
      (
        await request(
          base,
          "POST",
          "/pair/v2/finish",
          "user-2",
          undefined,
          finishBody,
        )
      ).status,
    ).toBe(401);
  });

  it("reuses the JSON body cap and pairing rate limiter", async () => {
    const base = await start();
    pairing.generateCode();
    for (let attempt = 0; attempt < 5; attempt += 1) {
      expect(
        (
          await request(base, "POST", "/pair/v2/start", "user-1", undefined, {
            ke1: "AAAA",
          })
        ).status,
      ).toBe(400);
    }
    const limited = await request(
      base,
      "POST",
      "/pair/v2/start",
      "user-1",
      undefined,
      { ke1: "AAAA" },
    );
    expect(limited.status).toBe(429);
    expect(Number(limited.headers.get("retry-after"))).toBeGreaterThan(0);

    const oversized = await request(
      base,
      "POST",
      "/pair/v2/start",
      "user-2",
      undefined,
      { ke1: "A".repeat(1_048_576) },
    );
    expect(oversized.status).toBe(413);
    expect(oversized.headers.get("x-ha-nova-relay-version")).toBe(
      RELAY_VERSION,
    );
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
    server.listen(0, "127.0.0.1", () =>
      resolve((server.address() as { port: number }).port),
    );
  });
  return `http://127.0.0.1:${port}`;
}

function deps(): IngressListenerDeps {
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
