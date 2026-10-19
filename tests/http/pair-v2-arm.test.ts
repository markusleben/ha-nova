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
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
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

// POST /pair/v2/arm over the ingress listener
// (docs/work/2026-10-16-ha-sign-in-pairing-spec.md).

const OWNER = { id: "owner-1", name: "Owner", is_owner: true, is_active: true };
const ADMIN = { id: "admin-1", name: "Admin", is_owner: false, is_active: true };

let dir: string;
let registry: DeviceRegistry;
let pairing: PairingV1Manager;
let users: () => Promise<HaAuthUser[]>;
let peer: string;
let corrupt: boolean;
let servers: Server[];

beforeAll(async () => {
  await opaqueReady();
});

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-pair-v2-arm-"));
  registry = openDeviceRegistry(dir);
  pairing = createPairingV1Manager({
    registry,
    secureEndpoint: () => ({ spkiPin: "local-pin", securePort: 8792 }),
    cloudPairing: true,
    now: () => 1_000_000,
  });
  users = async () => [OWNER, ADMIN];
  peer = "172.30.32.2";
  corrupt = false;
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

describe("POST /pair/v2/arm", () => {
  it("arms a no-store secret for the owner that pairs a local device through ingress", async () => {
    const base = await start();
    const armed = await arm(base, "owner-1");
    expect(armed.status).toBe(200);
    expect(armed.headers.get("cache-control")).toBe("no-store");
    const { data } = (await armed.json()) as {
      data: {
        pairing_secret: string;
        expires_in_s: number;
        relay_instance_id: string;
      };
    };
    expect(data.pairing_secret).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(data.expires_in_s).toBe(120);
    expect(data.relay_instance_id).toBe(RELAY_ID);

    const page = await (await call(base, "GET", "/", "owner-1")).text();
    expect(page).not.toContain(data.pairing_secret);
    expect(page).toContain("connecting through Home Assistant sign-in");

    const provisioned = await pairThroughIngress(base, data.pairing_secret);
    expect(provisioned).toMatchObject({
      relay_instance_id: RELAY_ID,
      spki_pin: "local-pin",
      secure_port: 8792,
    });
    const [pending] = registry.list();
    expect(pending?.state).toBe("pending");
    expect(pending?.cloudUserId).toBeUndefined();
  });

  it("refuses everyone but the Home Assistant owner", async () => {
    const base = await start();
    await expectError(arm(base, "admin-1"), 403, "OWNER_REQUIRED");
    await expectError(arm(base, "unknown"), 403, "OWNER_REQUIRED");
    users = async () => [{ ...OWNER, is_active: false }];
    await expectError(arm(base, "owner-1"), 403, "OWNER_REQUIRED");
    users = async () => [{ ...OWNER, system_generated: true }];
    await expectError(arm(base, "owner-1"), 403, "OWNER_REQUIRED");
    users = async () => {
      throw new Error("Supervisor unavailable");
    };
    await expectError(arm(base, "owner-1"), 503, "OWNER_CHECK_UNAVAILABLE");
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("is machine-only, JSON-only, and ingress-only", async () => {
    const base = await start();
    await expectError(
      arm(base, "owner-1", { "sec-fetch-site": "same-origin" }),
      403,
      "MACHINE_REQUEST_REQUIRED",
    );
    await expectError(
      arm(base, "owner-1", { "content-type": "text/plain" }),
      415,
      "UNSUPPORTED_MEDIA_TYPE",
    );
    await expectError(
      arm(base, "owner-1", { "x-remote-user-id": "" }),
      403,
      "INGRESS_REQUIRED",
    );
    const duplicate = await fetch(`${base}/pair/v2/arm`, {
      method: "POST",
      headers: [
        ["x-ingress-path", "/api/hassio_ingress/session"],
        ["x-remote-user-id", "owner-1"],
        ["x-remote-user-id", "owner-1"],
        ["content-type", "application/json"],
      ],
      body: "{}",
    });
    expect(duplicate.status).toBe(403);
    peer = "172.30.32.9";
    await expectError(arm(base, "owner-1"), 403, "INGRESS_REQUIRED");
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("reports an unmapped secure port instead of arming a dead slot", async () => {
    pairing = createPairingV1Manager({
      registry,
      secureEndpoint: () => null,
      cloudPairing: true,
      now: () => 1_000_000,
    });
    const base = await start();
    await expectError(arm(base, "owner-1"), 503, "PAIRING_UNAVAILABLE");
  });

  it("arms nothing while the device registry is corrupt", async () => {
    corrupt = true;
    const base = await start();
    await expectError(arm(base, "owner-1"), 503, "PAIRING_UNAVAILABLE");
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("pairs a local device without Cloud remote", async () => {
    pairing = createPairingV1Manager({
      registry,
      secureEndpoint: () => ({ spkiPin: "local-pin", securePort: 8792 }),
      now: () => 1_000_000,
    });
    const base = await start(false);
    const armed = await arm(base, "owner-1");
    expect(armed.status).toBe(200);
    const { data } = (await armed.json()) as { data: { pairing_secret: string } };
    expect(await pairThroughIngress(base, data.pairing_secret)).toMatchObject({
      spki_pin: "local-pin",
      secure_port: 8792,
    });
    expect(registry.list()[0]?.cloudUserId).toBeUndefined();
  });
});

async function pairThroughIngress(
  base: string,
  secret: string,
): Promise<Record<string, unknown>> {
  const login = opaque.client.startLogin({ password: secret });
  const started = await call(base, "POST", "/pair/v2/start", "owner-1", {
    ke1: login.startLoginRequest,
  });
  expect(started.status).toBe(200);
  const { data } = (await started.json()) as {
    data: { handshake_id: string; ke2: string };
  };
  const finishedClient = opaque.client.finishLogin({
    clientLoginState: login.clientLoginState,
    loginResponse: data.ke2,
    password: secret,
    identifiers: IDENTIFIERS,
    keyStretching: KSF,
  })!;
  const handshake = Buffer.from(data.handshake_id, "base64url");
  const keys = deriveDirectionKeys(
    Buffer.from(finishedClient.sessionKey, "base64url"),
    handshake,
  );
  const finished = await call(base, "POST", "/pair/v2/finish", "owner-1", {
    handshake_id: data.handshake_id,
    ke3: finishedClient.finishLoginRequest,
    metadata: seal(
      keys.c2s,
      handshake,
      "c2s",
      Buffer.from(JSON.stringify(META)),
    ).toString("base64url"),
  });
  expect(finished.status).toBe(200);
  const body = (await finished.json()) as { data: { response: string } };
  const plaintext = open(
    keys.s2c,
    handshake,
    "s2c",
    Buffer.from(body.data.response, "base64url"),
  );
  return JSON.parse(plaintext!.toString("utf8")) as Record<string, unknown>;
}

async function expectError(
  pending: Promise<Response>,
  status: number,
  code: string,
): Promise<void> {
  const response = await pending;
  expect(response.status).toBe(status);
  expect(((await response.json()) as { error: { code: string } }).error.code).toBe(
    code,
  );
}

function arm(
  base: string,
  userId: string,
  headers: Record<string, string> = {},
): Promise<Response> {
  return fetch(`${base}/pair/v2/arm`, {
    method: "POST",
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": userId,
      "content-type": "application/json",
      ...headers,
    },
    body: "{}",
  });
}

function call(
  base: string,
  method: "GET" | "POST",
  path: string,
  userId: string,
  body?: unknown,
): Promise<Response> {
  return fetch(`${base}${path}`, {
    method,
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": userId,
      ...(body !== undefined ? { "content-type": "application/json" } : {}),
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
}

async function start(cloudRemoteEnabled = true): Promise<string> {
  const listener = createIngressListener(deps(cloudRemoteEnabled));
  const server = createServer((request, response) => {
    Object.defineProperty(request.socket, "remoteAddress", {
      configurable: true,
      value: peer,
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

function deps(cloudRemoteEnabled: boolean): IngressListenerDeps {
  const supervisor: SupervisorClient = {
    getSelfInfo: async () => ({
      version: RELAY_VERSION,
      versionLatest: RELAY_VERSION,
      updateAvailable: false,
      ingressPanel: true,
      hostname: "",
      network: {},
    }),
    getMappedHostPort: async () => null,
    setOptions: async () => {},
    setIngressPanel: async () => {},
    sendDiscovery: async () => "",
    deleteDiscovery: async () => {},
  };
  const wsClient = {
    sendMessage: async () => await users(),
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
    registryCorrupt: () => corrupt,
    resetRegistry: () => {},
    now: () => 1_000_000,
    logger: { warn: () => {}, error: () => {} },
  };
}
