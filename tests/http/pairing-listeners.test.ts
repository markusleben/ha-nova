import { createServer as createHttp, type Server } from "node:http";
import { createServer as createHttps } from "node:https";
import { request as httpsRequest } from "node:https";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import type { RouteHandler } from "../../nova/src/http/router.js";
import { createBootstrapListener, createDeviceListener } from "../../nova/src/runtime/listeners.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { OPAQUE_CLIENT_ID, OPAQUE_KSF, OPAQUE_SERVER_ID, opaqueReady } from "../../nova/src/security/opaque-server.js";
import { deriveDirectionKeys, open, seal } from "../../nova/src/security/pairing-crypto.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
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
let users: HaAuthUser[] | Error; // Home Assistant's users as the listener reads them (an Error: the read fails)
let usersReads: number;
const fetchAuthUsers = async () => {
  usersReads++;
  if (users instanceof Error) throw users;
  return users;
};

const echo: RouteHandler = () => ({ echoed: true });
const functional = { health: echo, ws: echo, core: echo, files: echo, backups: echo, supervisor: echo, ask: echo };

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
  return { status: res.status, json: (await res.json()) as { ok: boolean; data?: Record<string, unknown>; error?: { code: string } } };
}

// https to the self-signed device listener (pin already proven elsewhere; here
// we only assert routing/auth). Trust the self-signed leaf as its own CA and
// skip only the hostname check, rather than disabling verification outright.
function deviceReq(path: string, auth: string, method = "POST"): Promise<{ status: number; json: any }> {
  return new Promise((resolve, reject) => {
    const req = httpsRequest(
      { host: "127.0.0.1", port: devicePort, path, method, ca: tls.certPem, checkServerIdentity: () => undefined, headers: { authorization: `Bearer ${auth}` } },
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
  users = [{ id: "owner-1", is_owner: true, is_active: true }, { id: "admin-1", is_active: true, group_ids: ["system-admin"] }];
  usersReads = 0;
  const deps = { registry, pairingManager: manager, functional, relayVersion: "0.7.0", now: () => Date.now(), fetchAuthUsers };
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

async function completePairing(madeBy?: string): Promise<{ credential: string }> {
  const { code } = manager.generateCode(madeBy);
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
    const info = (await res.json()).data;
    expect(info.protocol_version).toBe("v1");
    expect(info.sign_in).toBe(true);
    expect(info.window).toBeUndefined(); // no /gate/v1/me here: no window contract
  });

  it("advertises window: 1 in /pair/v1/info where the App serves /gate/v1/me", async () => {
    const withMe = createHttp(createBootstrapListener({ registry, pairingManager: manager, functional: { ...functional, me: echo }, relayVersion: "0.7.0", now: () => Date.now(), fetchAuthUsers }));
    try {
      const res = await fetch(`http://127.0.0.1:${await listen(withMe)}/pair/v1/info`);
      expect((await res.json()).data).toMatchObject({ sign_in: true, window: 1 });
    } finally {
      withMe.close();
    }
  });

  it("hides the pairing window: start while inactive returns a generic 400, not a distinct code", async () => {
    // No code generated in this test, so the manager is inactive. On the
    // unauthenticated bootstrap port the response must be indistinguishable from
    // a malformed request (400), never a distinct 409 that reveals the window.
    const res = await postBoot("/pair/v1/start", { ke1: "AAAA" });
    expect(res.status).toBe(400);
  });

  it("routes the full flow: pair on bootstrap, then activate + functional over the device listener", async () => {
    const { credential } = await completePairing();

    // Device credential over the PLAIN bootstrap listener is refused.
    const refused = await postBoot("/core", {}, credential);
    expect(refused.status).toBe(403);

    // Activate + functional over the device (TLS) listener.
    expect((await deviceReq("/auth/device/activate", credential)).status).toBe(200);
    expect((await deviceReq("/core", credential)).status).toBe(200);
    // Peers: only this device; the test deps can't check chat-app files.
    const peers = await deviceReq("/auth/device/peers", credential, "GET");
    expect(peers.status).toBe(200);
    expect(peers.json.data).toEqual({ other_devices: 0, pairing_open: false, legacy_access: false, chat_app_files: null });

    // Revoke, then functional is unauthorized.
    expect((await deviceReq("/auth/device/revoke-self", credential)).status).toBe(200);
    expect((await deviceReq("/core", credential)).status).toBe(401);
    expect((await deviceReq("/auth/device/peers", credential, "GET")).status).toBe(401);
  });

  it("serves only the pairing routes on the plain listener and refuses every other route before dispatch", async () => {
    const { credential } = await completePairing();
    for (const path of ["/health", "/ws", "/core", "/files", "/backups", "/supervisor", "/gate/v1/ask", "/gate/v1/phone", "/auth/device/peers", "/nope"]) {
      const device = await postBoot(path, {}, credential);
      expect([path, device.status, device.json.error?.code]).toEqual([path, 403, "SECURE_TRANSPORT_REQUIRED"]);
      const res = await fetch(`${bootBase}${path}`, { method: "POST", headers: { authorization: "Bearer old-shared-token", "content-type": "application/json" }, body: "{}" });
      expect([path, res.status, res.headers.get("x-ha-nova-auth"), ((await res.json()) as { error: { code: string } }).error.code]).toEqual([path, 401, "pairing-required", "PAIRING_REQUIRED"]);
      const anonymous = await postBoot(path, {});
      expect([path, anonymous.status, anonymous.json.error?.code]).toEqual([path, 401, "UNAUTHORIZED"]);
    }
  });

  it("answers the old shared token with PAIRING_REQUIRED and the header on the device listener too", async () => {
    const res = await new Promise<{ status: number; header: string | undefined; code: string }>((resolve, reject) => {
      const req = httpsRequest(
        { host: "127.0.0.1", port: devicePort, path: "/core", method: "POST", ca: tls.certPem, checkServerIdentity: () => undefined, headers: { authorization: "Bearer old-shared-token" } },
        (r) => {
          let b = "";
          r.on("data", (c) => (b += c));
          r.on("end", () => resolve({ status: r.statusCode ?? 0, header: r.headers["x-ha-nova-auth"] as string | undefined, code: JSON.parse(b).error.code }));
        }
      );
      req.on("error", reject);
      req.end();
    });
    expect(res).toEqual({ status: 401, header: "pairing-required", code: "PAIRING_REQUIRED" });
  });

  it("lets a computer an administrator connected in only while that person is still an administrator (MAR-170)", async () => {
    const { credential } = await completePairing("admin-1");
    expect((await deviceReq("/auth/device/activate", credential)).status).toBe(200);
    const saved = JSON.parse(readFileSync(join(dir, "device-registry.json"), "utf8")) as { devices: unknown[]; roleCheckedDevices: Array<{ pairedBy: string; state: string }> };
    expect(saved.devices).toEqual([]); // an App before 0.36 reads only "devices": it doesn't know this computer
    expect(saved.roleCheckedDevices).toMatchObject([{ pairedBy: "admin-1", state: "active" }]);
    expect((await deviceReq("/core", credential)).status).toBe(200);
    const admin = (users as HaAuthUser[])[1]!;
    for (const [change, status, code] of [
      [() => (admin.group_ids = ["system-users"]), 401, "UNAUTHORIZED"],
      [() => (admin.group_ids = ["system-admin"]), 200, undefined], // the role is back: so is the access
      [() => (admin.is_active = false), 401, "UNAUTHORIZED"],
      [() => (users = [(users as HaAuthUser[])[0]!]), 401, "UNAUTHORIZED"], // deleted
      [() => (users = new Error("Home Assistant is restarting")), 503, "ROLE_UNKNOWN"],
    ] as const) {
      change();
      const res = await deviceReq("/core", credential);
      expect([res.status, res.json.error?.code]).toEqual([status, code]);
    }
  });

  it("reads no users for a computer the owner's code connected", async () => {
    const { credential } = await completePairing();
    expect((await deviceReq("/auth/device/activate", credential)).status).toBe(200);
    users = new Error("never read");
    expect((await deviceReq("/core", credential)).status).toBe(200);
    expect(usersReads).toBe(0);
    expect(registry.list()[0]!.pairedBy).toBeUndefined();
  });

  it("rejects an unknown credential on the device listener (401)", async () => {
    expect((await deviceReq("/core", "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")).status).toBe(401);
  });
});
