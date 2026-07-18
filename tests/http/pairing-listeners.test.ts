import { createServer as createHttp, type Server } from "node:http";
import { createServer as createHttps } from "node:https";
import { request as httpsRequest } from "node:https";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import type { RouteHandler } from "../../nova/src/http/router.js";
import { createBootstrapListener, createDeviceListener } from "../../nova/src/runtime/listeners.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { OPAQUE_CLIENT_ID, OPAQUE_KSF, OPAQUE_SERVER_ID, opaqueReady } from "../../nova/src/security/opaque-server.js";
import { deriveDirectionKeys, open, seal } from "../../nova/src/security/pairing-crypto.js";
import { createPairingV1Manager, type PairingV1Manager } from "../../nova/src/security/pairing-v1.js";
import { loadOrCreateTlsIdentity, type TlsIdentity } from "../../nova/src/security/tls-identity.js";

const IDENTIFIERS = { client: OPAQUE_CLIENT_ID, server: OPAQUE_SERVER_ID };
const KSF = { "argon2id-custom": OPAQUE_KSF } as const;
const META = { name: "MacBook", platform: "darwin", client: "claude", client_install_id: "install-1" };

let dir: string;
let registry: DeviceRegistry;
let manager: PairingV1Manager;
let tls: TlsIdentity;
let bootstrap: Server;
let device: Server;
let bootBase: string;
let devicePort: number;

const echo: RouteHandler = () => ({ echoed: true });
const functional = { health: echo, ws: echo, core: echo, files: echo, backups: echo };

async function listen(server: Server): Promise<number> {
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  return (server.address() as { port: number }).port;
}

// plain HTTP JSON
async function postBoot(path: string, body: unknown, auth?: string) {
  const res = await fetch(`${bootBase}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(auth ? { authorization: `Bearer ${auth}` } : {}) },
    body: JSON.stringify(body),
  });
  return { status: res.status, json: (await res.json()) as { ok: boolean; data?: Record<string, unknown> } };
}

// https to the self-signed device listener (pin already proven elsewhere; here
// we only assert routing/auth, so skip verification).
function deviceReq(path: string, auth: string): Promise<{ status: number; json: any }> {
  return new Promise((resolve, reject) => {
    const req = httpsRequest(
      { host: "127.0.0.1", port: devicePort, path, method: "POST", rejectUnauthorized: false, headers: { authorization: `Bearer ${auth}` } },
      (res) => {
        let b = "";
        res.on("data", (c) => (b += c));
        res.on("end", () => resolve({ status: res.statusCode ?? 0, json: b ? JSON.parse(b) : null }));
      }
    );
    req.on("error", reject);
    req.end();
  });
}

function pair(code: string) {
  const reg = opaque.client.startLogin({ password: code });
  return { reg };
}

beforeAll(async () => {
  await opaqueReady();
});
beforeEach(async () => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-listeners-"));
  registry = openDeviceRegistry(dir);
  tls = await loadOrCreateTlsIdentity(dir);
  device = createHttps({ key: tls.keyPem, cert: tls.certPem, minVersion: "TLSv1.3" }, () => undefined);
  devicePort = await listen(device);
  manager = createPairingV1Manager({
    registry,
    secureEndpoint: () => ({ spkiPin: tls.spkiPin, securePort: devicePort }),
    now: () => Date.now(),
  });
  const deps = { registry, pairingManager: manager, functional, relayVersion: "0.7.0", now: () => Date.now() };
  device.removeAllListeners("request");
  device.on("request", createDeviceListener(deps));
  bootstrap = createHttp(createBootstrapListener(deps));
  const bp = await listen(bootstrap);
  bootBase = `http://127.0.0.1:${bp}`;
});
afterEach(() => {
  bootstrap.close();
  device.close();
  rmSync(dir, { recursive: true, force: true });
});

async function completePairing(): Promise<{ credential: string }> {
  const { code } = manager.generateCode();
  const { reg } = pair(code);
  const started = await postBoot("/pair/v1/start", { ke1: reg.startLoginRequest });
  expect(started.status).toBe(200);
  const handshakeId = started.json.data!.handshake_id as string;
  const ke2 = started.json.data!.ke2 as string;
  const fin = opaque.client.finishLogin({ clientLoginState: reg.clientLoginState, loginResponse: ke2, password: code, identifiers: IDENTIFIERS, keyStretching: KSF })!;
  const hsId = Buffer.from(handshakeId, "base64url");
  const keys = deriveDirectionKeys(Buffer.from(fin.sessionKey, "base64url"), hsId);
  const encMeta = seal(keys.c2s, hsId, "c2s", Buffer.from(JSON.stringify(META))).toString("base64url");
  const finished = await postBoot("/pair/v1/finish", { handshake_id: handshakeId, ke3: fin.finishLoginRequest, metadata: encMeta });
  expect(finished.status).toBe(200);
  const plain = open(keys.s2c, hsId, "s2c", Buffer.from(finished.json.data!.response as string, "base64url"))!;
  return JSON.parse(plain.toString("utf8")) as { credential: string };
}

describe("pairing listeners wiring", () => {
  it("serves /pair/v1/info bearer-exempt on the bootstrap listener", async () => {
    const res = await fetch(`${bootBase}/pair/v1/info`);
    expect(res.status).toBe(200);
    expect((await res.json()).data.protocol_version).toBe("v1");
  });

  it("routes the full flow: pair on bootstrap, then activate + functional over the device listener", async () => {
    const { credential } = await completePairing();

    // Device credential over the PLAIN bootstrap listener is refused.
    const refused = await postBoot("/core", {}, credential);
    expect(refused.status).toBe(403);

    // Activate + functional over the device (TLS) listener.
    expect((await deviceReq("/auth/device/activate", credential)).status).toBe(200);
    expect((await deviceReq("/core", credential)).status).toBe(200);

    // Revoke, then functional is unauthorized.
    expect((await deviceReq("/auth/device/revoke-self", credential)).status).toBe(200);
    expect((await deviceReq("/core", credential)).status).toBe(401);
  });

  it("rejects an unknown credential on the device listener (401)", async () => {
    expect((await deviceReq("/core", "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")).status).toBe(401);
  });
});
